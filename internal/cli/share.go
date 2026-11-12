package cli

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newShareCommand(opts *Options) *cobra.Command {
	var ttl time.Duration
	var views int
	var password string
	var promptPassword bool
	cmd := &cobra.Command{
		Use:   "share KEY",
		Short: "Create a client-encrypted one-time share link",
		Long: "Encrypt a secret value locally, upload only ciphertext, and print a link\n" +
			"whose URL fragment contains the key. The server cannot decrypt it. The\n" +
			"link can be limited by views, expiry, and an optional password.",
		Args: cobra.ExactArgs(1),
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
			if promptPassword && password == "" {
				password, err = promptSecret("Share password (empty for none): ")
				if err != nil {
					return err
				}
			}
			var secret secretJSON
			if err := c.Get(cmd.Context(), secretPath(resolved, args[0])+"?reveal=true&reason=share&channel=cli", &secret); err != nil {
				return err
			}
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return err
			}
			nonce := make([]byte, 12)
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			block, err := aes.NewCipher(key)
			if err != nil {
				return err
			}
			gcm, err := cipher.NewGCM(block)
			if err != nil {
				return err
			}
			ciphertext := gcm.Seal(nil, nonce, []byte(secret.Value), nil)
			hours := int(ttl.Hours())
			if hours <= 0 {
				hours = 24
			}
			var created struct {
				ID        string `json:"id"`
				ExpiresAt string `json:"expires_at"`
			}
			sharePath := strings.TrimSuffix(secretsBase(resolved), "/secrets") + "/shares"
			if err := c.Post(cmd.Context(), sharePath, map[string]any{
				"ciphertext": base64.RawURLEncoding.EncodeToString(ciphertext),
				"nonce":      base64.RawURLEncoding.EncodeToString(nonce),
				"ttl_hours":  hours,
				"max_views":  views,
				"password":   password,
			}, &created); err != nil {
				return err
			}
			link := resolved.Server + "/share/" + created.ID + "#" + base64.RawURLEncoding.EncodeToString(key)
			if opts.JSON {
				return out.PrintJSON(map[string]any{
					"id": created.ID, "url": link, "expires_at": created.ExpiresAt, "max_views": views,
				})
			}
			out.Success("Share link created.")
			out.Info("%s", link)
			out.Warn("The URL fragment contains the decryption key. Share it only with the intended recipient.")
			return nil
		},
	}
	cmd.Flags().DurationVar(&ttl, "ttl", 24*time.Hour, "link lifetime (1h–720h)")
	cmd.Flags().IntVar(&views, "views", 1, "maximum claims (1–100)")
	cmd.Flags().StringVar(&password, "password", "", "optional password required to claim")
	cmd.Flags().BoolVar(&promptPassword, "prompt-password", false, "prompt for a password")
	return cmd
}
