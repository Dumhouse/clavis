-- +goose Up
CREATE TABLE recovery_codes (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  BLOB NOT NULL,
    used_at    TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX recovery_codes_user_idx ON recovery_codes (user_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS recovery_codes;
