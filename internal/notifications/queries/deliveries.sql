-- name: ClaimDelivery :execrows
-- Zero rows affected means another attempt already claimed this key.
INSERT INTO deliveries (key, user_id, search_name) VALUES (@key, sqlc.narg(user_id), sqlc.narg(search_name))
ON CONFLICT DO NOTHING;

-- name: ReleaseDelivery :exec
DELETE FROM deliveries WHERE key = @key;

-- name: PurgeDeliveries :execrows
DELETE FROM deliveries WHERE delivered_at < now() - make_interval(secs => @older_than_seconds::float8);

-- name: CountRecentSent :one
-- Ad messages this search sent on its own (not in a digest) within the window,
-- not counting the delivery being decided right now.
SELECT count(*) FROM deliveries
WHERE user_id = @user_id AND search_name = @search_name AND NOT digested AND key <> @key
  AND delivered_at > now() - make_interval(secs => @window_seconds::float8);

-- name: MarkDigested :exec
UPDATE deliveries SET digested = TRUE WHERE key = @key;

-- name: QueueDigest :exec
INSERT INTO digest_queue (key, user_id, search_name, channel_kind, channel_target, listing)
VALUES (@key, @user_id, @search_name, @channel_kind, @channel_target, @listing)
ON CONFLICT DO NOTHING;

-- name: DueDigests :many
-- Searches whose oldest queued ad has waited a full window.
SELECT user_id, search_name, channel_kind, channel_target FROM digest_queue
GROUP BY user_id, search_name, channel_kind, channel_target
HAVING min(queued_at) < now() - make_interval(secs => @window_seconds::float8);

-- name: TakeDigest :many
-- Removes one search's queued ads; run inside a transaction that rolls back
-- if the digest cannot be sent.
DELETE FROM digest_queue
WHERE user_id = @user_id AND search_name = @search_name AND channel_kind = @channel_kind AND channel_target = @channel_target
RETURNING listing, queued_at;
