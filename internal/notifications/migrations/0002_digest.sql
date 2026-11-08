-- +goose Up
-- Flood guard: ad messages are counted per (user, search). Past the burst
-- limit they are queued here and sent later as one digest per search.
ALTER TABLE deliveries
    ADD COLUMN user_id     BIGINT,
    ADD COLUMN search_name TEXT,
    ADD COLUMN digested    BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX idx_deliveries_search ON deliveries (user_id, search_name, delivered_at) WHERE NOT digested;

CREATE TABLE digest_queue (
    key            TEXT PRIMARY KEY,
    user_id        BIGINT      NOT NULL,
    search_name    TEXT        NOT NULL,
    channel_kind   TEXT        NOT NULL,
    channel_target TEXT        NOT NULL,
    listing        JSONB       NOT NULL,
    queued_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE digest_queue;
DROP INDEX idx_deliveries_search;
ALTER TABLE deliveries DROP COLUMN digested, DROP COLUMN search_name, DROP COLUMN user_id;
