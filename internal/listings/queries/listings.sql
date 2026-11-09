-- name: UpsertListing :one
-- Inserts a listing or refreshes a known one. xmax is zero only on a fresh
-- insert, which tells the caller whether the listing is new.
INSERT INTO listings (
    source, external_id, feed, title, url, location, attrs, price, price_value, image_url, posted_at
) VALUES (
    @source, @external_id, @feed, @title, @url, @location, @attrs, @price, @price_value, @image_url, @posted_at
)
ON CONFLICT (source, external_id) DO UPDATE SET
    feed         = CASE WHEN listings.feed = '' THEN EXCLUDED.feed ELSE listings.feed END,
    title        = EXCLUDED.title,
    url          = EXCLUDED.url,
    location     = EXCLUDED.location,
    attrs        = EXCLUDED.attrs,
    price        = EXCLUDED.price,
    price_value  = EXCLUDED.price_value,
    image_url    = EXCLUDED.image_url,
    last_seen_at = now(),
    is_active    = TRUE
RETURNING (xmax = 0)::boolean AS is_new;

-- name: TouchFeed :one
-- Registers a feed on first sight and returns how long ago that was, in
-- seconds. now() is the transaction start, so concurrent callers agree.
INSERT INTO feed_state (source, feed) VALUES (@source, @feed)
ON CONFLICT (source, feed) DO UPDATE SET source = EXCLUDED.source
RETURNING EXTRACT(EPOCH FROM now() - first_seen_at)::float8 AS age_seconds;

-- name: InsertOutbox :exec
INSERT INTO listings_outbox (payload) VALUES (@payload);

-- name: ClaimOutbox :many
SELECT id, payload FROM listings_outbox
WHERE published_at IS NULL
ORDER BY id
LIMIT @batch
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxPublished :exec
UPDATE listings_outbox SET published_at = now() WHERE id = ANY(@ids::bigint[]);

-- name: PurgeStaleListings :execrows
-- Keyed on last_seen_at, not first_seen_at: an ad that is still on a feed page
-- must stay known, or the next scrape would announce it as new again.
DELETE FROM listings WHERE last_seen_at < now() - make_interval(secs => @older_than_seconds::float8);

-- name: PurgePublishedOutbox :execrows
DELETE FROM listings_outbox
WHERE published_at IS NOT NULL AND published_at < now() - make_interval(secs => @older_than_seconds::float8);

-- name: KnownIDs :many
-- Which of these ads are already stored. The scraper uses it to see where the
-- ads it saw before begin, also right after a restart.
SELECT external_id FROM listings WHERE source = @source AND external_id = ANY(@ids::text[]);

-- name: FeedSeen :one
SELECT EXISTS (SELECT 1 FROM feed_state WHERE source = @source AND feed = @feed) AS seen;

-- name: GetListings :many
SELECT external_id, title, url, location, price, image_url,
       (attrs || COALESCE(details, '{}'::jsonb))::jsonb AS attrs, first_seen_at, is_active
FROM listings WHERE source = @source AND external_id = ANY(@ids::text[]);

-- name: RecentListings :many
-- The newest stored ads of a source, with the ad-page details merged in: what
-- a search preview is checked against.
SELECT id, external_id, feed, title, url, location, price, price_value, image_url,
       (attrs || COALESCE(details, '{}'::jsonb))::jsonb AS attrs, posted_at, first_seen_at, is_active
FROM listings WHERE source = @source
ORDER BY first_seen_at DESC, id DESC
LIMIT @max_rows;

-- name: GetListingByID :one
SELECT id, source, external_id, feed, title, url, location, price, price_value, image_url,
       (attrs || COALESCE(details, '{}'::jsonb))::jsonb AS attrs, posted_at, first_seen_at, is_active,
       detailed_at
FROM listings WHERE id = @id;

-- name: SaveListingDetails :exec
-- details replaces the previous read: the ad page is the whole truth.
UPDATE listings SET details = @details, detailed_at = now()
WHERE source = @source AND external_id = @external_id;

-- name: MarkListingGone :exec
-- The ad's page says it was removed; the next purge forgets it.
UPDATE listings SET is_active = FALSE WHERE source = @source AND external_id = @external_id;
