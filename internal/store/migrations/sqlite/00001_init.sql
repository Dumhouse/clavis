-- +goose Up

-- Users are instance-wide; they join organizations through memberships.
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    avatar_url    TEXT NOT NULL DEFAULT '',
    password_hash TEXT,
    mfa_enabled   INTEGER NOT NULL DEFAULT 0,
    mfa_secret_ciphertext BLOB,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE UNIQUE INDEX users_email_idx ON users (lower(email));

CREATE TABLE passkeys (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BLOB NOT NULL,
    public_key    BLOB NOT NULL,
    sign_count    INTEGER NOT NULL DEFAULT 0,
    name          TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    last_used_at  TEXT
);
CREATE UNIQUE INDEX passkeys_credential_idx ON passkeys (credential_id);

CREATE TABLE sessions (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash    BLOB NOT NULL,
    ip            TEXT NOT NULL DEFAULT '',
    user_agent    TEXT NOT NULL DEFAULT '',
    mfa_verified  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    last_seen_at  TEXT NOT NULL,
    expires_at    TEXT NOT NULL
);
CREATE UNIQUE INDEX sessions_token_idx ON sessions (token_hash);
CREATE INDEX sessions_user_idx ON sessions (user_id);
CREATE INDEX sessions_expiry_idx ON sessions (expires_at);

CREATE TABLE orgs (
    id              TEXT PRIMARY KEY,
    slug            TEXT NOT NULL,
    name            TEXT NOT NULL,
    settings        TEXT NOT NULL DEFAULT '{}',
    wrapped_org_key BLOB NOT NULL,
    key_version     INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE UNIQUE INDEX orgs_slug_idx ON orgs (lower(slug));

CREATE TABLE roles (
    id          TEXT PRIMARY KEY,
    org_id      TEXT REFERENCES orgs(id) ON DELETE CASCADE, -- NULL = built-in
    name        TEXT NOT NULL,
    permissions TEXT NOT NULL DEFAULT '[]',
    builtin     INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);
CREATE UNIQUE INDEX roles_org_name_idx ON roles (org_id, lower(name));

CREATE TABLE memberships (
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    TEXT REFERENCES roles(id) ON DELETE SET NULL,
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL,
    removed_at TEXT,
    PRIMARY KEY (org_id, user_id)
);

CREATE TABLE groups (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    external_id TEXT,
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX groups_org_name_idx ON groups (org_id, lower(name));

CREATE TABLE group_members (
    group_id TEXT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE role_bindings (
    id               TEXT PRIMARY KEY,
    org_id           TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    subject_kind     TEXT NOT NULL CHECK (subject_kind IN ('user','group','machine','agent')),
    subject_id       TEXT NOT NULL,
    role_id          TEXT REFERENCES roles(id) ON DELETE CASCADE,
    project_id       TEXT,
    environment_glob TEXT NOT NULL DEFAULT '',
    path_glob        TEXT NOT NULL DEFAULT '',
    expires_at       TEXT,
    created_at       TEXT NOT NULL
);
CREATE INDEX role_bindings_subject_idx ON role_bindings (org_id, subject_kind, subject_id);

CREATE TABLE projects (
    id                  TEXT PRIMARY KEY,
    org_id              TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    slug                TEXT NOT NULL,
    name                TEXT NOT NULL,
    wrapped_project_key BLOB NOT NULL,
    key_version         INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);
CREATE UNIQUE INDEX projects_org_slug_idx ON projects (org_id, lower(slug));

CREATE TABLE env_templates (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    environments TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL
);
CREATE UNIQUE INDEX env_templates_org_name_idx ON env_templates (org_id, lower(name));

CREATE TABLE environments (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    slug           TEXT NOT NULL,
    name           TEXT NOT NULL,
    type           TEXT NOT NULL CHECK (type IN ('development','preview','staging','production')),
    color          TEXT NOT NULL DEFAULT '',
    parent_id      TEXT REFERENCES environments(id) ON DELETE SET NULL,
    position       INTEGER NOT NULL DEFAULT 0,
    is_protected   INTEGER NOT NULL DEFAULT 0,
    branch_pattern TEXT NOT NULL DEFAULT '',
    preview_source TEXT NOT NULL DEFAULT '',
    expires_at     TEXT,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX environments_project_slug_idx ON environments (project_id, slug);
CREATE INDEX environments_parent_idx ON environments (parent_id);

CREATE TABLE overlays (
    id             TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    slug           TEXT NOT NULL,
    name           TEXT NOT NULL,
    created_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX overlays_env_slug_idx ON overlays (environment_id, slug);

CREATE TABLE freeze_windows (
    id             TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    starts_at      TEXT NOT NULL,
    ends_at        TEXT NOT NULL,
    reason         TEXT NOT NULL DEFAULT '',
    created_by     TEXT NOT NULL,
    created_at     TEXT NOT NULL
);
CREATE INDEX freeze_windows_env_idx ON freeze_windows (environment_id, starts_at);

CREATE TABLE secret_sets (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    slug            TEXT NOT NULL,
    name            TEXT NOT NULL,
    wrapped_set_key BLOB NOT NULL,
    key_version     INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE UNIQUE INDEX secret_sets_org_slug_idx ON secret_sets (org_id, lower(slug));

CREATE TABLE secret_set_imports (
    set_id         TEXT NOT NULL REFERENCES secret_sets(id) ON DELETE CASCADE,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    position       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (set_id, environment_id)
);

CREATE TABLE folders (
    id             TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    path           TEXT NOT NULL,
    created_at     TEXT NOT NULL
);
CREATE UNIQUE INDEX folders_env_path_idx ON folders (environment_id, path);

CREATE TABLE items (
    id                  TEXT PRIMARY KEY,
    environment_id      TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    overlay_id          TEXT REFERENCES overlays(id) ON DELETE CASCADE,
    folder_id           TEXT REFERENCES folders(id) ON DELETE SET NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('secret','variable','file')),
    key_ciphertext      BLOB NOT NULL,
    key_blind_index     TEXT NOT NULL,
    value_ciphertext    BLOB,
    comment_ciphertext  BLOB,
    tags                TEXT NOT NULL DEFAULT '[]',
    owner_group_id      TEXT REFERENCES groups(id) ON DELETE SET NULL,
    notes_ciphertext    BLOB,
    provider_url        TEXT NOT NULL DEFAULT '',
    expires_at          TEXT,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    deleted_at          TEXT
);
CREATE UNIQUE INDEX items_env_key_idx ON items (environment_id, key_blind_index)
    WHERE overlay_id IS NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX items_env_overlay_key_idx ON items (environment_id, overlay_id, key_blind_index)
    WHERE overlay_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX items_folder_idx ON items (folder_id);
CREATE INDEX items_deleted_idx ON items (deleted_at);

CREATE TABLE item_versions (
    item_id            TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    version            INTEGER NOT NULL,
    value_ciphertext   BLOB,
    comment_ciphertext BLOB,
    change_message     TEXT NOT NULL DEFAULT '',
    actor_id           TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    PRIMARY KEY (item_id, version)
);

CREATE TABLE item_overrides (
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_blind_index TEXT NOT NULL,
    value_ciphertext BLOB NOT NULL,
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (environment_id, user_id, key_blind_index)
);

CREATE TABLE secret_schema (
    project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key         TEXT NOT NULL,
    type        TEXT NOT NULL DEFAULT 'string',
    required    TEXT NOT NULL DEFAULT '{}',
    pattern     TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    example     TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL DEFAULT 'secret',
    PRIMARY KEY (project_id, key)
);

CREATE TABLE snapshots (
    id             TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    reason         TEXT NOT NULL DEFAULT '',
    payload        BLOB NOT NULL,
    created_at     TEXT NOT NULL
);
CREATE INDEX snapshots_env_idx ON snapshots (environment_id, created_at);

CREATE TABLE promotions (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_env        TEXT NOT NULL,
    target_env        TEXT NOT NULL,
    selected_keys     TEXT NOT NULL DEFAULT '[]',
    status            TEXT NOT NULL DEFAULT 'pending',
    change_request_id TEXT,
    created_at        TEXT NOT NULL
);

CREATE TABLE audit_events (
    id            TEXT PRIMARY KEY,
    org_id        TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    seq           INTEGER NOT NULL,
    prev_hash     TEXT NOT NULL,
    hash          TEXT NOT NULL,
    actor_id      TEXT NOT NULL,
    actor_kind    TEXT NOT NULL,
    actor_name    TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL DEFAULT '',
    resource_name TEXT NOT NULL DEFAULT '',
    project_id    TEXT NOT NULL DEFAULT '',
    env_id        TEXT NOT NULL DEFAULT '',
    details       TEXT NOT NULL DEFAULT '{}',
    ip            TEXT NOT NULL DEFAULT '',
    user_agent    TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL
);
CREATE UNIQUE INDEX audit_events_org_seq_idx ON audit_events (org_id, seq);
CREATE INDEX audit_events_org_created_idx ON audit_events (org_id, created_at);
CREATE INDEX audit_events_action_idx ON audit_events (org_id, action);

CREATE TABLE reveal_events (
    id        TEXT PRIMARY KEY,
    user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id   TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    version   INTEGER NOT NULL,
    channel   TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX reveal_events_user_idx ON reveal_events (user_id, created_at);
CREATE INDEX reveal_events_item_idx ON reveal_events (item_id);

CREATE TABLE identities (
    id               TEXT PRIMARY KEY,
    org_id           TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    type             TEXT NOT NULL CHECK (type IN ('machine','agent')),
    auth_methods     TEXT NOT NULL DEFAULT '[]',
    token_ttl_seconds INTEGER NOT NULL DEFAULT 3600,
    allowed_commands TEXT NOT NULL DEFAULT '[]',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);
CREATE UNIQUE INDEX identities_org_name_idx ON identities (org_id, lower(name));

CREATE TABLE access_tokens (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    identity_id  TEXT REFERENCES identities(id) ON DELETE CASCADE,
    user_id      TEXT REFERENCES users(id) ON DELETE CASCADE,
    token_hash   BLOB NOT NULL,
    prefix       TEXT NOT NULL,
    scopes       TEXT NOT NULL DEFAULT '[]',
    expires_at   TEXT,
    last_used_at TEXT,
    last_used_ip TEXT,
    created_at   TEXT NOT NULL,
    CHECK ((identity_id IS NOT NULL) <> (user_id IS NOT NULL))
);
CREATE UNIQUE INDEX access_tokens_hash_idx ON access_tokens (token_hash);
CREATE INDEX access_tokens_identity_idx ON access_tokens (identity_id);

CREATE TABLE syncs (
    id            TEXT PRIMARY KEY,
    project_id    TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    path          TEXT NOT NULL DEFAULT '/',
    provider      TEXT NOT NULL,
    config        BLOB NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending',
    last_synced_at TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX syncs_project_idx ON syncs (project_id);

CREATE TABLE dynamic_secret_configs (
    id             TEXT PRIMARY KEY,
    project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    engine         TEXT NOT NULL,
    config         BLOB NOT NULL,
    default_ttl_seconds INTEGER NOT NULL,
    max_ttl_seconds     INTEGER NOT NULL,
    created_at     TEXT NOT NULL
);

CREATE TABLE leases (
    id         TEXT PRIMARY KEY,
    config_id  TEXT NOT NULL REFERENCES dynamic_secret_configs(id) ON DELETE CASCADE,
    lease_data BLOB NOT NULL,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX leases_expiry_idx ON leases (expires_at);

CREATE TABLE access_requests (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    requester_id TEXT NOT NULL,
    target     TEXT NOT NULL,
    reason     TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending',
    expires_at TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE change_requests (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    requester_id TEXT NOT NULL,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    diff       BLOB NOT NULL,
    status     TEXT NOT NULL DEFAULT 'pending',
    expires_at TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE share_links (
    id            TEXT PRIMARY KEY,
    ciphertext    BLOB NOT NULL,
    view_limit    INTEGER NOT NULL DEFAULT 1,
    views         INTEGER NOT NULL DEFAULT 0,
    password_hash TEXT,
    expires_at    TEXT,
    created_at    TEXT NOT NULL
);

CREATE TABLE jobs (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    payload      BLOB NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','done','failed')),
    run_at       TEXT NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 10,
    locked_by    TEXT NOT NULL DEFAULT '',
    locked_until TEXT,
    last_error   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
CREATE INDEX jobs_claim_idx ON jobs (status, run_at);
CREATE INDEX jobs_locked_idx ON jobs (locked_until);

-- +goose Down
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS share_links;
DROP TABLE IF EXISTS change_requests;
DROP TABLE IF EXISTS access_requests;
DROP TABLE IF EXISTS leases;
DROP TABLE IF EXISTS dynamic_secret_configs;
DROP TABLE IF EXISTS syncs;
DROP TABLE IF EXISTS access_tokens;
DROP TABLE IF EXISTS identities;
DROP TABLE IF EXISTS reveal_events;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS promotions;
DROP TABLE IF EXISTS snapshots;
DROP TABLE IF EXISTS secret_schema;
DROP TABLE IF EXISTS item_overrides;
DROP TABLE IF EXISTS item_versions;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS folders;
DROP TABLE IF EXISTS secret_set_imports;
DROP TABLE IF EXISTS secret_sets;
DROP TABLE IF EXISTS freeze_windows;
DROP TABLE IF EXISTS overlays;
DROP TABLE IF EXISTS environments;
DROP TABLE IF EXISTS env_templates;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS role_bindings;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS orgs;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS passkeys;
DROP TABLE IF EXISTS users;
