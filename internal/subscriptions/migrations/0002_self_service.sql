-- +goose Up
-- Users manage their own searches through chat bots (Discord now, Telegram
-- later) and the dashboard. A chat account (platform + its user ID) is the
-- identity.
ALTER TABLE users
    ADD COLUMN platform     TEXT,
    ADD COLUMN external_id  TEXT,
    ADD COLUMN display_name TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT users_platform_external_key UNIQUE (platform, external_id);

-- A user can have a direct chat and any number of group channels.
ALTER TABLE channels DROP CONSTRAINT channels_user_id_kind_key;
ALTER TABLE channels ADD CONSTRAINT channels_user_kind_target_key UNIQUE (user_id, kind, target);
-- How to show it: "Private messages", "#cars".
ALTER TABLE channels ADD COLUMN label TEXT NOT NULL DEFAULT '';

-- spec is the search as the user wrote it ({category, filters} in the
-- source's names), so it can be shown and edited; feeds and filters are
-- compiled from it on every save.
ALTER TABLE searches
    ADD COLUMN spec       JSONB       NOT NULL DEFAULT '{}',
    ADD COLUMN origin     TEXT        NOT NULL DEFAULT '',
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Dashboard login: a one-time link token (sent by a bot) becomes a session.
-- Only SHA-256 hashes are stored.
CREATE TABLE login_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE sessions;
DROP TABLE login_tokens;
ALTER TABLE searches DROP COLUMN updated_at, DROP COLUMN origin, DROP COLUMN spec;
ALTER TABLE channels DROP COLUMN label;
ALTER TABLE channels DROP CONSTRAINT channels_user_kind_target_key;
ALTER TABLE channels ADD CONSTRAINT channels_user_id_kind_key UNIQUE (user_id, kind);
ALTER TABLE users DROP CONSTRAINT users_platform_external_key;
ALTER TABLE users DROP COLUMN display_name, DROP COLUMN external_id, DROP COLUMN platform;
