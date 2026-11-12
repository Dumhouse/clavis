-- +goose Up
ALTER TABLE syncs ADD COLUMN confirmed INTEGER NOT NULL DEFAULT 0;

CREATE TABLE sync_runs (
    id           TEXT PRIMARY KEY,
    sync_id      TEXT NOT NULL REFERENCES syncs(id) ON DELETE CASCADE,
    project_id   TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    dry_run      INTEGER NOT NULL DEFAULT 0,
    secret_count INTEGER NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT '',
    detail       TEXT NOT NULL DEFAULT '',
    started_at   TEXT NOT NULL,
    finished_at  TEXT
);
CREATE INDEX sync_runs_sync_idx ON sync_runs (sync_id, started_at);

-- +goose Down
DROP TABLE IF EXISTS sync_runs;
ALTER TABLE syncs DROP COLUMN confirmed;
