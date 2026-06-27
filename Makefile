COMPOSE = docker compose --project-directory . -f deploy/docker-compose.yml --env-file .env

.PHONY: build test lint up down migrate psql check-env

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run

check-env:
	@test -f .env || (echo "ERROR: .env not found — run: cp .env.example .env" && exit 1)

up: check-env
	$(COMPOSE) up

down: check-env
	$(COMPOSE) down

migrate: check-env
	$(COMPOSE) run --rm migrate

psql: check-env
	$(COMPOSE) exec postgres bash -c 'psql -U $$POSTGRES_USER -d $$POSTGRES_DB'
