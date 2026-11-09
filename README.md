# Advertisement monitoring

Watches ss.com (cars, flats) and sends new ads matching people's searches to Discord.
Details: [docs/PROJECT_NOTES.md](docs/PROJECT_NOTES.md). Plans: [docs/ROADMAP.md](docs/ROADMAP.md).

## Run

Needs Docker, `make`, `openssl`.

```sh
make env    # creates .env; then set DISCORD_BOT_TOKEN
make up     # start everything
make logs
```

`make down` stops it.

## Discord bot

1. https://discord.com/developers/applications → New Application → Bot → Reset Token → `DISCORD_BOT_TOKEN`.
2. OAuth2 → URL Generator: scopes `bot`, `applications.commands`; permissions View Channels, Send Messages,
   Embed Links. Open the URL to add the bot to your server.

Optional in `.env`: more admins in `ADMIN_USERS` (the app's owner is one already), and `DISCORD_GUILD_ID` to
register commands on one server only.

## Commands

- `/watch cars name:Parents make:Skoda, Toyota price_max:7000 channel:#monitoring`
- `/watch flats name:Rent region:Rīga deal:Izīrē rooms_min:2`
- `keywords:quattro, 4x4` (cars): only ads whose text has one of the words.
- `/edit cars name:Parents price_max:9000 clear:model` changes only what you give.
- `/searches`, `/unwatch`, `/matches` (recent alerts), `/dashboard` (browser login link).

Alerts go to your DMs, or to `channel:` if you can post there. `/edit … clear:channel` moves them back to DMs.

Admins: `/setup status-channel`, `/setup status-off`, `/setup show` for the service's start/stop/error messages.

## Dashboard

http://127.0.0.1:8080, log in with `/dashboard`. To reach it from elsewhere, use a tunnel and set `PUBLIC_URL`.
