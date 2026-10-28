-- +goose Up
-- One row per notification already handed to a channel; the key is the
-- idempotency key from events.NotificationRequested.
CREATE TABLE deliveries (
    key          TEXT PRIMARY KEY,
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE deliveries;
