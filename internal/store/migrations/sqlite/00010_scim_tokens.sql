-- +goose Up
CREATE TABLE scim_tokens (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    token_hash   BLOB NOT NULL,
    prefix       TEXT NOT NULL,
    created_by   TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    last_used_at TEXT
);
CREATE UNIQUE INDEX scim_tokens_hash_idx ON scim_tokens (token_hash);
CREATE INDEX scim_tokens_org_idx ON scim_tokens (org_id);

-- +goose Down
DROP TABLE IF EXISTS scim_tokens;
