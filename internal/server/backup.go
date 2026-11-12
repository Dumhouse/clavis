package server

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/dumhouse/clavis/internal/backup"
)

// backupLoop writes an encrypted backup on start and then on the configured
// interval. Restoring requires the root key, which is never stored in the
// backup itself.
func (s *Server) backupLoop(ctx context.Context) {
	if !s.cfg.Backup.Enabled {
		return
	}
	s.runScheduledBackup(ctx)
	ticker := time.NewTicker(s.cfg.Backup.Interval.Duration())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runScheduledBackup(ctx)
		}
	}
}

func (s *Server) runScheduledBackup(ctx context.Context) {
	dir := s.cfg.Backup.Dir
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.logger.Error("scheduled backup: create directory", "dir", dir, "error", err)
		return
	}

	temp, err := os.CreateTemp("", "clavis-scheduled-backup-*")
	if err != nil {
		s.logger.Error("scheduled backup: temp file", "error", err)
		return
	}
	tempPath := temp.Name()
	_ = temp.Close()
	defer func() { _ = os.Remove(tempPath) }()

	if s.cfg.Database.Driver == "postgres" {
		if err := backup.SnapshotPostgres(ctx, s.cfg.Database.DSN, tempPath); err != nil {
			s.logger.Error("scheduled backup: dump", "error", err)
			return
		}
	} else if err := backup.SnapshotSQLite(ctx, s.store.DB(), tempPath); err != nil {
		s.logger.Error("scheduled backup: snapshot", "error", err)
		return
	}

	plaintext, err := os.ReadFile(tempPath)
	if err != nil {
		s.logger.Error("scheduled backup: read snapshot", "error", err)
		return
	}
	encrypted, err := backup.Encrypt(s.rootKey[:], plaintext)
	if err != nil {
		s.logger.Error("scheduled backup: encrypt", "error", err)
		return
	}

	dest := filepath.Join(dir, backup.FileName(time.Now().UTC()))
	if err := os.WriteFile(dest, encrypted, 0o600); err != nil { //nolint:gosec // G703: backup directory is operator configuration
		s.logger.Error("scheduled backup: write", "path", dest, "error", err)
		return
	}
	removed, err := backup.Prune(dir, s.cfg.Backup.Keep)
	if err != nil {
		s.logger.Warn("scheduled backup: prune", "error", err)
	}
	s.logger.Info("scheduled backup written",
		"path", dest, "bytes", len(encrypted), "pruned", len(removed),
		"keep", s.cfg.Backup.Keep)
}
