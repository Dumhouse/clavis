-- +goose Up
CREATE TABLE rotation_policies (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment_id   TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    secret_key       TEXT NOT NULL,
    provider         TEXT NOT NULL,
    config           BYTEA NOT NULL,
    interval_seconds INTEGER NOT NULL,
    enabled          INTEGER NOT NULL DEFAULT 1,
    last_run_at      TEXT,
    next_run_at      TEXT NOT NULL,
    last_status      TEXT NOT NULL DEFAULT '',
    last_error       TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL
);
CREATE INDEX rotation_policies_due_idx ON rotation_policies(enabled, next_run_at);

CREATE TABLE rotation_runs (
    id          TEXT PRIMARY KEY,
    policy_id   TEXT NOT NULL REFERENCES rotation_policies(id) ON DELETE CASCADE,
    status      TEXT NOT NULL,
    phase       TEXT NOT NULL,
    message     TEXT NOT NULL DEFAULT '',
    started_at  TEXT NOT NULL,
    finished_at TEXT
);
CREATE INDEX rotation_runs_policy_idx ON rotation_runs(policy_id, started_at);

-- +goose Down
DROP TABLE IF EXISTS rotation_runs;
DROP TABLE IF EXISTS rotation_policies;
