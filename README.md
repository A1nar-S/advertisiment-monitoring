# Advertisement monitoring

Watches ss.com (cars, flats) and posts new ads matching your searches to Discord.
Architecture and tests: [docs/PROJECT_NOTES.md](docs/PROJECT_NOTES.md). Plans: [docs/ROADMAP.md](docs/ROADMAP.md).

## Run

Needs Docker, `make`, `openssl`.

```sh
make env           # .env with a random DB password; then set DISCORD_BOT_TOKEN, ADMIN_CHANNEL_ID
make users         # config/users.yaml from the example; then set your channel ID and searches
make check-users   # validate searches
make up            # start everything
make logs
```

`make reload-users` after editing searches, `make down` to stop. Run it on one machine at a time.

## Discord bot

1. https://discord.com/developers/applications → New Application → Bot → Reset Token → `DISCORD_BOT_TOKEN`.
2. OAuth2 → URL Generator: scope `bot`, permissions View Channels, Send Messages, Embed Links. Open the URL, add the bot.
3. Discord Developer Mode on, right-click a channel → Copy Channel ID.

## Dashboard (optional)

```sh
go run ./cmd/api          # with the stack running
cd web && bun install && bun run dev   # http://127.0.0.1:5173
```

Local only, no authentication.
