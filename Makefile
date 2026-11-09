# Day-to-day commands. Everything runs in Docker; Go is only needed for `make test`.
COMPOSE := docker compose --env-file .env -f infra/docker-compose.yml

.PHONY: help env up infra down logs ps purge test

help:
	@echo "make env     create .env with a random DB password (then add the Discord values)"
	@echo "make up      build and start everything: Postgres, Kafka, scraper, ingest, matcher, notifier, bot, dashboard"
	@echo "make infra   start only Postgres and Kafka (for go run on the host)"
	@echo "make logs    follow the service logs"
	@echo "make ps      show container status"
	@echo "make purge   delete listings not seen for RETENTION_HOURS, and expired logins"
	@echo "make down    stop everything (data is kept in Docker volumes)"

env:
	@test ! -f .env || { echo ".env already exists; not overwriting"; exit 1; }
	@sed "s/^DB_PASSWORD=.*/DB_PASSWORD=$$(openssl rand -hex 24)/" .env.example > .env
	@chmod 600 .env
	@echo "created .env; now set DISCORD_BOT_TOKEN, ADMIN_USERS and ADMIN_CHANNEL_ID"

up: .env
	$(COMPOSE) --profile app up -d --build

infra: .env
	$(COMPOSE) up -d

down:
	$(COMPOSE) --profile app down

logs:
	$(COMPOSE) --profile app logs -f --tail=100 scraper ingest matcher notifier bot api

ps:
	$(COMPOSE) --profile app ps

purge:
	$(COMPOSE) --profile tools run --rm --build admin purge

test:
	go test ./cmd/... ./internal/...

.env:
	@echo "missing .env: run 'make env' first"; exit 1
