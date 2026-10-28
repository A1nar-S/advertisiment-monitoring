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
   - `internal/scraping/<source>`: fetch and parse a site (`ss`, later `city24`, ...).
   - `internal/listings`: persistence, upsert, new-listing detection. Owns the `listings` table.
   - `internal/notifications`: `Notifier` interface, `Dispatcher`, one subpackage per channel (`discord`, later `telegram`).
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

## Done so far (walking skeleton, `cmd/bot`)
Runs as **one process wiring all slices directly** (no Kafka yet):
- `scraping/ss`: POST search form (302 redirect followed), parse rows (`tr_*`, skips banner
  `tr_bnr_712`), split district/address on `<br>`, price parsing. Tested against a real trimmed
  SS.com fixture (`testdata/search_result.html`).
- `listings`: single-statement upsert (`ON CONFLICT ... RETURNING (xmax = 0)`, as an sqlc query)
  that reports whether a listing is new; goose migration, version table `listings_migrations`.
  Integration test needs `TEST_DATABASE_URL` and passed against Postgres 15.
- `notifications/discord`: embeds over the REST API (no gateway). Each new listing is its own
  message, sent through `Dispatcher` with a 1s delay.
- `cmd/bot`: ticker loop, graceful shutdown, start/stop/error messages.
- Uniqueness is `(source, external_id)` so multiple sites can coexist.

### Not verified
- Discord delivery has not been run (needs a real bot token and channel).
- On first run every currently listed ad counts as new, so the first crawl posts all of them.
  Probably want a "seed without notifying" mode.

## Next steps (suggested order)
1. Run `cmd/bot` end to end with a real Discord token; add "seed silently on first run".
2. Dockerfile for `cmd/bot` + add the bot to `infra/docker-compose.yml`.
3. Telegram notifier (second implementation of `Notifier`).
4. Introduce `events` over Kafka: split `cmd/bot` into scheduler / scraper / ingest /
   notifier binaries; add the outbox in `listings`.
5. Users + saved searches slice (`searches`, `users`), matcher slice, auth.
6. `api` + dashboard slice.
7. Observability, CI, lint (`golangci-lint`, `depguard` to enforce slice boundaries).

## Running it
```sh
cp .env.example .env            # fill in Discord token/channel
docker compose -f infra/docker-compose.yml up -d postgres   # needs infra/.env, see infra/.env.example
go run ./cmd/bot
go generate ./...               # after changing a slice's migrations/ or queries/
go test ./...                   # listings test is skipped unless TEST_DATABASE_URL is set
```
Note: `infra/` still holds the original Kafka + Postgres compose; the bot's default DB
settings (`admonitorbot`) must match whatever `infra/.env` creates.

Note: `go.mod` is on Go 1.26 (sqlc v1.31 requires it); an older local Go auto-downloads the toolchain.
