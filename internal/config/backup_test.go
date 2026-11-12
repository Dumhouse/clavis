package config_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dumhouse/clavis/internal/config"
)

func TestBackupDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAVIS_DATA_DIR", dir)
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backup.Enabled {
		t.Fatal("backups must be opt-in")
	}
	if cfg.Backup.Dir != filepath.Join(dir, "backups") {
		t.Fatalf("backup dir = %q", cfg.Backup.Dir)
	}
	if cfg.Backup.Interval.Duration() != 24*time.Hour || cfg.Backup.Keep != 7 {
		t.Fatalf("backup defaults = %v, %d", cfg.Backup.Interval, cfg.Backup.Keep)
	}
}

func TestBackupEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAVIS_DATA_DIR", dir)
	t.Setenv("CLAVIS_BACKUP_ENABLED", "true")
	t.Setenv("CLAVIS_BACKUP_DIR", filepath.Join(dir, "snapshots"))
	t.Setenv("CLAVIS_BACKUP_INTERVAL", "6h")
	t.Setenv("CLAVIS_BACKUP_KEEP", "3")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Backup.Enabled {
		t.Fatal("backups should be enabled")
	}
	if cfg.Backup.Dir != filepath.Join(dir, "snapshots") {
		t.Fatalf("backup dir = %q", cfg.Backup.Dir)
	}
	if cfg.Backup.Interval.Duration() != 6*time.Hour || cfg.Backup.Keep != 3 {
		t.Fatalf("backup config = %v, %d", cfg.Backup.Interval, cfg.Backup.Keep)
	}
}

func TestBackupValidation(t *testing.T) {
	t.Setenv("CLAVIS_BACKUP_ENABLED", "true")
	t.Setenv("CLAVIS_BACKUP_KEEP", "0")
	if _, err := config.Load(""); err == nil {
		t.Fatal("expected invalid keep to fail")
	}
}
