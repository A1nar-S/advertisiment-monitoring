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
bot (Discord) ─┐
dashboard api ─┴─> subscriptions (users, channels, searches, logins) <── read live by scraper and matcher
```
- **Self-service, no config file.** People manage their own searches with the chat bot or the dashboard; there is no
  users file. Both go through one write path, `subscriptions.Service` (`CreateSearch`/`UpdateSearch`/`SaveSearchByName`,
  `SetEnabled`, `DeleteSearch`), which compiles the search through the source's `sources.Catalog` (validation, "did you
  mean") and the filter parser. A search is a **category** (`cars`, `flats`) plus form-style filters; users never see feeds.
  `searches.spec` keeps what the user wrote (for editing), `feeds`/`filters` what the pipeline runs on. The scraper and
  matcher read the table every cycle/ad, so changes apply without restarts. Per-user limit `MAX_SEARCHES_PER_USER`;
  `ADMIN_USERS` (`platform:id`) see and edit everyone's. Another user's search is "not found".
- **Identity:** a chat account, `users(platform, external_id)`; channels are `(user, kind=platform, target)` with a label.
  The channel kind is the notifier kind, so alerts for a Telegram user would go through a Telegram notifier.
- **Bot** (`internal/bot`): platform-neutral core (commands as specs, `Run`/`Complete`/`Act`, replies with neutral menus)
  behind a `Messenger` interface; `internal/bot/discordbot` is the Discord adapter (slash commands, autocomplete from the
  codebooks, select menus; gateway connection, so it works behind NAT). Depguard keeps discordgo out of the core. A
  Telegram adapter would implement `Messenger` and map the same specs to `/watch cars make=… price_max=…` and inline
  keyboards. Commands: `/watch cars|flats`, `/edit cars|flats` (merges the given filters into the search; `clear:`
  removes filters, bounds, whole ranges or the channel). Car searches take `keywords` (any of the words in the ad text:
  the list snippet first, the ad page's description only if needed; `Filters.Contains` on the `text` attr), `/searches`, `/matches`, `/unwatch`, `/dashboard`. Users can
  manage their own searches fully from the bot; the dashboard is optional. `channel:` is accepted only where the user may
  post (View + Send Messages, Manage Channels or Administrator): the adapter works it out from the gateway's cached roles
  and overrides and passes it as `Call.Postable`. `/setup` (status channel, stored in the `settings` table and read by
  the scraper per status message; `ADMIN_CHANNEL_ID` is the fallback) is shown to Manage Server members only and needs a
  bot admin: `ADMIN_USERS` plus the Discord app's owner or team, read at start. Alerts go by DM unless `channel:` is given; closed DMs get a warning, and the notifier drops "cannot DM"/"unknown channel" errors instead of retrying.
- **Dashboard login:** `/dashboard` replies with a one-time link (`PUBLIC_URL/api/login?token=…`, 10 minutes, single use)
  that becomes a 30-day HttpOnly SameSite=Lax session cookie. Only SHA-256 hashes of tokens are stored. Changes need an
  `X-Requested-With` header (CSRF). No OAuth app to set up.
- **Sources are plugins** (`internal/sources.Source`): `FetchFeed`, `PlanFeeds` (merge feeds to cut requests),
  `InScope` (does an ad belong to a search's feeds). `internal/events.ListingDiscovered` is source-neutral: common
  fields plus `Attrs` (filterable facts) and `Fields` (what to display). Register a new site in `internal/app/sources.go`.
  Searches filter generically on `Attrs` keys (`{min,max}` ranges, string sets, `price`), so cars/vinted need no core change.
- **ss.com** (`internal/scraping/ss`): a feed is a category path below `/lv/` (`transport/cars`,
  `real-estate/flats/riga/centre/hand_over`). The parser is **header-driven** (reads column meaning from the page's header
  row; unknown layout = error, not guesswork). New category = new entries in `columns`.
- **Codebooks** (`internal/scraping/ss/codebook/{cars,flats}.json`, embedded): ss.com's own search-form vocabulary
  (field IDs, option names, makes→models, regions→districts with URL slugs). Searches are written in these names and
  validated on save (`sources.Catalog.CompileSearch`: unknown names fail with "did you mean"). Parsed attrs use the
  same names, and ad pages are read through the same field IDs (`td#tdo_<id>`). `Catalog.Categories()` serves them to
  the bot's autocomplete and the dashboard's forms. A checked-in snapshot rather than a DB table: versioned, offline,
  reviewable diffs. Refresh with `go run ./cmd/admin ss-codebook` (~85 requests), then rebuild.
- **Cars = one shared feed.** Every car search watches `transport/cars` (~1000 new ads/day, about 3.5 per 5 min), so one
  request per cycle serves all users. Filters on list columns are free (make, model, year, engine, fuel, mileage, price).
  Ad-page-only fields (gearbox, body, color, inspection, equipment; flats: house type) are read **lazily by the matcher**
  (`sources.Enricher`): only for a new ad that passes every cheap filter of some search, at most once per ad, shared by all
  searches. If the ad page fails, candidates get the alert flagged "⚠️ Unconfirmed" rather than nothing; a removed ad
  matches nothing. ss.com's own server search was rejected: it is POST + session, 2 requests per search per cycle.
- **Flats:** `region`/`district`/`deal` compile into feed paths (shared by everyone watching the area); the rest are local
  filters.
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
- **Services:** `cmd/{scraper,ingest,matcher,notifier,bot,api,admin}` are thin; wiring lives in `internal/app` so tests
  run the real thing.
- **Dashboard:** `web/` is a Vue 3 + TypeScript SPA (Bun, Vite), built into every image and served by `cmd/api`
  (`internal/web`: API + SPA). Tabs: your searches (editor generated from `/api/categories`), your alerts, and for
  admins the old read-only overview (`internal/dashboard`'s cross-slice queries). Compose binds it to `127.0.0.1:8080`;
  publish it through a tunnel and set `PUBLIC_URL` for others. `cd web && bun run dev` proxies `/api` to a local API.

## Testing
- `go test ./...` unit tests; slice tests need `TEST_DATABASE_URL` (skipped otherwise). The bot core is tested with a
  fake `Messenger`, the Discord adapter's translation on its own, the web API with `httptest`.
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
- Anything against a real Discord token: delivery, slash-command registration, autocomplete, select menus. The full
  Docker stack was run on 2026-11-08: dashboard login, search create/validate and the overview's admin check worked end to
  end; the bot and the notifier wait for a real `DISCORD_BOT_TOKEN`.
- Telegram does not exist yet: one more `Notifier` plus a bot adapter implementing `bot.Messenger`.

## Running it
See the README. In short:
```sh
make env                        # .env (random DB password); then the Discord values
make up                         # Postgres, Kafka, scraper, ingest, matcher, notifier, bot, dashboard (all in Docker)
make logs | make purge | make down
# Services on the host instead: make infra, then go run ./cmd/<service>; the dashboard with cd web && bun run dev
go run ./cmd/admin ss-codebook  # refresh the ss.com codebooks
go generate ./...               # after changing a slice's migrations/ or queries/
```
Note: `go.mod` is on Go 1.26 (sqlc v1.31 requires it); an older local Go auto-downloads the toolchain.
Kafka's data is a named volume (`kafka-data`): the image runs as uid 1000 and a bind mount is created root-owned.
