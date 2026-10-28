-- +goose Up
CREATE TABLE users (
    id   BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

-- One delivery destination per platform per user.
CREATE TABLE channels (
    id      BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind    TEXT   NOT NULL,
    target  TEXT   NOT NULL,
    UNIQUE (user_id, kind)
);

-- feeds says what to scrape (the source's own notion of a feed); filters are
-- applied locally to what the feeds return, see subscriptions.Filters.
CREATE TABLE searches (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT  NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    channel_id BIGINT  NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    source     TEXT    NOT NULL DEFAULT 'ss.com',
    feeds      TEXT[]  NOT NULL,
    filters    JSONB   NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

-- Which listing was already matched to which search. No FK to listings: that
-- table belongs to another slice and gets purged on its own schedule.
CREATE TABLE matches (
    search_id   BIGINT      NOT NULL REFERENCES searches (id) ON DELETE CASCADE,
    source      TEXT        NOT NULL,
    external_id TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (search_id, source, external_id)
);

CREATE INDEX idx_matches_created ON matches (created_at);

-- +goose Down
DROP TABLE matches;
DROP TABLE searches;
DROP TABLE channels;
DROP TABLE users;
