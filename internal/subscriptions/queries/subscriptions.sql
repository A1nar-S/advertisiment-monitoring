-- name: UpsertUser :one
INSERT INTO users (name) VALUES (@name)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id;

-- name: UpsertChannel :one
INSERT INTO channels (user_id, kind, target) VALUES (@user_id, @kind, @target)
ON CONFLICT (user_id, kind) DO UPDATE SET target = EXCLUDED.target
RETURNING id;

-- name: UpsertSearch :exec
-- created_at is left alone on update: it marks when the user started watching.
INSERT INTO searches (user_id, channel_id, name, enabled, source, feeds, filters)
VALUES (@user_id, @channel_id, @name, @enabled, @source, @feeds, @filters)
ON CONFLICT (user_id, name) DO UPDATE SET
    channel_id = EXCLUDED.channel_id,
    enabled    = EXCLUDED.enabled,
    source     = EXCLUDED.source,
    feeds      = EXCLUDED.feeds,
    filters    = EXCLUDED.filters;

-- name: ListEnabledSearches :many
SELECT s.id, s.user_id, s.name, s.source, s.feeds, s.filters,
       c.kind AS channel_kind, c.target AS channel_target
FROM searches s
JOIN channels c ON c.id = s.channel_id
WHERE s.enabled AND s.source = @source;

-- name: ListEnabledFeeds :many
SELECT DISTINCT unnest(feeds)::text AS feed FROM searches WHERE enabled AND source = @source;

-- name: ListMatchedSearchIDs :many
SELECT search_id FROM matches WHERE source = @source AND external_id = @external_id;

-- name: RecordMatch :exec
INSERT INTO matches (search_id, source, external_id) VALUES (@search_id, @source, @external_id)
ON CONFLICT DO NOTHING;

-- name: PurgeMatches :execrows
DELETE FROM matches WHERE created_at < now() - make_interval(secs => @older_than_seconds::float8);
