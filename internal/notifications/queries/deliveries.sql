-- name: ClaimDelivery :execrows
-- Zero rows affected means another attempt already claimed this key.
INSERT INTO deliveries (key) VALUES (@key) ON CONFLICT DO NOTHING;

-- name: ReleaseDelivery :exec
DELETE FROM deliveries WHERE key = @key;

-- name: PurgeDeliveries :execrows
DELETE FROM deliveries WHERE delivered_at < now() - make_interval(secs => @older_than_seconds::float8);
