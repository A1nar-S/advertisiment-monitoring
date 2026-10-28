# Project notes (compacted from the planning conversation, 2026-10-28)

Read this first when picking the project back up. Future work and priorities live in
[ROADMAP.md](ROADMAP.md).

## Goal
One multipurpose offer-monitoring platform: discover new classified ads and track price,
discount, and availability changes on known offers. Examples: an Audi search, flats in Riga,
or waiting for a particular Apple Watch to reach a target price. Shared dashboard, users,
source adapters, storage, and notification delivery. Backend in **Go**.

Minimize maintenance overhead: evolve this repo and reuse the existing pipeline rather
than creating a separate application or bot for each website or watch type. Retail and
price-change monitoring are planned, not implemented; see [ROADMAP.md](ROADMAP.md).

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
3. **Runtime architecture:** Kafka event bus with services
  `scraper -> ingest (outbox) -> matcher -> notifier`, plus `api` and a dashboard
  (Vue 3 SPA with Bun, no SSR). Future scheduler and infrastructure work is tracked in
  [ROADMAP.md](ROADMAP.md).
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
  Persisted source health and admin failure/recovery alerts are not implemented yet; they are
  a required foundation in [ROADMAP.md](ROADMAP.md#required-foundation-source-health-and-admin-alerting).
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
- **Dashboard:** `web/` is a Vue 3 + TypeScript SPA using Bun and Vite. `cmd/api` serves a read-only
  `GET /api/overview` endpoint on `127.0.0.1:8080`; `internal/dashboard` owns its cross-slice reporting queries.
  Vite proxies API requests. No authentication yet: local access only. See README for startup commands.
  The listings view has an All / Cars / Flats filter over the latest 100 loaded records. For now it derives
  categories from ss.com category paths in `location`; unrecognized categories remain visible under All.

## Testing
- `go test ./...` unit tests; slice tests need `TEST_DATABASE_URL` (skipped otherwise).
- `go test -tags integration ./internal/app` runs scraper+ingest+matcher+notifier against real Postgres and Kafka
  (testcontainers, needs Docker) with a fake ss.com and fake Discord. It covers silent prefill, exactly-once
  notification, scraper restart, a new feed added while running, feed collapsing, and flats + cars. Set
  `TEST_DATABASE_URL` + `TEST_KAFKA_BROKERS` to use servers you already run.
- `SS_LIVE=1 go test ./internal/scraping/ss -run TestLiveStructure` compares the saved pages' column headers with the live
  site (a few real requests). Run it when the scraper starts failing, or now and then.
- `golangci-lint run ./...` and `golangci-lint fmt ./...` (config in `.golangci.yml`, includes depguard slice boundaries).
- If a local Postgres bind mount makes root-wide commands fail with permission denied, use
  `go test ./cmd/... ./internal/...`, `golangci-lint run ./cmd/... ./internal/...`, and
  `golangci-lint fmt --diff ./cmd ./internal` to check all Go source without traversing database files.
- Frontend checks: `cd web && bun run format:check && bun run build`. Apply formatting with
  `bun run format`; Prettier is pinned in the frontend development dependencies.

## Not verified
- Discord delivery against a real token and channel (the notifier is tested with a fake).
- Telegram does not exist yet; it is one more `Notifier`.
- `docker compose --profile app up` (the services in containers) builds an image but was not run end to end.

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
