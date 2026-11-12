-- +goose Up
ALTER TABLE access_requests ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
ALTER TABLE access_requests ADD COLUMN environment TEXT NOT NULL DEFAULT '';
ALTER TABLE access_requests ADD COLUMN role TEXT NOT NULL DEFAULT '';
ALTER TABLE access_requests ADD COLUMN duration_seconds INTEGER NOT NULL DEFAULT 86400;
ALTER TABLE access_requests ADD COLUMN required_approvals INTEGER NOT NULL DEFAULT 1;
ALTER TABLE access_requests ADD COLUMN approvals TEXT NOT NULL DEFAULT '[]';
ALTER TABLE access_requests ADD COLUMN decided_at TEXT;
ALTER TABLE access_requests ADD COLUMN applied_binding_id TEXT;
CREATE INDEX access_requests_project_idx ON access_requests(project_id, status, created_at);

-- +goose Down
DROP INDEX IF EXISTS access_requests_project_idx;
ALTER TABLE access_requests DROP COLUMN applied_binding_id;
ALTER TABLE access_requests DROP COLUMN decided_at;
ALTER TABLE access_requests DROP COLUMN approvals;
ALTER TABLE access_requests DROP COLUMN required_approvals;
ALTER TABLE access_requests DROP COLUMN duration_seconds;
ALTER TABLE access_requests DROP COLUMN role;
ALTER TABLE access_requests DROP COLUMN environment;
ALTER TABLE access_requests DROP COLUMN project_id;
