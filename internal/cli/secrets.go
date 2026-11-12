package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/pkg/problem"
)

type secretJSON struct {
	Key               string    `json:"key"`
	Kind              string    `json:"kind"`
	Comment           string    `json:"comment,omitempty"`
	Value             string    `json:"value,omitempty"`
	Version           int       `json:"version"`
	Revealed          bool      `json:"revealed"`
	SourceEnvironment string    `json:"source_environment,omitempty"`
	Inherited         bool      `json:"inherited,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type secretsListResponse struct {
	Secrets []secretJSON `json:"secrets"`
}

type versionsResponse struct {
	Versions []struct {
		Version       int       `json:"version"`
		ChangeMessage string    `json:"change_message,omitempty"`
		ActorID       string    `json:"actor_id,omitempty"`
		CreatedAt     time.Time `json:"created_at"`
	} `json:"versions"`
}

func secretsBase(r *resolvedContext) string {
	return fmt.Sprintf("/api/v1/orgs/%s/projects/%s/environments/%s/secrets",
		url.PathEscape(r.Org), url.PathEscape(r.Project), url.PathEscape(r.Env))
}

func secretPath(r *resolvedContext, key string) string {
	return secretsBase(r) + "/" + url.PathEscape(key)
}

func newSecretsCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secrets in the linked project",
	}
	cmd.AddCommand(newSecretsListCommand(opts))
	cmd.AddCommand(newSecretsGetCommand(opts))
	cmd.AddCommand(newSecretsSetCommand(opts))
	cmd.AddCommand(newSecretsRemoveCommand(opts))
	cmd.AddCommand(newSecretsHistoryCommand(opts))
	cmd.AddCommand(newSecretsRollbackCommand(opts))
	cmd.AddCommand(newSecretsGenerateCommand(opts))
	cmd.AddCommand(newSecretsOverrideCommand(opts))
	return cmd
}

func newSecretsListCommand(opts *Options) *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List secret keys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result secretsListResponse
			path := secretsBase(resolved)
			query := url.Values{}
			if reveal {
				query.Set("reveal", "true")
				query.Set("channel", "cli")
			}
			if opts.Overlay != "" {
				query.Set("overlay", opts.Overlay)
			}
			if len(query) > 0 {
				path += "?" + query.Encode()
			}
			if err := c.Get(cmd.Context(), path, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Secrets)
			}
			if len(result.Secrets) == 0 {
				out.Info("No secrets in %s/%s/%s.", resolved.Org, resolved.Project, resolved.Env)
				return nil
			}
			hasSource := false
			for _, secret := range result.Secrets {
				if secret.Inherited {
					hasSource = true
				}
			}
			headers := []string{"KEY", "KIND", "VERSION"}
			if hasSource {
				headers = append(headers, "SOURCE")
			}
			if reveal {
				headers = append(headers, "VALUE")
			}
			rows := make([][]string, 0, len(result.Secrets))
			for _, secret := range result.Secrets {
				row := []string{secret.Key, secret.Kind, fmt.Sprintf("%d", secret.Version)}
				if hasSource {
					source := "local"
					if secret.Inherited {
						source = secret.SourceEnvironment
					}
					row = append(row, source)
				}
				if reveal {
					row = append(row, secret.Value)
				}
				rows = append(rows, row)
			}
			out.Table(headers, rows)
			return nil
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "decrypt values (requires secrets:read-values, audited)")
	return cmd
}

func newSecretsGetCommand(opts *Options) *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "get KEY",
		Short: "Print one secret value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var secret secretJSON
			path := secretPath(resolved, args[0]) + "?reveal=true&reason=cli&channel=cli"
			if opts.Overlay != "" {
				path += "&overlay=" + url.QueryEscape(opts.Overlay)
			}
			if err := c.Get(cmd.Context(), path, &secret); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(secret)
			}
			fmt.Fprintln(out.Out, secret.Value)
			return nil
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", true, "print the decrypted value")
	return cmd
}

func newSecretsSetCommand(opts *Options) *cobra.Command {
	var kind, message, comment string
	var yes bool
	cmd := &cobra.Command{
		Use:   "set KEY [VALUE]",
		Short: "Create or update a secret (VALUE omitted prompts, - reads stdin)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			value := ""
			switch {
			case len(args) == 2 && args[1] == "-":
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				value = strings.TrimRight(string(b), "\r\n")
			case len(args) == 2:
				value = args[1]
			default:
				value, err = promptSecret("Value for " + args[0] + ": ")
				if err != nil {
					return err
				}
			}
			if err := requireProductionConfirmation(cmd.Context(), opts, c, resolved, "set "+args[0], yes); err != nil {
				return err
			}
			secret, created, err := upsertSecret(cmd.Context(), c, resolved, args[0], value, kind, comment, message, "")
			if err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(secret)
			}
			action := "Updated"
			if created {
				action = "Created"
			}
			out.Success("%s %s (v%d) in %s/%s/%s", action, secret.Key, secret.Version, resolved.Org, resolved.Project, resolved.Env)
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "secret", "secret, variable, or file")
	cmd.Flags().StringVar(&comment, "comment", "", "comment shown in the UI")
	cmd.Flags().StringVar(&message, "message", "", "why this changed (shown in history)")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip production confirmation")
	return cmd
}

type apiClient interface {
	Get(context.Context, string, any) error
	Post(context.Context, string, any, any) error
	Put(context.Context, string, any, any) error
	Delete(context.Context, string) error
}

func upsertSecret(ctx context.Context, c apiClient, resolved *resolvedContext, key, value, kind, comment, message, folder string) (secretJSON, bool, error) {
	if kind == "" {
		kind = "secret"
	}
	body := map[string]string{
		"value": value,
		"kind":  kind,
	}
	if comment != "" {
		body["comment"] = comment
	}
	if message != "" {
		body["message"] = message
	}
	if folder != "" {
		body["folder"] = folder
	}

	var existing secretJSON
	err := c.Get(ctx, secretPath(resolved, key), &existing)
	if err == nil {
		var updated secretJSON
		if err := c.Put(ctx, secretPath(resolved, key), body, &updated); err != nil {
			return secretJSON{}, false, err
		}
		return updated, false, nil
	}
	var p *problem.Problem
	if !errors.As(err, &p) || p.Status != http.StatusNotFound {
		return secretJSON{}, false, err
	}
	body["key"] = key
	var created secretJSON
	if err := c.Post(ctx, secretsBase(resolved), body, &created); err != nil {
		return secretJSON{}, false, err
	}
	return created, true, nil
}

func newSecretsRemoveCommand(opts *Options) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "rm KEY",
		Short: "Delete a secret (soft delete, history kept)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if err := requireProductionConfirmation(cmd.Context(), opts, c, resolved, "delete "+args[0], yes); err != nil {
				return err
			}
			if !yes {
				ok, err := confirm(fmt.Sprintf("Delete %s from %s/%s?", args[0], resolved.Project, resolved.Env))
				if err != nil {
					return err
				}
				if !ok {
					out.Info("Cancelled.")
					return nil
				}
			}
			if err := c.Delete(cmd.Context(), secretPath(resolved, args[0])); err != nil {
				return err
			}
			out.Success("Deleted %s", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation")
	return cmd
}

func newSecretsHistoryCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "history KEY",
		Short: "Show version history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result versionsResponse
			if err := c.Get(cmd.Context(), secretPath(resolved, args[0])+"/versions", &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Versions)
			}
			rows := make([][]string, 0, len(result.Versions))
			for _, v := range result.Versions {
				rows = append(rows, []string{
					fmt.Sprintf("%d", v.Version),
					v.CreatedAt.Local().Format("2006-01-02 15:04"),
					v.ActorID,
					v.ChangeMessage,
				})
			}
			out.Table([]string{"VERSION", "WHEN", "ACTOR", "MESSAGE"}, rows)
			return nil
		},
	}
}

func newSecretsRollbackCommand(opts *Options) *cobra.Command {
	var version int
	var yes bool
	cmd := &cobra.Command{
		Use:   "rollback KEY",
		Short: "Create a new version from an old one",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if version < 1 {
				return errors.New("--version must be 1 or greater")
			}
			if err := requireProductionConfirmation(cmd.Context(), opts, c, resolved, "rollback "+args[0], yes); err != nil {
				return err
			}
			var secret secretJSON
			path := secretPath(resolved, args[0]) + "/rollback"
			if err := c.Post(cmd.Context(), path, map[string]int{"version": version}, &secret); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(secret)
			}
			out.Success("Rolled %s back to v%d as v%d", args[0], version, secret.Version)
			return nil
		},
	}
	cmd.Flags().IntVar(&version, "version", 0, "version to restore")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip production confirmation")
	return cmd
}

func newSecretsGenerateCommand(opts *Options) *cobra.Command {
	var genType, kind, message string
	var length int
	var yes bool
	cmd := &cobra.Command{
		Use:   "generate KEY",
		Short: "Generate a random value and store it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			value, err := generateValue(genType, length)
			if err != nil {
				return err
			}
			if err := requireProductionConfirmation(cmd.Context(), opts, c, resolved, "generate "+args[0], yes); err != nil {
				return err
			}
			secret, created, err := upsertSecret(cmd.Context(), c, resolved, args[0], value, kind, "", message, "")
			if err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(secret)
			}
			action := "Updated"
			if created {
				action = "Created"
			}
			out.Success("%s %s (v%d, %s, %d bytes)", action, secret.Key, secret.Version, genType, len(value))
			if out.Color {
				out.Info("Value: %s", value)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&genType, "type", "password", "password, apikey, hex, base64, uuid, jwt, ed25519, or rsa")
	cmd.Flags().IntVar(&length, "length", 32, "length for password/apikey/hex/base64 types")
	cmd.Flags().StringVar(&kind, "kind", "secret", "secret, variable, or file")
	cmd.Flags().StringVar(&message, "message", "Generated", "why this changed")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip production confirmation")
	return cmd
}

func generateValue(genType string, length int) (string, error) {
	if length <= 0 {
		length = 32
	}
	switch genType {
	case "uuid":
		return uuid.NewString(), nil
	case "hex":
		b := make([]byte, (length+1)/2)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return hex.EncodeToString(b)[:length], nil
	case "base64":
		b := make([]byte, length)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(b), nil
	case "apikey":
		return randomFrom("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", length)
	case "password":
		return randomFrom("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*()-_=+[]{}", length)
	case "jwt":
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(b), nil
	case "ed25519":
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", err
		}
		der, err := x509.MarshalPKCS8PrivateKey(privateKey)
		if err != nil {
			return "", err
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
	case "rsa":
		privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return "", err
		}
		der, err := x509.MarshalPKCS8PrivateKey(privateKey)
		if err != nil {
			return "", err
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
	default:
		return "", fmt.Errorf("unknown type %q (password, apikey, hex, base64, uuid, jwt, ed25519, rsa)", genType)
	}
}

func randomFrom(alphabet string, length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, length)
	for i := range b {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out), nil
}

func newSecretsOverrideCommand(opts *Options) *cobra.Command {
	var remove bool
	var comment string
	cmd := &cobra.Command{
		Use:   "override KEY [VALUE]",
		Short: "Set or remove a personal override (non-production only)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			endpoint := secretPath(resolved, args[0]) + "/override"
			if remove {
				if err := c.Delete(cmd.Context(), endpoint); err != nil {
					return err
				}
				if opts.JSON {
					return out.PrintJSON(map[string]string{"key": args[0], "override": "removed"})
				}
				out.Success("Removed the personal override for %s.", args[0])
				return nil
			}
			value := ""
			switch {
			case len(args) == 2 && args[1] == "-":
				b, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				value = strings.TrimRight(string(b), "\r\n")
			case len(args) == 2:
				value = args[1]
			default:
				value, err = promptSecret("Override value for " + args[0] + ": ")
				if err != nil {
					return err
				}
			}
			var secret secretJSON
			if err := c.Put(cmd.Context(), endpoint, map[string]string{"value": value, "comment": comment}, &secret); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(secret)
			}
			out.Success("Saved a personal override for %s in %s.", args[0], resolved.Env)
			return nil
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the override and fall back to the shared value")
	cmd.Flags().StringVar(&comment, "comment", "", "optional note shown in history")
	return cmd
}
