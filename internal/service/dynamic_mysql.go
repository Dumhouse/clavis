package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/dumhouse/clavis/internal/store"
)

// mysqlDynamicConfig configures admin access to MySQL or MariaDB for issuing
// short-lived users.
type mysqlDynamicConfig struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Database        string `json:"database"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	TLS             string `json:"tls"`
	GrantPrivileges string `json:"grant_privileges"`
	RoleTemplate    string `json:"role_template,omitempty"`
	GrantTemplate   string `json:"grant_template,omitempty"`
	RevokeTemplate  string `json:"revoke_template,omitempty"`
}

func validateMySQLDynamic(config map[string]any) error {
	for _, key := range []string{"host", "database", "username", "password"} {
		if value, _ := config[key].(string); strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: mysql config needs %s", ErrValidation, key)
		}
	}
	if value, _ := config["grant_privileges"].(string); value != "" && !safePrivileges(value) {
		return fmt.Errorf("%w: grant_privileges must be a comma-separated list of privilege names", ErrValidation)
	}
	if value, _ := config["tls"].(string); !safeMySQLTLS(value) {
		return fmt.Errorf("%w: tls must be false, true, skip-verify, or preferred", ErrValidation)
	}
	return nil
}

// safePrivileges allows privilege lists such as "SELECT, INSERT" but no
// quoting, so it cannot break out of the GRANT statement.
func safePrivileges(value string) bool {
	letters := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letters = true
		case r == '_', r == ',', r == ' ':
		default:
			return false
		}
	}
	return letters
}

func safeMySQLTLS(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false", "true", "skip-verify", "preferred":
		return true
	}
	return false
}

func mysqlTLSName(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false":
		return ""
	case "true":
		return "true"
	case "skip-verify":
		return "skip-verify"
	case "preferred":
		return "preferred"
	default:
		return strings.TrimSpace(value)
	}
}

func (s *Service) mysqlAdminClient(config *mysqlDynamicConfig) (*sql.DB, error) {
	port := config.Port
	if port == 0 {
		port = 3306
	}
	dsn := mysql.NewConfig()
	dsn.User = config.Username
	dsn.Passwd = config.Password
	dsn.Net = "tcp"
	dsn.Addr = net.JoinHostPort(config.Host, strconv.Itoa(port))
	dsn.DBName = config.Database
	dsn.TLSConfig = mysqlTLSName(config.TLS)
	dsn.Timeout = 10 * time.Second
	dsn.ReadTimeout = 10 * time.Second
	dsn.WriteTimeout = 10 * time.Second
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("dynamic mysql: connect: %w", err)
	}
	return db, nil
}

func mysqlQuotedString(value string) string {
	return `'` + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(value) + `'`
}

func mysqlQuotedIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func mysqlQuotedUser(name string) string {
	return mysqlQuotedString(name) + "@'%'"
}

func mysqlPrivileges(config *mysqlDynamicConfig) string {
	privileges := strings.Join(strings.Fields(config.GrantPrivileges), " ")
	if privileges == "" {
		return "SELECT"
	}
	return privileges
}

// mysqlStatement fills a template. {{name}} is the raw user name, {{quoted_name}}
// is 'user'@'%', {{password}} is a quoted literal, {{database}} a quoted
// identifier, and {{privileges}} the configured privilege list.
func mysqlStatement(template string, config *mysqlDynamicConfig, name, password string, expires time.Time) string {
	expiry := ""
	if !expires.IsZero() {
		expiry = expires.UTC().Format(time.RFC3339)
	}
	return strings.NewReplacer(
		"{{quoted_name}}", mysqlQuotedUser(name),
		"{{name}}", name,
		"{{password}}", mysqlQuotedString(password),
		"{{database}}", mysqlQuotedIdentifier(config.Database),
		"{{privileges}}", mysqlPrivileges(config),
		"{{expires}}", expiry,
	).Replace(template)
}

func mysqlCreateUserStatement(config *mysqlDynamicConfig, name, password string, expires time.Time) string {
	template := config.RoleTemplate
	if template == "" {
		template = "CREATE USER {{quoted_name}} IDENTIFIED BY {{password}}"
	}
	return mysqlStatement(template, config, name, password, expires)
}

func mysqlGrantStatement(config *mysqlDynamicConfig, name string) string {
	template := config.GrantTemplate
	if template == "" {
		template = "GRANT {{privileges}} ON {{database}}.* TO {{quoted_name}}"
	}
	return mysqlStatement(template, config, name, "", time.Time{})
}

func mysqlRevokeStatement(config *mysqlDynamicConfig, name string) string {
	template := config.RevokeTemplate
	if template == "" {
		template = "DROP USER IF EXISTS {{quoted_name}}"
	}
	return mysqlStatement(template, config, name, "", time.Time{})
}

func (s *Service) createMySQLCredentials(ctx context.Context, leaseID string, expires time.Time, config *mysqlDynamicConfig) (map[string]string, error) {
	name := "clavis_" + strings.ReplaceAll(leaseID, "-", "")[:12]
	passwordBytes := make([]byte, 24)
	if _, err := rand.Read(passwordBytes); err != nil {
		return nil, err
	}
	password := base64.RawURLEncoding.EncodeToString(passwordBytes)
	db, err := s.mysqlAdminClient(config)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, mysqlCreateUserStatement(config, name, password, expires)); err != nil {
		return nil, fmt.Errorf("dynamic mysql: create user: %w", err)
	}
	if _, err := db.ExecContext(ctx, mysqlGrantStatement(config, name)); err != nil {
		_, _ = db.ExecContext(context.Background(), mysqlRevokeStatement(config, name))
		return nil, fmt.Errorf("dynamic mysql: grant privileges: %w", err)
	}
	port := config.Port
	if port == 0 {
		port = 3306
	}
	return map[string]string{
		"engine": "mysql", "username": name, "password": password,
		"host": config.Host, "port": strconv.Itoa(port),
		"database": config.Database,
	}, nil
}

// revokeMySQLUser drops a lease user. Revocation is best effort, like the
// Postgres path: the lease is marked revoked even when the server is down.
func (s *Service) revokeMySQLUser(ctx context.Context, config *mysqlDynamicConfig, username string) {
	db, err := s.mysqlAdminClient(config)
	if err != nil {
		return
	}
	defer func() { _ = db.Close() }()
	_, _ = db.ExecContext(ctx, mysqlRevokeStatement(config, username))
}

func (s *Service) decryptMySQLDynamicConfig(org *store.Org, config *store.DynamicSecretConfig) (*mysqlDynamicConfig, error) {
	raw, err := s.decryptOrgSecret(org, config.ID, "dynamic_config", config.Config)
	if err != nil {
		return nil, err
	}
	var out mysqlDynamicConfig
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
