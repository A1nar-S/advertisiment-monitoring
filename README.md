# Advertisement monitoring

Watches listing sites (ss.com first: flats, cars) and notifies each user on Discord about new ads matching
their searches. Event-driven Go services over Kafka and Postgres, built so another site is one plugin.

See [docs/PROJECT_NOTES.md](docs/PROJECT_NOTES.md) for the architecture, how to run it and how to test it.

The [offer monitoring roadmap](docs/ROADMAP.md) records the planned
single-platform expansion into product price watches, price history, and per-user alert routing.

## Local dashboard

Vue 3 + TypeScript SPA, built and run with **Bun** (no SSR). The read-only Go API
shows database totals, the latest 100 listings, and the latest 100 saved searches.
Listings and searches can be filtered; search rows expand to show feeds and filters.
The dashboard refreshes every 30 seconds. An empty database shows empty states, not sample data.

Start a local database with the example credentials (development only):

```sh
docker compose --env-file infra/.env.example -f infra/docker-compose.yml up -d postgres
export DATABASE_URL=postgres://your_user:your_password@localhost:5432/ads_monitor
go run ./cmd/admin migrate
go run ./cmd/api
```

In another terminal:

```sh
cd web
bun install
bun run dev
```

Open the URL Vite prints, normally http://127.0.0.1:5173. Vite proxies `/api`
to the Go API at http://127.0.0.1:8080. Both bind to loopback only. These commands
do not start scraping or notification workers. Existing database credentials can
instead be loaded from `.env` or supplied using `DATABASE_URL`.

```sh
go test ./internal/dashboard ./cmd/api
cd web
bun run format:check
bun run build
bun run preview
```

`bun run build` produces `web/dist`; the preview also proxies to the running Go API.
There is no authentication or search editing yet. Do not expose this admin interface
publicly without adding authentication and access controls.
