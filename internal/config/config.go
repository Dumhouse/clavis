// Package config loads Clavis configuration from defaults, an optional YAML
// file, and environment variables (in increasing precedence).
package config

import (
	"bytes"
	"fmt"
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
}

// DatabaseConfig controls the data store.
type DatabaseConfig struct {
	Driver string `yaml:"driver"` // sqlite (postgres arrives in M3)
	Path   string `yaml:"path"`
}

// KeysConfig controls the root key provider.
type KeysConfig struct {
	Provider    string `yaml:"provider"` // file | env
	RootKeyFile string `yaml:"root_key_file"`
	RootKeyEnv  string `yaml:"root_key_env"`
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
	if c.Server.BaseURL == "" {
		addr := c.Server.Addr
		if strings.HasPrefix(addr, ":") {
			addr = "localhost" + addr
		}
		if strings.HasPrefix(addr, "0.0.0.0:") {
			addr = "localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
		}
		c.Server.BaseURL = "http://" + addr
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
	setString("CLAVIS_KEY_PROVIDER", &c.Keys.Provider)
	setString("CLAVIS_ROOT_KEY_FILE", &c.Keys.RootKeyFile)
	setString("CLAVIS_ROOT_KEY_ENV", &c.Keys.RootKeyEnv)
	setString("CLAVIS_LOG_LEVEL", &c.Log.Level)
	setString("CLAVIS_LOG_FORMAT", &c.Log.Format)
	setString("CLAVIS_EMAIL_HOST", &c.Email.Host)
	setString("CLAVIS_EMAIL_USERNAME", &c.Email.Username)
	setString("CLAVIS_EMAIL_PASSWORD", &c.Email.Password)
	setString("CLAVIS_EMAIL_FROM", &c.Email.From)
	setString("CLAVIS_EMAIL_TLS", &c.Email.TLS)
	setString("CLAVIS_BREACH_CHECK_URL", &c.Security.BreachCheckURL)
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
		setBool("CLAVIS_METRICS_PUBLIC", &c.Server.MetricsPublic),
		setDuration("CLAVIS_READ_TIMEOUT", &c.Server.ReadTimeout),
		setDuration("CLAVIS_WRITE_TIMEOUT", &c.Server.WriteTimeout),
		setDuration("CLAVIS_IDLE_TIMEOUT", &c.Server.IdleTimeout),
		setDuration("CLAVIS_SHUTDOWN_TIMEOUT", &c.Server.ShutdownTimeout),
		setInt64("CLAVIS_MAX_BODY_BYTES", &c.Server.MaxBodyBytes),
		setBool("CLAVIS_EMAIL_ENABLED", &c.Email.Enabled),
		setInt("CLAVIS_EMAIL_PORT", &c.Email.Port),
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
	switch c.Database.Driver {
	case "sqlite":
	case "postgres":
		return fmt.Errorf("config: database.driver postgres is not supported until M3")
	default:
		return fmt.Errorf("config: unknown database.driver %q (supported: sqlite)", c.Database.Driver)
	}
	if c.Database.Path == "" {
		return fmt.Errorf("config: database.path is required")
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
	default:
		return fmt.Errorf("config: unknown keys.provider %q (supported: file, env)", c.Keys.Provider)
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
