-- +goose Up
ALTER TABLE secret_schema ADD COLUMN enum_values TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE secret_schema DROP COLUMN enum_values;
