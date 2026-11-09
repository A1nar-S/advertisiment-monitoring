-- +goose Up
-- feed: the feed an ad was first found on. A search's scope depends on it
-- (a flat's deal, rent or sale, is only in the feed), so previews can check
-- stored ads the way the matcher checks new ones. Older rows stay ''.
--
-- details: what the ad's own page adds (a car's region, gearbox, ...), read
-- on demand. Kept apart from attrs, which every scrape rewrites from the
-- list page; reads merge the two, details winning.
ALTER TABLE listings
    ADD COLUMN feed        TEXT  NOT NULL DEFAULT '',
    ADD COLUMN details     JSONB,
    ADD COLUMN detailed_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE listings DROP COLUMN detailed_at, DROP COLUMN details, DROP COLUMN feed;
