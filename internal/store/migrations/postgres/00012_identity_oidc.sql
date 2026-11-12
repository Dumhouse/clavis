-- +goose Up
ALTER TABLE identities ADD COLUMN oidc_trust TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE identities DROP COLUMN oidc_trust;
