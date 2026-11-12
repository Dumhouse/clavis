-- +goose Up
CREATE TABLE shared_sets (
    id          TEXT PRIMARY KEY,
    org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    wrapped_key BYTEA NOT NULL,
    key_version INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX shared_sets_org_slug_idx ON shared_sets (org_id, lower(slug));

CREATE TABLE shared_set_items (
    id                 TEXT PRIMARY KEY,
    set_id             TEXT NOT NULL REFERENCES shared_sets(id) ON DELETE CASCADE,
    key_ciphertext     BYTEA NOT NULL,
    key_blind_index    TEXT NOT NULL,
    value_ciphertext   BYTEA NOT NULL,
    comment_ciphertext BYTEA,
    kind               TEXT NOT NULL DEFAULT 'secret',
    version            INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);
CREATE UNIQUE INDEX shared_set_items_key_idx ON shared_set_items (set_id, key_blind_index);

CREATE TABLE shared_set_imports (
    id             TEXT PRIMARY KEY,
    set_id         TEXT NOT NULL REFERENCES shared_sets(id) ON DELETE CASCADE,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    priority       INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX shared_set_imports_unique_idx ON shared_set_imports (set_id, environment_id);
CREATE INDEX shared_set_imports_env_idx ON shared_set_imports (environment_id, priority);

-- +goose Down
DROP TABLE IF EXISTS shared_set_imports;
DROP TABLE IF EXISTS shared_set_items;
DROP TABLE IF EXISTS shared_sets;
