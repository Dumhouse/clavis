-- +goose Up
CREATE TABLE audit_checkpoints (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    event_hash TEXT NOT NULL,
    signature  BYTEA NOT NULL,
    public_key BYTEA NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX audit_checkpoints_org_idx ON audit_checkpoints (org_id, seq);

-- +goose Down
DROP TABLE IF EXISTS audit_checkpoints;
