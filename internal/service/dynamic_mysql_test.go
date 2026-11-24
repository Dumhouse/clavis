package service

import (
	"context"
	"database/sql"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestValidateMySQLDynamic(t *testing.T) {
	valid := map[string]any{
		"host": "localhost", "database": "app", "username": "admin", "password": "secret",
	}
	if err := validateMySQLDynamic(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := validateMySQLDynamic(map[string]any{
		"host": "localhost", "database": "app", "username": "admin", "password": "secret",
		"grant_privileges": "SELECT, INSERT", "tls": "skip-verify",
	}); err != nil {
		t.Fatalf("valid config with options rejected: %v", err)
	}
	for _, key := range []string{"host", "database", "username", "password"} {
		config := map[string]any{
			"host": "localhost", "database": "app", "username": "admin", "password": "secret",
		}
		delete(config, key)
		if err := validateMySQLDynamic(config); err == nil {
			t.Errorf("missing %s accepted", key)
		}
	}
	if err := validateMySQLDynamic(map[string]any{
		"host": "localhost", "database": "app", "username": "admin", "password": "secret",
		"grant_privileges": "SELECT; DROP DATABASE app",
	}); err == nil {
		t.Error("unsafe privileges accepted")
	}
	if err := validateMySQLDynamic(map[string]any{
		"host": "localhost", "database": "app", "username": "admin", "password": "secret",
		"tls": "bogus",
	}); err == nil {
		t.Error("unsupported TLS mode accepted")
	}
}

func TestMySQLStatements(t *testing.T) {
	config := &mysqlDynamicConfig{Database: "app", GrantPrivileges: "SELECT, INSERT"}
	if got := mysqlCreateUserStatement(config, "clavis_abc", `pa'ss\word`, time.Time{}); got != `CREATE USER 'clavis_abc'@'%' IDENTIFIED BY 'pa''ss\\word'` {
		t.Fatalf("create = %s", got)
	}
	if got := mysqlGrantStatement(config, "clavis_abc"); got != "GRANT SELECT, INSERT ON `app`.* TO 'clavis_abc'@'%'" {
		t.Fatalf("grant = %s", got)
	}
	if got := mysqlRevokeStatement(config, "clavis_abc"); got != "DROP USER IF EXISTS 'clavis_abc'@'%'" {
		t.Fatalf("revoke = %s", got)
	}

	empty := &mysqlDynamicConfig{Database: "app"}
	if got := mysqlGrantStatement(empty, "u"); got != "GRANT SELECT ON `app`.* TO 'u'@'%'" {
		t.Fatalf("default grant = %s", got)
	}
	templated := &mysqlDynamicConfig{
		Database:        "app",
		RoleTemplate:    "CREATE USER {{quoted_name}} IDENTIFIED BY {{password}} PASSWORD EXPIRE INTERVAL 1 DAY",
		GrantTemplate:   "GRANT {{privileges}} ON {{database}}.* TO {{quoted_name}}",
		RevokeTemplate:  "DROP USER IF EXISTS {{quoted_name}}",
		GrantPrivileges: "SELECT",
	}
	if got := mysqlCreateUserStatement(templated, "u", "p", time.Time{}); !strings.Contains(got, "PASSWORD EXPIRE INTERVAL 1 DAY") {
		t.Fatalf("custom template = %s", got)
	}
	if got := mysqlQuotedIdentifier("we`ird"); got != "`we``ird`" {
		t.Fatalf("identifier = %s", got)
	}
}

// mysqlTestConfig reads CLAVIS_TEST_MYSQL, a go-sql-driver DSN such as
// root:clavis@tcp(127.0.0.1:3306)/clavis_test.
func mysqlTestConfig(t *testing.T) *mysqlDynamicConfig {
	t.Helper()
	dsn := os.Getenv("CLAVIS_TEST_MYSQL")
	if dsn == "" {
		t.Skip("set CLAVIS_TEST_MYSQL to run MySQL integration tests")
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse CLAVIS_TEST_MYSQL: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Addr)
	if err != nil {
		t.Fatalf("CLAVIS_TEST_MYSQL address %q: %v", parsed.Addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("CLAVIS_TEST_MYSQL port: %v", err)
	}
	return &mysqlDynamicConfig{
		Host: host, Port: port, Database: parsed.DBName,
		Username: parsed.User, Password: parsed.Passwd,
		GrantPrivileges: "SELECT",
	}
}

func mysqlConnect(base *mysqlDynamicConfig, user, password string) (*sql.DB, error) {
	port := base.Port
	if port == 0 {
		port = 3306
	}
	dsn := mysql.NewConfig()
	dsn.User = user
	dsn.Passwd = password
	dsn.Net = "tcp"
	dsn.Addr = net.JoinHostPort(base.Host, strconv.Itoa(port))
	dsn.DBName = base.Database
	dsn.Timeout = 5 * time.Second
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func TestMySQLDynamicCredentials(t *testing.T) {
	config := mysqlTestConfig(t)
	ctx := context.Background()
	service := &Service{}

	data, err := service.createMySQLCredentials(ctx, "11111111-2222-3333-4444-555555555555",
		time.Now().Add(time.Hour), config)
	if err != nil {
		t.Fatal(err)
	}
	username := data["username"]
	if data["engine"] != "mysql" || data["password"] == "" || data["port"] != strconv.Itoa(config.Port) {
		t.Fatalf("lease data = %#v", data)
	}

	db, err := mysqlConnect(config, username, data["password"])
	if err != nil {
		t.Fatalf("generated user cannot log in: %v", err)
	}
	var grants string
	if err := db.QueryRowContext(ctx, "SHOW GRANTS FOR CURRENT_USER()").Scan(&grants); err != nil {
		t.Fatalf("show grants: %v", err)
	}
	_ = db.Close()
	if !strings.Contains(grants, "SELECT") {
		t.Fatalf("grants = %q", grants)
	}

	service.revokeMySQLUser(ctx, config, username)
	if db, err := mysqlConnect(config, username, data["password"]); err == nil {
		_ = db.Close()
		t.Fatal("revoked user can still log in")
	}
}

func TestMySQLRotation(t *testing.T) {
	config := mysqlTestConfig(t)
	ctx := context.Background()
	service := &Service{}

	admin, err := mysqlConnect(config, config.Username, config.Password)
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}
	defer func() { _ = admin.Close() }()
	role := "clavis_rot_test"
	_, _ = admin.ExecContext(ctx, "DROP USER IF EXISTS '"+role+"'@'%'")
	if _, err := admin.ExecContext(ctx, "CREATE USER '"+role+"'@'%' IDENTIFIED BY 'old-password'"); err != nil {
		t.Fatalf("create role: %v", err)
	}
	defer func() { _, _ = admin.ExecContext(context.Background(), "DROP USER IF EXISTS '"+role+"'@'%'") }()

	rotation := rotationMySQLConfig{
		Host: config.Host, Port: config.Port, Database: config.Database,
		Username: config.Username, Password: config.Password, Role: role,
	}
	if err := service.setMySQLPassword(ctx, rotation, "new-password"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if db, err := mysqlConnect(config, role, "old-password"); err == nil {
		_ = db.Close()
		t.Fatal("old password still works after rotation")
	}
	db, err := mysqlConnect(config, role, "new-password")
	if err != nil {
		t.Fatalf("new password does not work: %v", err)
	}
	_ = db.Close()
}
