# Day-to-day commands. Everything runs in Docker; Go is only needed for `make test`.
COMPOSE := docker compose --env-file .env -f infra/docker-compose.yml

.PHONY: help env users check-users up infra down logs ps reload-users purge test

help:
	@echo "make env           create .env with a random DB password (then add the Discord values)"
	@echo "make users         create config/users.yaml from the example (then edit it)"
	@echo "make check-users   validate config/users.yaml (typos get a did-you-mean)"
	@echo "make up            build and start Postgres, Kafka and all services"
	@echo "make infra         start only Postgres and Kafka (for go run on the host)"
	@echo "make logs          follow the service logs"
	@echo "make ps            show container status"
	@echo "make reload-users  re-read config/users.yaml (restarts the matcher)"
	@echo "make purge         delete listings not seen for RETENTION_HOURS"
	@echo "make down          stop everything (data is kept in Docker volumes)"

env:
	@test ! -f .env || { echo ".env already exists; not overwriting"; exit 1; }
	@sed "s/^DB_PASSWORD=.*/DB_PASSWORD=$$(openssl rand -hex 24)/" .env.example > .env
	@chmod 600 .env
	@echo "created .env; now set DISCORD_BOT_TOKEN and ADMIN_CHANNEL_ID"

users:
	@test ! -f config/users.yaml || { echo "config/users.yaml already exists; not overwriting"; exit 1; }
	@cp config/users.example.yaml config/users.yaml
	@echo "created config/users.yaml; set your Discord channel ID and searches"

up: .env config/users.yaml
	$(COMPOSE) --profile app up -d --build

infra: .env
	$(COMPOSE) up -d

down:
	$(COMPOSE) --profile app down

logs:
	$(COMPOSE) --profile app logs -f --tail=100 scraper ingest matcher notifier

ps:
	$(COMPOSE) --profile app ps

check-users: .env config/users.yaml
	$(COMPOSE) --profile tools run --rm --no-deps --build admin check

reload-users: check-users
	$(COMPOSE) --profile app restart matcher

purge:
	$(COMPOSE) --profile tools run --rm --build admin purge

test:
	go test ./cmd/... ./internal/...

.env:
	@echo "missing .env: run 'make env' first"; exit 1

config/users.yaml:
	@echo "missing config/users.yaml: run 'make users' first"; exit 1
