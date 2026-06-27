COMPOSE = docker compose --project-directory . -f deploy/docker-compose.yml --env-file .env

.PHONY: build test lint up down migrate psql

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run

up:
	$(COMPOSE) up

down:
	$(COMPOSE) down

migrate:
	$(COMPOSE) run --rm migrate

psql:
	$(COMPOSE) exec postgres bash -c 'psql -U $$POSTGRES_USER -d $$POSTGRES_DB'
