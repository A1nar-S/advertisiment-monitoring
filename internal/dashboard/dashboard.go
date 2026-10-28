package dashboard

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Loader func(context.Context) (json.RawMessage, error)

func Load(pool *pgxpool.Pool) Loader {
	return func(ctx context.Context) (json.RawMessage, error) {
		var data json.RawMessage
		err := pool.QueryRow(ctx, overviewQuery).Scan(&data)
		return data, err
	}
}

func NewHandler(load Loader) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/overview", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		data, err := load(ctx)
		if err != nil {
			slog.Error("dashboard overview", "err", err)
			http.Error(w, "Dashboard data is unavailable. Check the database and migrations.", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(data); err != nil {
			slog.Warn("dashboard response", "err", err)
		}
	})
	return mux
}

const overviewQuery = `
SELECT jsonb_build_object(
  'updatedAt', now(),
  'stats', jsonb_build_object(
    'listings', (SELECT count(*) FROM listings),
    'activeListings', (SELECT count(*) FROM listings WHERE is_active),
    'newToday', (SELECT count(*) FROM listings WHERE first_seen_at >= date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'),
    'searches', (SELECT count(*) FROM searches),
    'enabledSearches', (SELECT count(*) FROM searches WHERE enabled),
    'users', (SELECT count(*) FROM users),
    'matches', (SELECT count(*) FROM matches),
    'deliveries', (SELECT count(*) FROM deliveries)
  ),
  'listings', COALESCE((SELECT jsonb_agg(row_to_json(recent)) FROM (
    SELECT id, source, title, url, location, price, image_url AS "imageUrl",
           is_active AS active, first_seen_at AS "firstSeenAt", attrs
    FROM listings ORDER BY first_seen_at DESC, id DESC LIMIT 100
  ) recent), '[]'::jsonb),
  'searches', COALESCE((SELECT jsonb_agg(row_to_json(saved)) FROM (
    SELECT searches.id, searches.name, users.name AS "userName", searches.source,
           searches.enabled, searches.feeds, searches.filters, channels.kind AS channel,
           (SELECT count(*) FROM matches WHERE search_id = searches.id) AS matches
    FROM searches JOIN users ON users.id = searches.user_id
    JOIN channels ON channels.id = searches.channel_id
    ORDER BY searches.created_at DESC, searches.id DESC LIMIT 100
  ) saved), '[]'::jsonb)
)`
