-- +goose Up
CREATE TABLE invites (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    email       TEXT NOT NULL DEFAULT '',
    role_id     TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    token_hash  BYTEA NOT NULL,
    created_by  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL,
    accepted_at TEXT,
    accepted_by TEXT
);
CREATE UNIQUE INDEX invites_token_idx ON invites (token_hash);
CREATE INDEX invites_org_idx ON invites (org_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS invites;
