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
- **ss.com** (`internal/scraping/ss`): a feed is a category path below `/lv/` (`transport/cars`,
  `real-estate/flats/riga/centre/hand_over`). The parser is **header-driven** (reads column meaning from the page's header
  row; unknown layout = error, not guesswork). New category = new entries in `columns`.
- **Codebooks** (`internal/scraping/ss/codebook/{cars,flats}.json`, embedded): ss.com's own search-form vocabulary
  (field IDs, option names, makes→models, regions→districts with URL slugs). Searches are written in these names and
  validated at seed time (`sources.SearchNormalizer`: unknown names fail with "did you mean"). Parsed attrs use the same
  names, and ad pages are read through the same field IDs (`td#tdo_<id>`). A checked-in snapshot rather than a DB table:
  versioned, offline, reviewable diffs. Refresh with `go run ./cmd/admin ss-codebook` (~85 requests). A DB table becomes
  worthwhile when the dashboard edits searches.
- **Cars = one shared feed.** Every car search uses `feeds: [transport/cars]` (~1000 new ads/day, about 3.5 per 5 min), so one
  request per cycle serves all users. Filters on list columns are free (make, model, year, engine, fuel, mileage, price).
  Ad-page-only fields (gearbox, body, color, inspection, equipment; flats: house type) are read **lazily by the matcher**
  (`sources.Enricher`): only for a new ad that passes every cheap filter of some search, at most once per ad, shared by all
  searches. If the ad page fails, candidates get the alert flagged "⚠️ Unconfirmed" rather than nothing; a removed ad
  matches nothing. ss.com's own server search was rejected: it is POST + session, 2 requests per search per cycle.
- **Flats:** `feeds: [real-estate/flats]` + `region`/`district`/`deal` compile into feed paths (shared by everyone watching
  the area); the rest are local filters. Full feed paths still work as before.
  Persisted source health and admin failure/recovery alerts are not implemented yet; they are
  a required foundation in [ROADMAP.md](ROADMAP.md#monitoring-required-for-scraping).
- **Scrape budget and paging:** feeds are derived from users' searches, de-duplicated, and >`FEED_COLLAPSE_AT` siblings
  merge into the parent feed. Feeds are ordered by last bump, not ID, so a known row at the *top* means nothing (it was
  bumped). The runner fetches the next page until the **last 2 rows** of a page are both known, up to `MAX_PAGES` (5). "Seen" is
  the listings table plus the previous cycle in memory, so a restart catches up on downtime. A feed's first-ever scrape
  is page 1 only (it is stored silently anyway). Hitting `MAX_PAGES` without reaching known rows sends one admin alert
  ("feed busier than N pages") and halves the wait until it catches up. Global request gap, jitter, backoff on
  403/429/5xx; with no feeds yet it re-checks every minute.
- **No spam on first start:** a feed's first scrape (`feed_state`, `SEED_WINDOW_SECONDS`) is stored silently, for any new
  feed, including one added by a new search later. Cost: a genuinely new ad inside that window is not announced.
  A new search on an already watched feed (all car searches) never back-announces: the matcher sees only new ads.
- **Flood guard** (`internal/notifications`): at most `NOTIFY_BURST` (5) ad messages per (user, search) per
  `NOTIFY_BURST_WINDOW_MINUTES` (10); the rest wait in `digest_queue` and go out as one digest per search when the window
  ends. Covers catch-up after downtime, broad searches and bugs alike. Operational texts are never limited.
- **Delivery:** at-least-once; dedup by DB keys (`listings` unique, `matches`, `deliveries` idempotency key).
  Matches are recorded after publishing, so a crash re-publishes rather than loses; the notifier dedups.
- **Lifecycle:** `make purge` / `go run ./cmd/admin purge` deletes listings not *seen* for `RETENTION_HOURS` (default 60
  days). The listings table doubles as the "already announced" memory: a shorter retention re-announces old ads when they
  are bumped. Matches/deliveries are kept 2x longer. Cron it weekly.
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
  notification, scraper restart, a new feed added while running, feed collapsing, and flats + cars. `TestCarsPipeline`
  covers the shared cars feed: ad pages read only for candidates, and a restart while 45 bump-ordered ads land on two
  pages (all announced exactly once, 5 singly and the rest in a digest). Set
  `TEST_DATABASE_URL` + `TEST_KAFKA_BROKERS` to use servers you already run.
- `SS_LIVE=1 go test ./internal/scraping/ss -run TestLiveStructure` compares the saved pages' column headers with the live
  site (a few real requests). Run it when the scraper starts failing, or now and then.
- `golangci-lint run ./...` and `golangci-lint fmt ./...` (config in `.golangci.yml`, includes depguard slice boundaries).
- golangci-lint must be built with Go ≥ 1.26 (older release images refuse the module); `go install
  github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest` works.
- Postgres data now lives in the named volume `pg-data` (it used to be a root-owned bind mount under `infra/postgres`).
- Frontend checks: `cd web && bun run format:check && bun run build`. Apply formatting with
  `bun run format`; Prettier is pinned in the frontend development dependencies.

## Not verified
- Discord delivery against a real token and channel (the notifier is tested with a fake). The full Docker stack
  (`make up`) was run end to end up to the notifier on 2026-11-08; the notifier waits for a real `DISCORD_BOT_TOKEN`.
- Telegram does not exist yet; it is one more `Notifier`.

## Running it
See the README ("Run it"). In short:
```sh
make env && make users          # .env (random DB password) and config/users.yaml; then fill in Discord values
make check-users                # validate searches against the codebooks
make up                         # Postgres, Kafka, topics, scraper, ingest, matcher, notifier (all in Docker)
make logs | make reload-users | make purge | make down
# Services on the host instead: make infra, then go run ./cmd/ingest & ./cmd/matcher & ./cmd/notifier & ./cmd/scraper
go run ./cmd/admin ss-codebook  # refresh the ss.com codebooks
go generate ./...               # after changing a slice's migrations/ or queries/
```
Note: `go.mod` is on Go 1.26 (sqlc v1.31 requires it); an older local Go auto-downloads the toolchain.
Kafka's data is a named volume (`kafka-data`): the image runs as uid 1000 and a bind mount is created root-owned.
