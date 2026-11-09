-- +goose Up
-- Service-wide settings changed from chat (/setup), e.g. where status
-- messages go. A row overrides the matching .env setting.
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      JSONB       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE settings;
