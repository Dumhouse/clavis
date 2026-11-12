package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/dumhouse/clavis/internal/importers"
	"github.com/dumhouse/clavis/pkg/client"
)

func newImportCommand(opts *Options) *cobra.Command {
	var kind, message, provider, token, baseURL string
	var remoteProject, remoteEnv, remotePath string
	var vaultMount, onePasswordVault string
	var vaultVersion int
	var awsRegion, awsAccessKey, awsSecretKey, awsSessionToken, awsPrefix, awsService string
	var gcpProject, gcpServiceAccountFile, gcpPrefix string
	var folder, mapFile string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "import [FILE]",
		Short: "Import secrets from files or external providers",
		Long: "Import .env, JSON, or YAML files, dotenv directories, or pull from\n" +
			"Infisical, Doppler, Vault/OpenBao, 1Password Connect, AWS Secrets Manager,\n" +
			"or GCP Secret Manager. Dry runs preview new, changed, and unchanged keys.",
		Args: cobra.MaximumNArgs(1),
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

			if provider == "" || provider == "file" {
				if len(args) != 1 {
					return fmt.Errorf("pass a file or directory, or choose a provider with --provider")
				}
			} else if len(args) > 0 {
				return fmt.Errorf("--provider %s does not take a file argument", provider)
			}

			entries := make([]importEntry, 0)
			if provider == "" || provider == "file" {
				parsed, err := collectImportEntries(args[0], kind)
				if err != nil {
					return err
				}
				entries = parsed
			} else {
				options, err := importProviderOptions(provider, importProviderFlags{
					Token: token, BaseURL: baseURL, Project: remoteProject, Env: remoteEnv, Path: remotePath,
					VaultMount: vaultMount, VaultVersion: vaultVersion, OnePasswordVault: onePasswordVault,
					AWSRegion: awsRegion, AWSAccessKey: awsAccessKey, AWSSecretKey: awsSecretKey,
					AWSSessionToken: awsSessionToken, AWSPrefix: awsPrefix, AWSService: awsService,
					GCPProject: gcpProject, GCPServiceAccountFile: gcpServiceAccountFile, GCPPrefix: gcpPrefix,
				})
				if err != nil {
					return err
				}
				imported, err := importers.Load(cmd.Context(), options)
				if err != nil {
					return err
				}
				for _, entry := range imported {
					entries = append(entries, importEntry{
						key: entry.Key, value: entry.Value, kind: entry.Kind, source: entry.Source,
					})
				}
			}
			if len(entries) == 0 {
				return fmt.Errorf("no key/value pairs found")
			}
			mapping, err := loadImportMapping(mapFile)
			if err != nil {
				return err
			}
			entries = applyImportMapping(entries, mapping, folder, kind)
			if len(entries) == 0 {
				return fmt.Errorf("the mapping removed every key")
			}

			current, revealed := currentImportValues(cmd.Context(), c, resolved)
			if dryRun {
				if opts.JSON {
					rows := make([]map[string]any, 0, len(entries))
					for _, entry := range entries {
						rows = append(rows, map[string]any{
							"key": entry.key, "kind": entry.kind, "folder": entry.folder,
							"action": importAction(entry, current, revealed),
						})
					}
					return out.PrintJSON(rows)
				}
				rows := make([][]string, 0, len(entries))
				for _, entry := range entries {
					rows = append(rows, []string{entry.key, entry.kind, entry.folderOrRoot(), importAction(entry, current, revealed)})
				}
				out.Table([]string{"KEY", "KIND", "FOLDER", "ACTION"}, rows)
				out.Info("%d keys would be imported into %s/%s/%s.", len(entries), resolved.Org, resolved.Project, resolved.Env)
				return nil
			}

			if err := requireProductionConfirmation(cmd.Context(), opts, c, resolved, "import", yes); err != nil {
				return err
			}
			imported := 0
			for _, entry := range entries {
				entryMessage := message
				if entryMessage == "" {
					entryMessage = "Imported from " + entry.source
				}
				if _, _, err := upsertSecret(cmd.Context(), c, resolved, entry.key, entry.value, entry.kind, "", entryMessage, entry.folder); err != nil {
					return fmt.Errorf("import %s: %w", entry.key, err)
				}
				imported++
			}
			out.Success("Imported %d keys into %s/%s/%s", imported, resolved.Org, resolved.Project, resolved.Env)
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "file", "file, infisical, doppler, vault, onepassword, aws, or gcp")
	cmd.Flags().StringVar(&kind, "kind", "", "secret, variable, or file (default: preserve provider kind, else secret)")
	cmd.Flags().StringVar(&message, "message", "", "change message recorded in history")
	cmd.Flags().StringVar(&folder, "folder", "", "target folder for imported keys")
	cmd.Flags().StringVar(&mapFile, "map", "", "YAML/JSON key and folder mapping file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview new, changed, and unchanged keys without writing")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip production confirmation")
	cmd.Flags().StringVar(&token, "token", "", "provider token (or provider-specific environment variable)")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "provider base URL")
	cmd.Flags().StringVar(&remoteProject, "remote-project", "", "remote project/workspace ID")
	cmd.Flags().StringVar(&remoteEnv, "remote-env", "", "remote environment/config name")
	cmd.Flags().StringVar(&remotePath, "remote-path", "", "remote secret path")
	cmd.Flags().StringVar(&vaultMount, "vault-mount", "secret", "Vault KV mount")
	cmd.Flags().IntVar(&vaultVersion, "vault-version", 2, "Vault KV version: 1 or 2")
	cmd.Flags().StringVar(&onePasswordVault, "op-vault", "", "1Password vault ID")
	cmd.Flags().StringVar(&awsRegion, "aws-region", "", "AWS region")
	cmd.Flags().StringVar(&awsAccessKey, "aws-access-key", "", "AWS access key ID")
	cmd.Flags().StringVar(&awsSecretKey, "aws-secret-key", "", "AWS secret access key")
	cmd.Flags().StringVar(&awsSessionToken, "aws-session-token", "", "AWS session token")
	cmd.Flags().StringVar(&awsPrefix, "aws-prefix", "", "only import secrets with this name prefix")
	cmd.Flags().StringVar(&awsService, "aws-service", "secretsmanager", "AWS service (secretsmanager)")
	cmd.Flags().StringVar(&gcpProject, "gcp-project", "", "GCP project ID")
	cmd.Flags().StringVar(&gcpServiceAccountFile, "gcp-service-account", "", "GCP service account JSON file")
	cmd.Flags().StringVar(&gcpPrefix, "gcp-prefix", "", "only import secrets with this name prefix")
	return cmd
}

type importProviderFlags struct {
	Token                 string
	BaseURL               string
	Project               string
	Env                   string
	Path                  string
	VaultMount            string
	VaultVersion          int
	OnePasswordVault      string
	AWSRegion             string
	AWSAccessKey          string
	AWSSecretKey          string
	AWSSessionToken       string
	AWSPrefix             string
	AWSService            string
	GCPProject            string
	GCPServiceAccountFile string
	GCPPrefix             string
}

func importProviderOptions(provider string, flags importProviderFlags) (importers.Options, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	options := importers.Options{
		Provider: provider, BaseURL: flags.BaseURL, Token: flags.Token,
		RemoteProject: flags.Project, RemoteEnv: flags.Env, RemotePath: flags.Path,
		VaultMount: flags.VaultMount, VaultVersion: flags.VaultVersion,
		OnePasswordVault: flags.OnePasswordVault,
		AWSRegion:        flags.AWSRegion, AWSAccessKey: flags.AWSAccessKey, AWSSecretKey: flags.AWSSecretKey,
		AWSSessionToken: flags.AWSSessionToken, AWSPrefix: flags.AWSPrefix, AWSKind: "secret",
		GCPProject: flags.GCPProject, GCPPrefix: flags.GCPPrefix,
	}
	switch provider {
	case "infisical":
		options.Token = firstNonEmpty(options.Token, os.Getenv("INFISICAL_TOKEN"))
		options.BaseURL = firstNonEmpty(options.BaseURL, os.Getenv("INFISICAL_URL"))
		options.RemoteProject = firstNonEmpty(options.RemoteProject, os.Getenv("INFISICAL_PROJECT"))
		options.RemoteEnv = firstNonEmpty(options.RemoteEnv, os.Getenv("INFISICAL_ENV"))
		options.RemotePath = firstNonEmpty(options.RemotePath, os.Getenv("INFISICAL_PATH"))
	case "doppler":
		options.Token = firstNonEmpty(options.Token, os.Getenv("DOPPLER_TOKEN"))
		options.BaseURL = firstNonEmpty(options.BaseURL, os.Getenv("DOPPLER_API_URL"))
		options.RemoteProject = firstNonEmpty(options.RemoteProject, os.Getenv("DOPPLER_PROJECT"))
		options.RemoteEnv = firstNonEmpty(options.RemoteEnv, os.Getenv("DOPPLER_CONFIG"))
	case "vault", "openbao":
		options.Token = firstNonEmpty(options.Token, os.Getenv("VAULT_TOKEN"), os.Getenv("OPENBAO_TOKEN"))
		options.BaseURL = firstNonEmpty(options.BaseURL, os.Getenv("VAULT_ADDR"), os.Getenv("OPENBAO_ADDR"))
		options.RemotePath = firstNonEmpty(options.RemotePath, os.Getenv("VAULT_PATH"), os.Getenv("OPENBAO_PATH"))
	case "onepassword", "1password":
		options.Token = firstNonEmpty(options.Token, os.Getenv("OP_CONNECT_TOKEN"), os.Getenv("OP_SERVICE_ACCOUNT_TOKEN"))
		options.BaseURL = firstNonEmpty(options.BaseURL, os.Getenv("OP_CONNECT_HOST"))
		options.OnePasswordVault = firstNonEmpty(options.OnePasswordVault, os.Getenv("OP_VAULT"))
	case "aws", "aws-secrets-manager":
		options.AWSRegion = firstNonEmpty(options.AWSRegion, os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"))
		options.AWSAccessKey = firstNonEmpty(options.AWSAccessKey, os.Getenv("AWS_ACCESS_KEY_ID"))
		options.AWSSecretKey = firstNonEmpty(options.AWSSecretKey, os.Getenv("AWS_SECRET_ACCESS_KEY"))
		options.AWSSessionToken = firstNonEmpty(options.AWSSessionToken, os.Getenv("AWS_SESSION_TOKEN"))
		options.AWSPrefix = firstNonEmpty(options.AWSPrefix, os.Getenv("AWS_SECRETS_PREFIX"))
		if !strings.EqualFold(flags.AWSService, "secretsmanager") {
			return options, fmt.Errorf("only the secretsmanager AWS service is supported for imports")
		}
	case "gcp", "gcp-secret-manager":
		options.GCPProject = firstNonEmpty(options.GCPProject, os.Getenv("GCP_PROJECT"), os.Getenv("GOOGLE_CLOUD_PROJECT"))
		accountFile := firstNonEmpty(flags.GCPServiceAccountFile, os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
		if accountFile != "" {
			raw, err := os.ReadFile(accountFile)
			if err != nil {
				return options, fmt.Errorf("read GCP service account: %w", err)
			}
			options.GCPServiceAccount = string(raw)
		}
	default:
		return options, fmt.Errorf("unsupported import provider %q", provider)
	}
	return options, nil
}

type importMapping struct {
	Keys    map[string]string `json:"keys" yaml:"keys"`
	Folders map[string]string `json:"folders" yaml:"folders"`
	Folder  string            `json:"folder" yaml:"folder"`
}

func loadImportMapping(path string) (importMapping, error) {
	if path == "" {
		return importMapping{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return importMapping{}, fmt.Errorf("read mapping: %w", err)
	}
	var mapping importMapping
	if err := yaml.Unmarshal(raw, &mapping); err != nil {
		return importMapping{}, fmt.Errorf("parse mapping: %w", err)
	}
	return mapping, nil
}

func applyImportMapping(entries []importEntry, mapping importMapping, folder, kind string) []importEntry {
	out := make([]importEntry, 0, len(entries))
	for _, entry := range entries {
		if target, ok := mapping.Keys[entry.key]; ok {
			if strings.TrimSpace(target) == "" {
				continue
			}
			entry.key = strings.TrimSpace(target)
		}
		if kind != "" {
			entry.kind = kind
		}
		if entry.kind == "" {
			entry.kind = "secret"
		}
		entry.folder = firstNonEmpty(folder, mapping.Folders[entry.folder], mapping.Folder, entry.folder)
		out = append(out, entry)
	}
	return out
}

func currentImportValues(ctx context.Context, c *client.Client, resolved *resolvedContext) (map[string]string, bool) {
	values := map[string]string{}
	var result secretsListResponse
	if err := c.Get(ctx, secretsBase(resolved)+":resolve?channel=cli", &result); err == nil {
		for _, secret := range result.Secrets {
			values[secret.Key] = secret.Value
		}
		return values, true
	}
	if err := c.Get(ctx, secretsBase(resolved), &result); err != nil {
		return values, false
	}
	for _, secret := range result.Secrets {
		values[secret.Key] = ""
	}
	return values, false
}

func importAction(entry importEntry, current map[string]string, revealed bool) string {
	value, ok := current[entry.key]
	if !ok {
		return "new"
	}
	if !revealed {
		return "present"
	}
	if value == entry.value {
		return "unchanged"
	}
	return "changed"
}

func readAll(r *os.File) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	return buf.Bytes(), err
}

// ParseImportFile selects a parser by extension, defaulting to dotenv.
func parseImportFile(path string, data []byte) (map[string]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return parseJSONValues(data)
	case ".yaml", ".yml":
		return parseYAMLValues(data)
	default:
		return parseDotenv(data)
	}
}

func parseJSONValues(data []byte) (map[string]string, error) {
	out := map[string]string{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return out, nil
}

func parseYAMLValues(data []byte) (map[string]string, error) {
	out := map[string]string{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	return out, nil
}

// ParseDotenv parses .env content. It supports comments, `export ` prefixes,
// single/double quotes, and common escapes. It never executes code.
func ParseDotenv(data []byte) (map[string]string, error) { return parseDotenv(data) }

func parseDotenv(data []byte) (map[string]string, error) {
	out := map[string]string{}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", lineNo)
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNo)
		}
		raw := strings.TrimSpace(line[eq+1:])
		value, err := parseDotenvValue(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		out[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dotenv: %w", err)
	}
	return out, nil
}

func parseDotenvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '"':
		if len(raw) < 2 || raw[len(raw)-1] != '"' {
			return "", fmt.Errorf("unterminated double quote")
		}
		inner := raw[1 : len(raw)-1]
		var b strings.Builder
		for i := 0; i < len(inner); i++ {
			if inner[i] != '\\' {
				b.WriteByte(inner[i])
				continue
			}
			i++
			if i >= len(inner) {
				return "", fmt.Errorf("trailing backslash")
			}
			switch inner[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(inner[i])
			}
		}
		return b.String(), nil
	case '\'':
		if len(raw) < 2 || raw[len(raw)-1] != '\'' {
			return "", fmt.Errorf("unterminated single quote")
		}
		return raw[1 : len(raw)-1], nil
	default:
		// Strip an inline comment only when preceded by whitespace.
		for i := 1; i < len(raw); i++ {
			if raw[i] == '#' && unicode.IsSpace(rune(raw[i-1])) {
				raw = strings.TrimSpace(raw[:i])
				break
			}
		}
		return raw, nil
	}
}

type importEntry struct {
	key    string
	value  string
	kind   string
	folder string
	source string
}

func (e importEntry) folderOrRoot() string {
	if e.folder == "" {
		return "/"
	}
	return e.folder
}

// collectImportEntries accepts a file or a directory. Directories are walked
// recursively; each file's parent directory becomes the secret folder.
func collectImportEntries(path, _ string) ([]importEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var files []string
	root := ""
	if info.IsDir() {
		root = path
		err = filepath.WalkDir(path, func(candidate string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if importableFile(entry.Name()) {
				files = append(files, candidate)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		files = []string{path}
	}
	sort.Strings(files)
	var out []importEntry
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		values, err := parseImportFile(file, data)
		if err != nil {
			return nil, err
		}
		folder := ""
		if root != "" {
			rel, err := filepath.Rel(root, filepath.Dir(file))
			if err == nil && rel != "." {
				folder = "/" + filepath.ToSlash(rel)
			}
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out = append(out, importEntry{key: key, value: values[key], folder: folder, source: file})
		}
	}
	return out, nil
}

func importableFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, ".env") {
		return true
	}
	switch filepath.Ext(lower) {
	case ".json", ".yaml", ".yml":
		return true
	default:
		return false
	}
}
