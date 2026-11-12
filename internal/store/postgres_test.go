package store_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

func TestPostgresSmoke(t *testing.T) {
	dsn := os.Getenv("CLAVIS_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set CLAVIS_TEST_POSTGRES to run Postgres integration tests")
	}
	ctx := context.Background()
	st, err := store.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user := &store.User{ID: uuid.NewString(), Email: uuid.NewString() + "@example.com", Name: "Postgres User", EmailVerified: true}
	if err := st.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.GetUserByEmail(ctx, user.Email)
	if err != nil || loaded.ID != user.ID {
		t.Fatalf("get user = %+v %v", loaded, err)
	}
	if err := st.SetMFASecret(ctx, user.ID, []byte{0x01, 0x02}, true); err != nil {
		t.Fatal(err)
	}

	root, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	orgID := uuid.NewString()
	wrapped, _, err := crypto.NewKeyring(root).NewOrgKey(orgID)
	if err != nil {
		t.Fatal(err)
	}
	role := &store.Role{ID: uuid.NewString(), Name: "Owner", Permissions: "[]", Builtin: true}
	org := &store.Org{ID: orgID, Slug: "pg-" + orgID[:8], Name: "Postgres", Settings: "{}", WrappedOrgKey: wrapped, KeyVersion: 1}
	if err := st.CreateOrgWithOwner(ctx, org, []*store.Role{role}, user.ID, role.ID); err != nil {
		t.Fatal(err)
	}
	members, err := st.ListMembers(ctx, org.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("members = %+v %v", members, err)
	}
	project := &store.Project{ID: uuid.NewString(), OrgID: org.ID, Slug: "app", Name: "App", WrappedProjectKey: []byte{1, 2, 3}}
	if err := st.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	env := &store.Environment{ID: uuid.NewString(), ProjectID: project.ID, Slug: "dev", Name: "Development", Type: "development"}
	if err := st.CreateEnvironment(ctx, env); err != nil {
		t.Fatal(err)
	}
	item := &store.Item{
		ID: uuid.NewString(), EnvironmentID: env.ID, Kind: "secret", KeyCiphertext: []byte{1},
		KeyBlindIndex: "blind-" + orgID[:8], ValueCiphertext: []byte{2}, Tags: "[]", Version: 1,
	}
	if err := st.CreateItem(ctx, item, "created"); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListItems(ctx, env.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v %v", items, err)
	}
	event := &audit.Event{
		ID: uuid.NewString(), OrgID: org.ID, Seq: 1,
		Actor:  audit.Actor{ID: user.ID, Kind: "user", Name: user.Name},
		Action: "test.postgres", Resource: audit.Resource{Type: "test", ID: org.ID},
		CreatedAt: time.Now().UTC(),
	}
	if err := st.AppendAuditEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	events, err := st.ListAuditEvents(ctx, org.ID, 0, 10)
	if err != nil || len(events) == 0 {
		t.Fatalf("events = %+v %v", events, err)
	}
}

func TestMigrateSQLiteToPostgres(t *testing.T) {
	dsn := os.Getenv("CLAVIS_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set CLAVIS_TEST_POSTGRES to run Postgres integration tests")
	}
	ctx := context.Background()
	source, err := store.Open(ctx, filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	if err := source.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user := &store.User{ID: uuid.NewString(), Email: "migrate@example.com", Name: "Migrate", EmailVerified: true}
	if err := source.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "clavis_migrate_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		_ = admin.Close()
	}()
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	target, err := store.OpenPostgres(ctx, dsn+separator+"search_path="+url.QueryEscape(schema))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Close() }()
	if err := target.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	report, err := store.MigrateFromSQLite(ctx, source, target, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Rows == 0 || report.Tables["users"] != 1 {
		t.Fatalf("report = %+v", report)
	}
	loaded, err := target.GetUserByEmail(ctx, user.Email)
	if err != nil || loaded.ID != user.ID {
		t.Fatalf("migrated user = %+v %v", loaded, err)
	}
	if _, err := store.MigrateFromSQLite(ctx, source, target, false); err == nil {
		t.Fatal("expected non-empty destination refusal")
	}
}

func TestPostgresEventNotify(t *testing.T) {
	dsn := os.Getenv("CLAVIS_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set CLAVIS_TEST_POSTGRES to run Postgres integration tests")
	}
	ctx := context.Background()
	st, err := store.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	listenCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	received := make(chan string, 1)
	go func() {
		_ = st.ListenEvents(listenCtx, func(payload string) {
			select {
			case received <- payload:
			default:
			}
		})
	}()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	wanted := `{"hello":"postgres"}`
	for {
		select {
		case payload := <-received:
			if payload != wanted {
				t.Fatalf("payload = %q", payload)
			}
			return
		case <-ticker.C:
			_ = st.NotifyEvent(ctx, wanted)
		case <-deadline:
			t.Fatal("notification not received")
		}
	}
}
