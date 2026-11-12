-- +goose Up
ALTER TABLE users ADD COLUMN preferences TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE users DROP COLUMN preferences;
