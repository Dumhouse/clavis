-- +goose Up
CREATE TABLE promotion_requests (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_env   TEXT NOT NULL,
    target_env   TEXT NOT NULL,
    keys         TEXT NOT NULL DEFAULT '[]',
    status       TEXT NOT NULL DEFAULT 'pending',
    requested_by TEXT NOT NULL,
    requested_at TEXT NOT NULL,
    decided_by   TEXT,
    decided_at   TEXT
);

CREATE INDEX idx_promotion_requests_project ON promotion_requests(project_id, status, requested_at);

-- +goose Down
DROP TABLE IF EXISTS promotion_requests;
