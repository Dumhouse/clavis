-- +goose Up
ALTER TABLE leases ADD COLUMN identity_id TEXT NOT NULL DEFAULT '';
CREATE INDEX leases_identity_idx ON leases(identity_id);

-- +goose Down
DROP INDEX IF EXISTS leases_identity_idx;
ALTER TABLE leases DROP COLUMN identity_id;
