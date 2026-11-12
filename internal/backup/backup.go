// Package backup creates and restores encrypted, consistent database backups.
package backup

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Magic prefixes every encrypted backup file.
const Magic = "CLVBK1\n"

const (
	filePrefix = "clavis-"
	fileSuffix = ".clvbk"
)

// FileName returns the backup name for a timestamp. The layout sorts
// lexicographically, which Prune relies on.
func FileName(at time.Time) string {
	return filePrefix + at.UTC().Format("20060102T150405Z") + fileSuffix
}

func backupCipher(key []byte) (cipher.AEAD, error) {
	sum := sha256.Sum256(append(append([]byte(nil), key...), []byte("clavis-backup-v1")...))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt seals plaintext with a root key. The same root key is required to
// restore, so it must be stored separately from the backup.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	gcm, err := backupCipher(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(append([]byte(Magic), nonce...), nonce, plaintext, []byte(Magic)), nil
}

// Decrypt opens a backup file produced by Encrypt.
func Decrypt(key, data []byte) ([]byte, error) {
	if !strings.HasPrefix(string(data), Magic) {
		return nil, fmt.Errorf("not a Clavis backup")
	}
	gcm, err := backupCipher(key)
	if err != nil {
		return nil, err
	}
	payload := data[len(Magic):]
	if len(payload) < gcm.NonceSize() {
		return nil, fmt.Errorf("truncated backup")
	}
	return gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], []byte(Magic))
}

// SnapshotSQLite writes a consistent database copy with VACUUM INTO. An empty
// file at dest is replaced.
func SnapshotSQLite(ctx context.Context, db *sql.DB, dest string) error {
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sqlite backup: remove %s: %w", dest, err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		return fmt.Errorf("sqlite backup: %w", err)
	}
	return nil
}

// SnapshotPostgres dumps a Postgres database with pg_dump.
func SnapshotPostgres(ctx context.Context, dsn, dest string) error {
	command := exec.CommandContext(ctx, "pg_dump", "--dbname", dsn, "--file", dest, "--no-owner", "--no-privileges")
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("pg_dump: %w (is pg_dump installed?)", err)
	}
	return nil
}

// RestorePostgres loads a dump with psql.
func RestorePostgres(ctx context.Context, dsn, path string) error {
	command := exec.CommandContext(ctx, "psql", "--dbname", dsn, "--file", path)
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("psql: %w (is psql installed?)", err)
	}
	return nil
}

// Prune keeps the newest keep backups in dir and returns the names removed.
func Prune(dir string, keep int) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)
	if len(files) <= keep {
		return nil, nil
	}
	removed := make([]string, 0, len(files)-keep)
	for _, name := range files[:len(files)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, err
		}
		removed = append(removed, name)
	}
	return removed, nil
}
