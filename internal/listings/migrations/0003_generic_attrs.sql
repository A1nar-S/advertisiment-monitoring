-- +goose Up
-- The first schema was real-estate shaped. Anything specific to a site now
-- lives in attrs so a new source (cars, vinted, ...) needs no migration.
ALTER TABLE listings
    ADD COLUMN location TEXT  NOT NULL DEFAULT '',
    ADD COLUMN attrs    JSONB NOT NULL DEFAULT '{}';

UPDATE listings SET attrs = jsonb_strip_nulls(jsonb_build_object(
    'rooms',    rooms,
    'area_m2',  area_m2,
    'series',   NULLIF(series, ''),
    'district', NULLIF(district, ''),
    'address',  NULLIF(address, ''),
    'category', NULLIF(category, '')
));

ALTER TABLE listings
    DROP COLUMN category,
    DROP COLUMN district,
    DROP COLUMN address,
    DROP COLUMN rooms,
    DROP COLUMN area_m2,
    DROP COLUMN series,
    DROP COLUMN has_photo;

-- +goose Down
ALTER TABLE listings
    ADD COLUMN category  TEXT    NOT NULL DEFAULT '',
    ADD COLUMN district  TEXT    NOT NULL DEFAULT '',
    ADD COLUMN address   TEXT    NOT NULL DEFAULT '',
    ADD COLUMN rooms     INTEGER,
    ADD COLUMN area_m2   INTEGER,
    ADD COLUMN series    TEXT    NOT NULL DEFAULT '',
    ADD COLUMN has_photo BOOLEAN NOT NULL DEFAULT FALSE;
-- attrs is not mapped back: the old columns were real-estate only.
ALTER TABLE listings DROP COLUMN attrs, DROP COLUMN location;
