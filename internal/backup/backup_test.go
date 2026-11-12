package backup_test

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/backup"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestEncryptRoundTripAndTamper(t *testing.T) {
	key := testKey(t)
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
	if _, err := backup.Decrypt(testKey(t), encrypted); err == nil {
		t.Fatal("backup decrypted with a different key")
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		name := backup.FileName(base.Add(time.Duration(i) * time.Hour))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Unrelated files must survive.
	keepMe := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(keepMe, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := backup.Prune(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 3 {
		t.Fatalf("removed = %v, want 3", removed)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 2 backups + 1 note", len(entries))
	}
	if _, err := os.Stat(keepMe); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
}
