COMPOSE = docker compose --project-directory . -f deploy/docker-compose.yml --env-file .env

PROTO_DIR     = proto
PB_OUT        = .
PROTO_FILES   = $(shell find $(PROTO_DIR)/sevitout -name '*.proto')
GATEWAY_PROTO = $(shell go env GOPATH)/pkg/mod/github.com/grpc-ecosystem/grpc-gateway/v2@v2.19.1

.PHONY: build test lint generate proto up down migrate psql check-env

proto:
	protoc \
	  -I $(PROTO_DIR) \
	  -I $(GATEWAY_PROTO) \
	  --go_out=$(PB_OUT) --go_opt=module=github.com/g8rswimmer/sevitout \
	  --go-grpc_out=$(PB_OUT) --go-grpc_opt=module=github.com/g8rswimmer/sevitout \
	  --grpc-gateway_out=$(PB_OUT) --grpc-gateway_opt=module=github.com/g8rswimmer/sevitout \
	  --openapiv2_out=internal/api/pb \
	  $(PROTO_FILES)

generate:
	sqlc generate

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run

check-env:
	@test -f .env || (echo "ERROR: .env not found — run: cp .env.example .env" && exit 1)

up: check-env
	$(COMPOSE) up --build

down: check-env
	$(COMPOSE) down

migrate: check-env
	$(COMPOSE) run --rm migrate

migrate-down: check-env
	$(COMPOSE) run --rm migrate -path=/migrations -database "postgres://$${POSTGRES_USER}:$${POSTGRES_PASSWORD}@postgres:5432/$${POSTGRES_DB}?sslmode=disable" down 1

test-integration: check-env
	go test -tags integration -v ./internal/store/...

psql: check-env
	$(COMPOSE) exec postgres bash -c 'psql -U $$POSTGRES_USER -d $$POSTGRES_DB'
