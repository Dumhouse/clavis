-- +goose Up
ALTER TABLE overlays ADD COLUMN kind TEXT NOT NULL DEFAULT 'personal';
CREATE INDEX overlays_env_kind_idx ON overlays(environment_id, kind);

-- +goose Down
DROP INDEX IF EXISTS overlays_env_kind_idx;
ALTER TABLE overlays DROP COLUMN kind;
