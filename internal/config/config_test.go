package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func yamlNode(s string) *yaml.Node {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(s), &n); err != nil {
		panic(err)
	}
	return n.Content[0]
}

func TestDefaults(t *testing.T) {
	os.Unsetenv("CLAVIS_CONFIG")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":8080" {
		t.Fatalf("addr = %q", cfg.Server.Addr)
	}
	if cfg.Server.BaseURL != "http://localhost:8080" {
		t.Fatalf("base url = %q", cfg.Server.BaseURL)
	}
	if cfg.Database.Path != filepath.Join("data", "clavis.db") {
		t.Fatalf("db path = %q", cfg.Database.Path)
	}
	if cfg.Keys.RootKeyFile != filepath.Join("data", "root.key") {
		t.Fatalf("key file = %q", cfg.Keys.RootKeyFile)
	}
}

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clavis.yaml")
	content := `
server:
  addr: "127.0.0.1:9999"
  base_url: "https://clavis.example.com/"
  read_timeout: 3s
  write_timeout: 4
  max_body_bytes: 2048
database:
  path: /tmp/custom.db
keys:
  provider: env
  root_key_env: MY_KEY
log:
  level: debug
  format: console
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != "127.0.0.1:9999" || cfg.Server.BaseURL != "https://clavis.example.com" {
		t.Fatalf("server: %+v", cfg.Server)
	}
	if cfg.Server.ReadTimeout.Duration().Seconds() != 3 || cfg.Server.WriteTimeout.Duration().Seconds() != 4 {
		t.Fatalf("durations: %v %v", cfg.Server.ReadTimeout, cfg.Server.WriteTimeout)
	}
	if cfg.Database.Path != "/tmp/custom.db" || cfg.Keys.Provider != "env" || cfg.Keys.RootKeyEnv != "MY_KEY" {
		t.Fatalf("config: %+v", cfg)
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "console" {
		t.Fatalf("log: %+v", cfg.Log)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("CLAVIS_SERVER_ADDR", ":9000")
	t.Setenv("CLAVIS_SERVER_BASE_URL", "https://env.example.com")
	t.Setenv("CLAVIS_DB_PATH", "/tmp/env.db")
	t.Setenv("CLAVIS_KEY_PROVIDER", "env")
	t.Setenv("CLAVIS_ROOT_KEY_ENV", "ROOT_KEY")
	t.Setenv("CLAVIS_LOG_LEVEL", "warn")
	t.Setenv("CLAVIS_LOG_FORMAT", "json")
	t.Setenv("CLAVIS_DEV", "true")
	t.Setenv("CLAVIS_MAX_BODY_BYTES", "4096")
	t.Setenv("CLAVIS_READ_TIMEOUT", "7s")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":9000" || cfg.Server.BaseURL != "https://env.example.com" {
		t.Fatalf("server: %+v", cfg.Server)
	}
	if cfg.Database.Path != "/tmp/env.db" || cfg.Keys.Provider != "env" || cfg.Keys.RootKeyEnv != "ROOT_KEY" {
		t.Fatalf("config: %+v", cfg)
	}
	if cfg.Log.Level != "warn" || cfg.Log.Format != "json" || !cfg.Server.Dev {
		t.Fatalf("misc: %+v %+v", cfg.Log, cfg.Server)
	}
	if cfg.Server.MaxBodyBytes != 4096 || cfg.Server.ReadTimeout.Duration().Seconds() != 7 {
		t.Fatalf("numeric: %+v", cfg.Server)
	}
}

func TestDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAVIS_DATA_DIR", dir)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Path != filepath.Join(dir, "clavis.db") || cfg.Keys.RootKeyFile != filepath.Join(dir, "root.key") {
		t.Fatalf("derived paths: %q %q", cfg.Database.Path, cfg.Keys.RootKeyFile)
	}
}

func TestConfigPathFromEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("log:\n  level: error\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAVIS_CONFIG", path)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Log.Level != "error" {
		t.Fatalf("level = %q", cfg.Log.Level)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing config accepted")
	}

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	_ = os.WriteFile(bad, []byte("server:\n  read_timeout: not-a-duration\n"), 0o600)
	if _, err := Load(bad); err == nil {
		t.Fatal("bad duration accepted")
	}

	badInt := filepath.Join(t.TempDir(), "badint.yaml")
	_ = os.WriteFile(badInt, []byte("server:\n  read_timeout: {}\n"), 0o600)
	if _, err := Load(badInt); err == nil {
		t.Fatal("bad duration type accepted")
	}

	unknown := filepath.Join(t.TempDir(), "unknown.yaml")
	_ = os.WriteFile(unknown, []byte("server:\n  read_timout: 3s\n"), 0o600)
	if _, err := Load(unknown); err == nil {
		t.Fatal("unknown config key accepted")
	}
}

func TestEnvErrors(t *testing.T) {
	cases := map[string]string{
		"CLAVIS_DEV":              "maybe",
		"CLAVIS_METRICS_PUBLIC":   "nope",
		"CLAVIS_READ_TIMEOUT":     "soon",
		"CLAVIS_WRITE_TIMEOUT":    "soon",
		"CLAVIS_IDLE_TIMEOUT":     "soon",
		"CLAVIS_SHUTDOWN_TIMEOUT": "soon",
		"CLAVIS_MAX_BODY_BYTES":   "lots",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := Load(""); err == nil {
				t.Fatal("invalid env accepted")
			}
		})
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]func(*Config){
		"empty addr":        func(c *Config) { c.Server.Addr = "" },
		"small body":        func(c *Config) { c.Server.MaxBodyBytes = 1 },
		"huge body":         func(c *Config) { c.Server.MaxBodyBytes = 1 << 31 },
		"zero read":         func(c *Config) { c.Server.ReadTimeout = 0 },
		"zero write":        func(c *Config) { c.Server.WriteTimeout = 0 },
		"zero idle":         func(c *Config) { c.Server.IdleTimeout = 0 },
		"zero shutdown":     func(c *Config) { c.Server.ShutdownTimeout = 0 },
		"relative base url": func(c *Config) { c.Server.BaseURL = "/relative" },
		"bad scheme":        func(c *Config) { c.Server.BaseURL = "ftp://example.com" },
		"unknown db":        func(c *Config) { c.Database.Driver = "mysql" },
		"postgres":          func(c *Config) { c.Database.Driver = "postgres" },
		"empty db path":     func(c *Config) { c.Database.Path = "" },
		"unknown provider":  func(c *Config) { c.Keys.Provider = "kms" },
		"empty key file":    func(c *Config) { c.Keys.Provider = "file"; c.Keys.RootKeyFile = "" },
		"empty key env":     func(c *Config) { c.Keys.Provider = "env"; c.Keys.RootKeyEnv = "" },
		"bad log level":     func(c *Config) { c.Log.Level = "trace" },
		"bad log format":    func(c *Config) { c.Log.Format = "xml" },
		"email no host":     func(c *Config) { c.Email.Enabled = true; c.Email.From = "x@example.com" },
		"email no from":     func(c *Config) { c.Email.Enabled = true; c.Email.Host = "smtp.example.com" },
		"email bad port": func(c *Config) {
			c.Email.Enabled = true
			c.Email.Host = "smtp.example.com"
			c.Email.From = "x@example.com"
			c.Email.Port = 0
		},
		"email bad tls": func(c *Config) {
			c.Email.Enabled = true
			c.Email.Host = "smtp.example.com"
			c.Email.From = "x@example.com"
			c.Email.TLS = "ssl"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			cfg.applyDerived()
			mutate(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestBaseURLDerivation(t *testing.T) {
	cases := map[string]string{
		":8080":       "http://localhost:8080",
		"0.0.0.0:80":  "http://localhost:80",
		"127.0.0.1:9": "http://127.0.0.1:9",
	}
	for addr, want := range cases {
		cfg := Default()
		cfg.Server.Addr = addr
		cfg.applyDerived()
		if cfg.Server.BaseURL != want {
			t.Errorf("addr %q -> %q, want %q", addr, cfg.Server.BaseURL, want)
		}
	}
}

func TestDurationRoundTrip(t *testing.T) {
	var d Duration
	if err := d.UnmarshalYAML(yamlNode("90s")); err != nil {
		t.Fatal(err)
	}
	if d.Duration().Seconds() != 90 || d.String() != "1m30s" {
		t.Fatalf("duration = %v", d)
	}
	if _, err := d.MarshalYAML(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.String(), "1m") {
		t.Fatal("unexpected string")
	}
}
