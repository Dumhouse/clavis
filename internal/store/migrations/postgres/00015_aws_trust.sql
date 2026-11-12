-- +goose Up
ALTER TABLE identities ADD COLUMN aws_trust TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE identities DROP COLUMN aws_trust;
