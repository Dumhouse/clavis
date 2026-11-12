-- +goose Up
ALTER TABLE syncs ADD COLUMN drift_keys TEXT NOT NULL DEFAULT '[]';
ALTER TABLE syncs ADD COLUMN drift_checked_at TEXT;

-- +goose Down
ALTER TABLE syncs DROP COLUMN drift_checked_at;
ALTER TABLE syncs DROP COLUMN drift_keys;
