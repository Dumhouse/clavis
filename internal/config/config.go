// Package config loads Clavis configuration from defaults, an optional YAML
// file, and environment variables (in increasing precedence).
package config

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that accepts "15s"-style YAML values or plain
// integer seconds.
type Duration time.Duration

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// String implements fmt.Stringer.
func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode && (value.Tag == "!!int" || value.Tag == "!!float") {
		var seconds int64
		if err := value.Decode(&seconds); err != nil {
			return fmt.Errorf("duration must be a string like \"15s\" or integer seconds")
		}
		*d = Duration(time.Duration(seconds) * time.Second)
		return nil
	}
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string like \"15s\" or integer seconds")
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

// Config is the root configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Keys     KeysConfig     `yaml:"keys"`
	Backup   BackupConfig   `yaml:"backup"`
	Log      LogConfig      `yaml:"log"`
	Email    EmailConfig    `yaml:"email"`
	Security SecurityConfig `yaml:"security"`
	Audit    AuditConfig    `yaml:"audit"`
}

// AuditConfig controls audit event export.
type AuditConfig struct {
	Exporters []AuditExporterConfig `yaml:"exporters"`
}

// AuditExporterConfig describes one external audit destination.
type AuditExporterConfig struct {
	Type      string `yaml:"type"`
	Format    string `yaml:"format"`
	URL       string `yaml:"url"`
	Token     string `yaml:"token"`
	APIKey    string `yaml:"api_key"`
	Address   string `yaml:"address"`
	Network   string `yaml:"network"`
	Bucket    string `yaml:"bucket"`
	Region    string `yaml:"region"`
	Endpoint  string `yaml:"endpoint"`
	Prefix    string `yaml:"prefix"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
}

// SecurityConfig controls optional hardening checks.
type SecurityConfig struct {
	BreachCheck    bool   `yaml:"breach_check"`
	BreachCheckURL string `yaml:"breach_check_url"`
	// OutboundAllowlist lists IPs or CIDRs that webhooks, syncs, SSO discovery
	// and audit exporters may reach even though they are private, loopback or
	// link-local, for example a Forgejo or Vault on the local network.
	// Everything else private stays blocked.
	OutboundAllowlist []string `yaml:"outbound_allowlist"`
}

// EmailConfig controls optional transactional email (password resets, invites).
type EmailConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
	TLS      string `yaml:"tls"` // starttls | none | tls
}

// ServerConfig controls the HTTP listener.
type ServerConfig struct {
	Addr               string   `yaml:"addr"`
	BaseURL            string   `yaml:"base_url"`
	Dev                bool     `yaml:"dev"`
	ReadTimeout        Duration `yaml:"read_timeout"`
	WriteTimeout       Duration `yaml:"write_timeout"`
	IdleTimeout        Duration `yaml:"idle_timeout"`
	ShutdownTimeout    Duration `yaml:"shutdown_timeout"`
	MaxBodyBytes       int64    `yaml:"max_body_bytes"`
	MetricsPublic      bool     `yaml:"metrics_public"`
	RateLimitPerMinute int      `yaml:"rate_limit_per_minute"`
	// TrustedProxies lists CIDRs or IPs of reverse proxies whose
	// X-Forwarded-For / X-Real-IP headers are believed. Empty (the default)
	// means those headers are ignored and the socket peer is the client.
	TrustedProxies []string `yaml:"trusted_proxies"`
	// TLS makes Clavis serve HTTPS itself. Off by default: localhost trials
	// and deployments behind a reverse proxy serve plain HTTP.
	TLS TLSConfig `yaml:"tls"`
	// Setup presets the first-run claim (useful for compose files and
	// automation). Both fields are optional.
	Setup SetupConfig `yaml:"setup"`
}

// TLS modes.
const (
	TLSOff        = "off"         // plain HTTP (localhost, or behind a reverse proxy)
	TLSSelfSigned = "self-signed" // generate a certificate for the base URL host
	TLSFile       = "file"        // read cert_file and key_file, reload on change
	TLSACME       = "acme"        // Let's Encrypt (or another ACME CA), automatic
)

// TLSConfig controls the built-in HTTPS listener.
type TLSConfig struct {
	Mode     string `yaml:"mode"`      // off | self-signed | file | acme
	Addr     string `yaml:"addr"`      // HTTPS listen address, default :8443
	CertFile string `yaml:"cert_file"` // mode=file
	KeyFile  string `yaml:"key_file"`  // mode=file
	Dir      string `yaml:"dir"`       // where generated and ACME certificates live, default <data-dir>/tls
	// ACME settings. Domains default to the base URL host.
	ACMEEmail     string   `yaml:"acme_email"`
	ACMEDomains   []string `yaml:"acme_domains"`
	ACMEDirectory string   `yaml:"acme_directory"` // default Let's Encrypt production
	// DNS-01 challenge: proves the domain through a TXT record, so the server
	// needs no inbound internet access (home labs, private networks) and
	// wildcard names work.
	ACMEDNS          string   `yaml:"acme_dns"`            // provider: cloudflare | duckdns
	ACMEDNSToken     string   `yaml:"acme_dns_token"`      // provider API token
	ACMEDNSTokenFile string   `yaml:"acme_dns_token_file"` // or read it from a file (Docker secrets)
	ACMEDNSResolvers []string `yaml:"acme_dns_resolvers"`  // check propagation here; default: the zone's nameservers
	ACMEDNSWait      Duration `yaml:"acme_dns_wait"`       // max time to wait for the TXT record, default 3m
}

// ACME DNS providers.
const (
	ACMEDNSCloudflare = "cloudflare"
	ACMEDNSDuckDNS    = "duckdns"
)

// Enabled reports whether Clavis terminates TLS itself.
func (t TLSConfig) Enabled() bool { return t.Mode != "" && t.Mode != TLSOff }

// SetupConfig presets first-run setup.
type SetupConfig struct {
	// Token, when set, is the one-time setup token instead of a random one.
	Token string `yaml:"token"`
	// OwnerEmail, when set, is the only email allowed for the owner account
	// and is prefilled in the setup wizard.
	OwnerEmail string `yaml:"owner_email"`
}

// DatabaseConfig controls the data store.
type DatabaseConfig struct {
	Driver string `yaml:"driver"` // sqlite | postgres
	Path   string `yaml:"path"`
	DSN    string `yaml:"dsn"`
}

// BackupConfig controls scheduled encrypted backups.
type BackupConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Dir      string   `yaml:"dir"`
	Interval Duration `yaml:"interval"`
	Keep     int      `yaml:"keep"`
}

// KeysConfig controls the root key provider.
type KeysConfig struct {
	Provider    string `yaml:"provider"` // file | env | aws-kms | gcp-kms
	RootKeyFile string `yaml:"root_key_file"`
	RootKeyEnv  string `yaml:"root_key_env"`

	AWSRegion    string `yaml:"aws_region"`
	AWSKeyID     string `yaml:"aws_kms_key_id"`
	AWSAccessKey string `yaml:"aws_access_key"`
	AWSSecretKey string `yaml:"aws_secret_key"`
	AWSSession   string `yaml:"aws_session_token"`
	AWSEndpoint  string `yaml:"aws_kms_endpoint"`

	GCPKey      string `yaml:"gcp_kms_key"`
	GCPKeyFile  string `yaml:"gcp_service_account_file"`
	GCPEndpoint string `yaml:"gcp_kms_endpoint"`
}

// LogConfig controls structured logging.
type LogConfig struct {
	Level  string `yaml:"level"`  // debug | info | warn | error
	Format string `yaml:"format"` // json | console
}

// Default returns the built-in defaults.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Addr:               ":8080",
			Dev:                false,
			ReadTimeout:        Duration(15 * time.Second),
			WriteTimeout:       Duration(30 * time.Second),
			IdleTimeout:        Duration(120 * time.Second),
			ShutdownTimeout:    Duration(20 * time.Second),
			MaxBodyBytes:       1 << 20, // 1 MiB
			MetricsPublic:      true,
			RateLimitPerMinute: 600,
		},
		Database: DatabaseConfig{Driver: "sqlite"},
		Backup:   BackupConfig{Interval: Duration(24 * time.Hour), Keep: 7},
		Keys: KeysConfig{
			Provider:   "file",
			RootKeyEnv: "CLAVIS_ROOT_KEY",
		},
		Log:      LogConfig{Level: "info", Format: "json"},
		Email:    EmailConfig{Port: 587, TLS: "starttls"},
		Security: SecurityConfig{BreachCheckURL: "https://api.pwnedpasswords.com/range/"},
	}
}

// Load builds configuration from defaults, an optional YAML file, and
// environment overrides. An explicit path that does not exist is an error; a
// path supplied only via CLAVIS_CONFIG is also required to exist.
func Load(explicitPath string) (*Config, error) {
	cfg := Default()

	path := explicitPath
	if path == "" {
		path = os.Getenv("CLAVIS_CONFIG")
	}
	if path != "" {
		b, err := os.ReadFile(path) //nolint:gosec // G703: the config path comes from the operator (flag or CLAVIS_CONFIG)
		if err != nil {
			return nil, fmt.Errorf("config: read %s: %w", path, err)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(b))
		decoder.KnownFields(true)
		if err := decoder.Decode(cfg); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
	}

	if err := applyEnv(cfg); err != nil {
		return nil, err
	}
	cfg.applyDerived()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyDerived() {
	if c.Database.Path == "" {
		c.Database.Path = filepath.Join(c.dataDir(), "clavis.db")
	}
	if c.Keys.RootKeyFile == "" {
		c.Keys.RootKeyFile = filepath.Join(c.dataDir(), "root.key")
	}
	if c.Backup.Dir == "" {
		c.Backup.Dir = filepath.Join(c.dataDir(), "backups")
	}
	if c.Server.TLS.Mode == "" {
		c.Server.TLS.Mode = TLSOff
	}
	if c.Server.TLS.Addr == "" {
		c.Server.TLS.Addr = ":8443"
	}
	if c.Server.TLS.Dir == "" {
		c.Server.TLS.Dir = filepath.Join(c.dataDir(), "tls")
	}
	if c.Server.BaseURL == "" {
		addr := c.Server.Addr
		scheme := "http://"
		if c.Server.TLS.Enabled() {
			addr, scheme = c.Server.TLS.Addr, "https://"
		}
		if strings.HasPrefix(addr, ":") {
			addr = "localhost" + addr
		}
		if strings.HasPrefix(addr, "0.0.0.0:") {
			addr = "localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
		}
		c.Server.BaseURL = scheme + addr
	}
	c.Server.BaseURL = strings.TrimRight(c.Server.BaseURL, "/")
	if c.Server.Dev && os.Getenv("CLAVIS_LOG_FORMAT") == "" && c.Log.Format == "json" {
		c.Log.Format = "console"
	}
}

func (c *Config) dataDir() string {
	if v := os.Getenv("CLAVIS_DATA_DIR"); v != "" {
		return v
	}
	return "./data"
}

func applyEnv(c *Config) error {
	setString := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok {
			*dst = v
		}
	}
	setBool := func(key string, dst *bool) error {
		v, ok := os.LookupEnv(key)
		if !ok {
			return nil
		}
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("config: %s must be true or false: %w", key, err)
		}
		*dst = parsed
		return nil
	}
	setDuration := func(key string, dst *Duration) error {
		v, ok := os.LookupEnv(key)
		if !ok {
			return nil
		}
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("config: %s must be a duration like 15s: %w", key, err)
		}
		*dst = Duration(parsed)
		return nil
	}
	setInt := func(key string, dst *int) error {
		v, ok := os.LookupEnv(key)
		if !ok {
			return nil
		}
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("config: %s must be an integer: %w", key, err)
		}
		*dst = parsed
		return nil
	}
	setInt64 := func(key string, dst *int64) error {
		v, ok := os.LookupEnv(key)
		if !ok {
			return nil
		}
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("config: %s must be an integer: %w", key, err)
		}
		*dst = parsed
		return nil
	}

	setString("CLAVIS_SERVER_ADDR", &c.Server.Addr)
	setString("CLAVIS_SERVER_BASE_URL", &c.Server.BaseURL)
	setString("CLAVIS_DB_DRIVER", &c.Database.Driver)
	setString("CLAVIS_DB_PATH", &c.Database.Path)
	setString("CLAVIS_DB_DSN", &c.Database.DSN)
	setString("CLAVIS_BACKUP_DIR", &c.Backup.Dir)
	setString("CLAVIS_KEY_PROVIDER", &c.Keys.Provider)
	setString("CLAVIS_ROOT_KEY_FILE", &c.Keys.RootKeyFile)
	setString("CLAVIS_ROOT_KEY_ENV", &c.Keys.RootKeyEnv)
	setString("CLAVIS_AWS_REGION", &c.Keys.AWSRegion)
	setString("CLAVIS_AWS_KMS_KEY_ID", &c.Keys.AWSKeyID)
	setString("CLAVIS_AWS_ACCESS_KEY", &c.Keys.AWSAccessKey)
	setString("CLAVIS_AWS_SECRET_KEY", &c.Keys.AWSSecretKey)
	setString("CLAVIS_AWS_SESSION_TOKEN", &c.Keys.AWSSession)
	setString("CLAVIS_AWS_KMS_ENDPOINT", &c.Keys.AWSEndpoint)
	setString("CLAVIS_GCP_KMS_KEY", &c.Keys.GCPKey)
	setString("CLAVIS_GCP_SERVICE_ACCOUNT_FILE", &c.Keys.GCPKeyFile)
	setString("CLAVIS_GCP_KMS_ENDPOINT", &c.Keys.GCPEndpoint)
	setString("CLAVIS_LOG_LEVEL", &c.Log.Level)
	setString("CLAVIS_LOG_FORMAT", &c.Log.Format)
	setString("CLAVIS_EMAIL_HOST", &c.Email.Host)
	setString("CLAVIS_EMAIL_USERNAME", &c.Email.Username)
	setString("CLAVIS_EMAIL_PASSWORD", &c.Email.Password)
	setString("CLAVIS_EMAIL_FROM", &c.Email.From)
	setString("CLAVIS_EMAIL_TLS", &c.Email.TLS)
	setString("CLAVIS_BREACH_CHECK_URL", &c.Security.BreachCheckURL)
	setString("CLAVIS_TLS", &c.Server.TLS.Mode)
	setString("CLAVIS_TLS_ADDR", &c.Server.TLS.Addr)
	setString("CLAVIS_TLS_CERT_FILE", &c.Server.TLS.CertFile)
	setString("CLAVIS_TLS_KEY_FILE", &c.Server.TLS.KeyFile)
	setString("CLAVIS_TLS_DIR", &c.Server.TLS.Dir)
	setString("CLAVIS_ACME_EMAIL", &c.Server.TLS.ACMEEmail)
	setString("CLAVIS_ACME_DIRECTORY", &c.Server.TLS.ACMEDirectory)
	setString("CLAVIS_ACME_DNS", &c.Server.TLS.ACMEDNS)
	setString("CLAVIS_ACME_DNS_TOKEN", &c.Server.TLS.ACMEDNSToken)
	setString("CLAVIS_ACME_DNS_TOKEN_FILE", &c.Server.TLS.ACMEDNSTokenFile)
	if v, ok := os.LookupEnv("CLAVIS_ACME_DNS_RESOLVERS"); ok {
		c.Server.TLS.ACMEDNSResolvers = nil
		for _, r := range strings.Split(v, ",") {
			if r = strings.TrimSpace(r); r != "" {
				c.Server.TLS.ACMEDNSResolvers = append(c.Server.TLS.ACMEDNSResolvers, r)
			}
		}
	}
	setString("CLAVIS_SETUP_TOKEN", &c.Server.Setup.Token)
	setString("CLAVIS_SETUP_OWNER_EMAIL", &c.Server.Setup.OwnerEmail)
	if v, ok := os.LookupEnv("CLAVIS_ACME_DOMAINS"); ok {
		c.Server.TLS.ACMEDomains = nil
		for _, d := range strings.Split(v, ",") {
			if d = strings.TrimSpace(d); d != "" {
				c.Server.TLS.ACMEDomains = append(c.Server.TLS.ACMEDomains, d)
			}
		}
	}
	if v, ok := os.LookupEnv("CLAVIS_OUTBOUND_ALLOWLIST"); ok {
		c.Security.OutboundAllowlist = nil
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.Security.OutboundAllowlist = append(c.Security.OutboundAllowlist, p)
			}
		}
	}
	if v, ok := os.LookupEnv("CLAVIS_TRUSTED_PROXIES"); ok {
		c.Server.TrustedProxies = nil
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.Server.TrustedProxies = append(c.Server.TrustedProxies, p)
			}
		}
	}

	for _, err := range []error{
		setBool("CLAVIS_DEV", &c.Server.Dev),
		setBool("CLAVIS_BACKUP_ENABLED", &c.Backup.Enabled),
		setBool("CLAVIS_METRICS_PUBLIC", &c.Server.MetricsPublic),
		setDuration("CLAVIS_READ_TIMEOUT", &c.Server.ReadTimeout),
		setDuration("CLAVIS_WRITE_TIMEOUT", &c.Server.WriteTimeout),
		setDuration("CLAVIS_IDLE_TIMEOUT", &c.Server.IdleTimeout),
		setDuration("CLAVIS_SHUTDOWN_TIMEOUT", &c.Server.ShutdownTimeout),
		setDuration("CLAVIS_ACME_DNS_WAIT", &c.Server.TLS.ACMEDNSWait),
		setDuration("CLAVIS_BACKUP_INTERVAL", &c.Backup.Interval),
		setInt64("CLAVIS_MAX_BODY_BYTES", &c.Server.MaxBodyBytes),
		setBool("CLAVIS_EMAIL_ENABLED", &c.Email.Enabled),
		setInt("CLAVIS_EMAIL_PORT", &c.Email.Port),
		setInt("CLAVIS_BACKUP_KEEP", &c.Backup.Keep),
		setInt("CLAVIS_RATE_LIMIT_PER_MINUTE", &c.Server.RateLimitPerMinute),
		setBool("CLAVIS_BREACH_CHECK", &c.Security.BreachCheck),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

// Validate checks configuration and returns actionable errors.
func (c *Config) Validate() error {
	if c.Server.Addr == "" {
		return fmt.Errorf("config: server.addr is required")
	}
	if c.Server.MaxBodyBytes < 1024 {
		return fmt.Errorf("config: server.max_body_bytes must be at least 1024")
	}
	if c.Server.MaxBodyBytes > 1<<30 {
		return fmt.Errorf("config: server.max_body_bytes must be at most 1 GiB")
	}
	for _, p := range c.Server.TrustedProxies {
		if _, err := ParseProxyPrefix(p); err != nil {
			return fmt.Errorf("config: server.trusted_proxies: %w", err)
		}
	}
	for _, p := range c.Security.OutboundAllowlist {
		if _, err := ParseProxyPrefix(p); err != nil {
			return fmt.Errorf("config: security.outbound_allowlist: %w", err)
		}
	}
	if c.Server.RateLimitPerMinute < 0 {
		return fmt.Errorf("config: server.rate_limit_per_minute cannot be negative")
	}
	for i, exporter := range c.Audit.Exporters {
		switch exporter.Type {
		case "webhook", "syslog", "s3", "datadog", "splunk", "elastic":
		default:
			return fmt.Errorf("config: audit.exporters[%d].type %q is not supported", i, exporter.Type)
		}
		if exporter.Format != "" && exporter.Format != "json" && exporter.Format != "cef" {
			return fmt.Errorf("config: audit.exporters[%d].format must be json or cef", i)
		}
		switch exporter.Type {
		case "syslog":
			if exporter.Address == "" {
				return fmt.Errorf("config: audit.exporters[%d].address is required for syslog", i)
			}
		case "s3":
			if exporter.Bucket == "" || exporter.Region == "" {
				return fmt.Errorf("config: audit.exporters[%d] requires bucket and region", i)
			}
			if exporter.AccessKey == "" || exporter.SecretKey == "" {
				return fmt.Errorf("config: audit.exporters[%d] requires access_key and secret_key", i)
			}
		default:
			if exporter.URL == "" {
				return fmt.Errorf("config: audit.exporters[%d].url is required", i)
			}
		}
	}
	if c.Security.BreachCheck {
		u, err := url.Parse(c.Security.BreachCheckURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("config: security.breach_check_url must be an absolute http(s) URL")
		}
	}
	for name, d := range map[string]Duration{
		"read_timeout":     c.Server.ReadTimeout,
		"write_timeout":    c.Server.WriteTimeout,
		"idle_timeout":     c.Server.IdleTimeout,
		"shutdown_timeout": c.Server.ShutdownTimeout,
	} {
		if d.Duration() <= 0 {
			return fmt.Errorf("config: server.%s must be positive", name)
		}
	}
	u, err := url.Parse(c.Server.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("config: server.base_url %q is not a valid absolute URL", c.Server.BaseURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("config: server.base_url scheme must be http or https")
	}
	if err := c.validateTLS(u); err != nil {
		return err
	}
	if t := c.Server.Setup.Token; t != "" && len(t) < 24 {
		return fmt.Errorf("config: CLAVIS_SETUP_TOKEN must be at least 24 characters (generate one with: openssl rand -base64 32)")
	}
	if e := c.Server.Setup.OwnerEmail; e != "" && !strings.Contains(e, "@") {
		return fmt.Errorf("config: CLAVIS_SETUP_OWNER_EMAIL %q is not an email address", e)
	}
	if c.Backup.Enabled {
		if c.Backup.Interval.Duration() <= 0 {
			return fmt.Errorf("config: backup.interval must be positive")
		}
		if c.Backup.Keep < 1 {
			return fmt.Errorf("config: backup.keep must be at least 1")
		}
		if c.Backup.Dir == "" {
			return fmt.Errorf("config: backup.dir is required when backups are enabled")
		}
	}
	switch c.Database.Driver {
	case "sqlite":
		if c.Database.Path == "" {
			return fmt.Errorf("config: database.path is required for sqlite")
		}
	case "postgres":
		if c.Database.DSN == "" {
			return fmt.Errorf("config: database.dsn is required for postgres")
		}
	default:
		return fmt.Errorf("config: unknown database.driver %q (supported: sqlite, postgres)", c.Database.Driver)
	}
	switch c.Keys.Provider {
	case "file":
		if c.Keys.RootKeyFile == "" {
			return fmt.Errorf("config: keys.root_key_file is required for the file provider")
		}
	case "env":
		if c.Keys.RootKeyEnv == "" {
			return fmt.Errorf("config: keys.root_key_env is required for the env provider")
		}
	case "aws-kms":
		if c.Keys.RootKeyFile == "" || c.Keys.AWSRegion == "" || c.Keys.AWSKeyID == "" || c.Keys.AWSAccessKey == "" || c.Keys.AWSSecretKey == "" {
			return fmt.Errorf("config: aws-kms requires root_key_file, aws_region, aws_kms_key_id, aws_access_key, and aws_secret_key")
		}
	case "gcp-kms":
		if c.Keys.RootKeyFile == "" || c.Keys.GCPKey == "" || c.Keys.GCPKeyFile == "" {
			return fmt.Errorf("config: gcp-kms requires root_key_file, gcp_kms_key, and gcp_service_account_file")
		}
	default:
		return fmt.Errorf("config: unknown keys.provider %q (supported: file, env, aws-kms, gcp-kms)", c.Keys.Provider)
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: log.level must be debug, info, warn, or error")
	}
	switch c.Log.Format {
	case "json", "console":
	default:
		return fmt.Errorf("config: log.format must be json or console")
	}
	if c.Email.Enabled {
		if c.Email.Host == "" {
			return fmt.Errorf("config: email.host is required when email is enabled")
		}
		if c.Email.Port < 1 || c.Email.Port > 65535 {
			return fmt.Errorf("config: email.port must be between 1 and 65535")
		}
		if c.Email.From == "" {
			return fmt.Errorf("config: email.from is required when email is enabled")
		}
		switch c.Email.TLS {
		case "starttls", "none", "tls":
		default:
			return fmt.Errorf("config: email.tls must be starttls, tls, or none")
		}
	}
	return nil
}

// ParseProxyPrefix parses a trusted proxy entry, either a CIDR ("10.0.0.0/8")
// or a single address ("192.0.2.10").
func ParseProxyPrefix(s string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(s); err == nil {
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%q is not an IP address or CIDR", s)
	}
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

func (c *Config) validateTLS(base *url.URL) error {
	t := c.Server.TLS
	switch t.Mode {
	case TLSOff:
		return nil
	case TLSSelfSigned, TLSFile, TLSACME:
	default:
		return fmt.Errorf("config: CLAVIS_TLS must be off, self-signed, file, or acme (got %q)", t.Mode)
	}
	if base.Scheme != "https" {
		return fmt.Errorf("config: CLAVIS_TLS=%s needs an https:// base URL; set CLAVIS_SERVER_BASE_URL=https://%s", t.Mode, base.Hostname())
	}
	if t.Addr == c.Server.Addr {
		return fmt.Errorf("config: the HTTPS address %s must differ from server.addr", t.Addr)
	}
	switch t.Mode {
	case TLSFile:
		if t.CertFile == "" || t.KeyFile == "" {
			return fmt.Errorf("config: CLAVIS_TLS=file needs CLAVIS_TLS_CERT_FILE and CLAVIS_TLS_KEY_FILE")
		}
	case TLSACME:
		host := base.Hostname()
		if len(t.ACMEDomains) == 0 && (host == "localhost" || net.ParseIP(host) != nil) {
			return fmt.Errorf("config: CLAVIS_TLS=acme needs a public domain name in the base URL, not %q; use self-signed for an IP address", host)
		}
		return c.validateACMEDNS(host)
	}
	return nil
}

func (c *Config) validateACMEDNS(baseHost string) error {
	t := &c.Server.TLS
	domains := t.ACMEDomains
	if len(domains) == 0 {
		domains = []string{baseHost}
	}
	for _, d := range domains {
		if net.ParseIP(d) != nil || d == "localhost" {
			return fmt.Errorf("config: CLAVIS_ACME_DOMAINS has %q; Let's Encrypt only issues certificates for domain names", d)
		}
		if strings.HasPrefix(d, "*.") && t.ACMEDNS == "" {
			return fmt.Errorf("config: the wildcard %q needs the DNS challenge; set CLAVIS_ACME_DNS (cloudflare or duckdns)", d)
		}
	}
	if t.ACMEDNS == "" {
		return nil
	}
	switch t.ACMEDNS {
	case ACMEDNSCloudflare:
	case ACMEDNSDuckDNS:
		for _, d := range domains {
			if !strings.HasSuffix(strings.TrimPrefix(d, "*."), ".duckdns.org") {
				return fmt.Errorf("config: CLAVIS_ACME_DNS=duckdns only works for names under duckdns.org, not %q", d)
			}
		}
	default:
		return fmt.Errorf("config: CLAVIS_ACME_DNS must be cloudflare or duckdns (got %q); for other DNS providers put Caddy or Traefik in front, see the HTTPS guide", t.ACMEDNS)
	}
	if t.ACMEDNSToken == "" && t.ACMEDNSTokenFile != "" {
		data, err := os.ReadFile(t.ACMEDNSTokenFile)
		if err != nil {
			return fmt.Errorf("config: CLAVIS_ACME_DNS_TOKEN_FILE: %w", err)
		}
		t.ACMEDNSToken = strings.TrimSpace(string(data))
	}
	if t.ACMEDNSToken == "" {
		return fmt.Errorf("config: CLAVIS_ACME_DNS=%s needs CLAVIS_ACME_DNS_TOKEN (or CLAVIS_ACME_DNS_TOKEN_FILE)", t.ACMEDNS)
	}
	if t.ACMEDNSWait <= 0 {
		t.ACMEDNSWait = Duration(3 * time.Minute)
	}
	return nil
}
