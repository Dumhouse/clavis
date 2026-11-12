-- +goose Up
CREATE TABLE offboarding_tasks (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    env_id     TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    item_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'pending',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(user_id, item_id)
);
CREATE INDEX offboarding_tasks_user_idx ON offboarding_tasks(org_id, user_id, status);

-- +goose Down
DROP TABLE IF EXISTS offboarding_tasks;
