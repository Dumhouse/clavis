-- +goose Up
ALTER TABLE sessions ADD COLUMN csrf_hash BYTEA;

-- +goose Down
ALTER TABLE sessions DROP COLUMN csrf_hash;
