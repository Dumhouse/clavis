-- +goose Up
-- The initial schema reserved passkeys for WebAuthn. Store the full
-- credential record as JSON so the library can round-trip all fields.
ALTER TABLE passkeys ADD COLUMN data BLOB NOT NULL DEFAULT X'7B7D';

-- +goose Down
ALTER TABLE passkeys DROP COLUMN data;
