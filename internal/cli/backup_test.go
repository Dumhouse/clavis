package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/backup"
	"github.com/dumhouse/clavis/internal/store"
)

func TestBackupEncryptionRoundTripAndTamper(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("database bytes with wrapped keys")
	encrypted, err := backup.Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := backup.Decrypt(key, encrypted)
	if err != nil || !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypt = %q %v", decrypted, err)
	}
	encrypted[len(encrypted)-1] ^= 0xff
	if _, err := backup.Decrypt(key, encrypted); err == nil {
		t.Fatal("tampered backup decrypted")
	}
}

func TestVerifySQLiteBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := func() []byte {
		t.Helper()
		dest := filepath.Join(dir, "snap.db")
		if err := backup.SnapshotSQLite(ctx, st.DB(), dest); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(dest)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	// A backup of a server that was never set up is flagged as empty.
	if _, err := verifySQLiteBackup(ctx, snapshot()); err == nil || !strings.Contains(err.Error(), "no organizations") {
		t.Fatalf("empty backup: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := st.DB().ExecContext(ctx,
		"INSERT INTO orgs (id, slug, name, wrapped_org_key, created_at, updated_at) VALUES ('o1','acme','Acme',x'00',?,?)", now, now); err != nil {
		t.Fatal(err)
	}
	summary, err := verifySQLiteBackup(ctx, snapshot())
	if err != nil || summary.Orgs != 1 || summary.IntegrityNote != "ok" {
		t.Fatalf("good backup: %+v %v", summary, err)
	}

	// Bytes that are not a database are rejected.
	if _, err := verifySQLiteBackup(ctx, []byte("this is not a database file at all, just text")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestVerifyPostgresDump(t *testing.T) {
	good := []byte("--\n-- PostgreSQL database dump\n--\nCREATE TABLE a ();\n-- PostgreSQL database dump complete\n")
	if _, err := backup.VerifyPostgresDump(good); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.VerifyPostgresDump(good[:len(good)-30]); err == nil {
		t.Fatal("truncated dump accepted")
	}
}
