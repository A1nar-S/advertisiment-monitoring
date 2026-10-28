# Project notes (compacted from the planning conversation, 2026-10-28)

Read this first when picking the project back up.

## Goal
Full-fledged advertisement monitoring: scrape listing sites (SS.com first), detect new
listings, notify users on Telegram / Discord / other platforms, simple dashboard,
multi-user support. Backend in **Go**. Overengineering is a feature, not a bug.

## Starting point
The repo began as a docker-compose (Kafka KRaft + Postgres) with empty service folders; the
event-driven direction is the one we are building towards.

## Decisions
1. **Single Go module, multiple binaries** (`cmd/*`), not a module/repo per service. Atomic
   cross-service changes, one dependency set, `internal/` enforces boundaries. Services still
   deploy and scale independently.
2. **Vertical slice organisation** inside `internal/`: group by feature, not by layer. A slice
   owns its types, storage, migrations, logic and tests. Slices do not import each other's
   internals.
   - `internal/events`: the only shared package. Contracts crossing slices (become Kafka payloads).
   - `internal/scraping/<source>`: fetch and parse a site (`ss`, later `city24`, ...); `internal/sources` is the plugin interface.
   - `internal/listings`: persistence, upsert, new-listing detection. Owns the `listings` table.
   - `internal/notifications`: `Notifier` interface, idempotent `Service`, one subpackage per channel (`discord`, later `telegram`).
   - `internal/subscriptions`: users, channels, searches, matching.
   - `internal/platform`: config, db/migrations runner, later kafka/otel/logging helpers.
3. **Target runtime architecture (the overengineered part):** Kafka event bus with services
   `scheduler -> scraper -> ingest (outbox) -> matcher -> notifier`, plus `api` and a dashboard
   (htmx or SvelteKit). protobuf schemas, per-source rate limiting/circuit breakers/proxies,
   DLQ + idempotency keys in notifier, OpenTelemetry/Prometheus/Loki, testcontainers, CI,
   Helm later.
4. **Database: sqlc + goose (not GORM).** goose manages migrations, sqlc generates typed query
   code from hand-written SQL. GORM's AutoMigrate is not a real migration manager and a model
   per table fights the slice layout. Each slice owns `migrations/` (goose files), `queries/`,
   `sqlc.yaml` and generated code (`<slice>db/`); regenerate with `go generate ./...`. Each slice
   tracks versions in its own table (`<slice>_migrations`) via `platform/db.Migrate`.
5. **Suggested libs:** `franz-go`, `pgx`, `connect-go`/gRPC, `chi`,
   `goquery`, `discordgo`, `go-telegram/bot`, `otel`.

## Architecture (built)
```
scraper --listings.discovered--> ingest (upsert + outbox) --listings.new--> matcher --notifications.requested--> notifier
```
- **Sources are plugins** (`internal/sources.Source`): `FetchFeed`, `PlanFeeds` (merge feeds to cut requests),
  `InScope` (does an ad belong to a search's feeds). `internal/events.ListingDiscovered` is source-neutral: common
  fields plus `Attrs` (filterable facts) and `Fields` (what to display). Register a new site in `internal/app/sources.go`.
  Searches filter generically on `Attrs` keys (`{min,max}` ranges, string sets, `price`), so cars/vinted need no core change.
- **ss.com** (`internal/scraping/ss`): a feed is a category path below `/lv/` (`real-estate/flats/riga/centre/hand_over`,
  `transport/cars/audi/a4`). The parser is **header-driven** (reads column meaning from the page's header row; unknown
  layout = error, not guesswork). New category = new entries in `columns`.
- **Scrape budget:** feeds are derived from users' searches, de-duplicated, and >`FEED_COLLAPSE_AT` siblings merge into the
  parent feed. Page 1 only, page 2+ only if all of page 1 was unseen. Global request gap, jitter, backoff on 403/429/5xx.
  Feeds are ordered by last bump, not ID, but a new ad always lands in the top rows (tested on saved pages).
- **No spam on first start:** a feed's first scrape (`feed_state`, `SEED_WINDOW_SECONDS`) is stored silently, for any new
  feed, including one added by a new search later. Cost: a genuinely new ad inside that window is not announced.
  Ads that appear while the scraper is down *are* announced on restart (up to the 30 on page 1).
- **Delivery:** at-least-once; dedup by DB keys (`listings` unique, `matches`, `deliveries` idempotency key).
  Matches are recorded after publishing, so a crash re-publishes rather than loses; the notifier dedups.
- **Lifecycle:** `go run ./cmd/admin purge` deletes listings not *seen* for `RETENTION_HOURS` (last_seen, not first_seen, or a
  still-listed ad would be re-announced). Matches/deliveries are kept 2x longer. Cron it later.
- **Services:** `cmd/{scraper,ingest,matcher,notifier,admin}` are thin; wiring lives in `internal/app` so tests run the real thing.

## Testing
- `go test ./...` unit tests; slice tests need `TEST_DATABASE_URL` (skipped otherwise).
- `go test -tags integration ./internal/app` runs scraper+ingest+matcher+notifier against real Postgres and Kafka
  (testcontainers, needs Docker) with a fake ss.com and fake Discord. It covers silent prefill, exactly-once
  notification, scraper restart, a new feed added while running, feed collapsing, and flats + cars. Set
  `TEST_DATABASE_URL` + `TEST_KAFKA_BROKERS` to use servers you already run.
- `SS_LIVE=1 go test ./internal/scraping/ss -run TestLiveStructure` compares the saved pages' column headers with the live
  site (a few real requests). Run it when the scraper starts failing, or now and then.
- `golangci-lint run ./...` and `golangci-lint fmt ./...` (config in `.golangci.yml`, includes depguard slice boundaries).

## Not verified
- Discord delivery against a real token and channel (the notifier is tested with a fake).
- Telegram does not exist yet; it is one more `Notifier`.
- `docker compose --profile app up` (the services in containers) builds an image but was not run end to end.

## Next steps
1. Real Discord token run; Telegram notifier.
2. Purge on a schedule (cron/compose job); optional burst guard (digest if one search matches >N ads in a few minutes).
3. `api` + dashboard slice; chat commands to edit searches.
4. More sources (city24, vinted) behind `sources.Source`; protobuf schemas, DLQ, observability, CI.

## Running it
```sh
cp .env.example .env && cp infra/.env.example infra/.env     # fill in; DB_* must match POSTGRES_* ; set KAFKA_LOG_DIRS=/var/lib/kafka/data
cp config/users.example.yaml config/users.yaml               # who watches what
docker compose -f infra/docker-compose.yml up -d             # postgres, kafka, topics
go run ./cmd/ingest & go run ./cmd/matcher & go run ./cmd/notifier & go run ./cmd/scraper
# or everything in containers: docker compose -f infra/docker-compose.yml --profile app up -d --build
go run ./cmd/admin purge            # lifecycle; also: admin seed | admin migrate
go generate ./...                   # after changing a slice's migrations/ or queries/
```
Note: `go.mod` is on Go 1.26 (sqlc v1.31 requires it); an older local Go auto-downloads the toolchain.
Kafka's data is a named volume (`kafka-data`): the image runs as uid 1000 and a bind mount is created root-owned.
