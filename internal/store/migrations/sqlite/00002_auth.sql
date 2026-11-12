-- +goose Up
ALTER TABLE sessions ADD COLUMN csrf_hash BLOB;

-- +goose Down
ALTER TABLE sessions DROP COLUMN csrf_hash;
