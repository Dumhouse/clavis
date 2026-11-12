-- +goose Up
CREATE TABLE webhook_deliveries (
    id              TEXT PRIMARY KEY,
    org_id          TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    webhook_id      TEXT NOT NULL,
    event_id        TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    url             TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0,
    response_status INTEGER,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    delivered_at    TEXT
);
CREATE INDEX webhook_deliveries_org_idx ON webhook_deliveries (org_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS webhook_deliveries;
