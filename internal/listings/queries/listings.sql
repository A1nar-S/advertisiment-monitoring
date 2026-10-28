-- name: UpsertListing :one
-- Inserts a listing or refreshes a known one. xmax is zero only on a fresh
-- insert, which tells the caller whether the listing is new.
INSERT INTO listings (
    source, external_id, title, url, location, attrs, price, price_value, image_url, posted_at
) VALUES (
    @source, @external_id, @title, @url, @location, @attrs, @price, @price_value, @image_url, @posted_at
)
ON CONFLICT (source, external_id) DO UPDATE SET
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
