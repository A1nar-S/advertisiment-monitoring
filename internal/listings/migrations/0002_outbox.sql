-- +goose Up
-- Listings that are new and worth telling the matcher about. Written in the
-- same transaction as the listing itself, published to Kafka by a relay.
CREATE TABLE listings_outbox (
    id           BIGSERIAL PRIMARY KEY,
    payload      JSONB       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

CREATE INDEX idx_listings_outbox_unpublished ON listings_outbox (id) WHERE published_at IS NULL;

-- When a feed was first scraped. Listings arriving shortly after that are the
-- feed's existing ads, stored silently so a new feed does not flood users.
CREATE TABLE feed_state (
    source        TEXT        NOT NULL,
    feed          TEXT        NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source, feed)
);

-- +goose Down
DROP TABLE feed_state;
DROP TABLE listings_outbox;
