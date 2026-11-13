-- +goose Up
-- An org-level provider credential that many syncs can share. The secret
-- fields in config are org-key ciphertext bound to the connection ID.
CREATE TABLE sync_connections (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,
    name         TEXT NOT NULL,
    base_url     TEXT NOT NULL DEFAULT '',
    config       TEXT NOT NULL DEFAULT '{}',
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    last_used_at TEXT,
    UNIQUE(org_id, name)
);
CREATE INDEX sync_connections_org_idx ON sync_connections (org_id, provider);

ALTER TABLE syncs ADD COLUMN connection_id TEXT NOT NULL DEFAULT '';
CREATE INDEX syncs_connection_idx ON syncs (connection_id);

-- +goose Down
DROP INDEX IF EXISTS syncs_connection_idx;
ALTER TABLE syncs DROP COLUMN connection_id;
DROP TABLE IF EXISTS sync_connections;
