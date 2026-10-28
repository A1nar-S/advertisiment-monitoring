-- +goose Up
CREATE TABLE listings (
    id            BIGSERIAL PRIMARY KEY,
    source        TEXT           NOT NULL,
    external_id   TEXT           NOT NULL,
    title         TEXT           NOT NULL,
    url           TEXT           NOT NULL,
    category      TEXT           NOT NULL DEFAULT '',
    district      TEXT           NOT NULL DEFAULT '',
    address       TEXT           NOT NULL DEFAULT '',
    rooms         INTEGER,
    area_m2       INTEGER,
    series        TEXT           NOT NULL DEFAULT '',
    price         TEXT           NOT NULL DEFAULT '',
    price_value   NUMERIC(10, 2),
    has_photo     BOOLEAN        NOT NULL DEFAULT FALSE,
    image_url     TEXT           NOT NULL DEFAULT '',
    posted_at     TIMESTAMPTZ,
    first_seen_at TIMESTAMPTZ    NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ    NOT NULL DEFAULT now(),
    is_active     BOOLEAN        NOT NULL DEFAULT TRUE,
    UNIQUE (source, external_id)
);

CREATE INDEX idx_listings_active ON listings (is_active);
CREATE INDEX idx_listings_first_seen ON listings (first_seen_at DESC);

-- +goose Down
DROP TABLE listings;
