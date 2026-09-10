COMPOSE = docker compose --project-directory . -f deploy/docker-compose.yml --env-file .env

PROTO_DIR     = proto
PB_OUT        = .
PROTO_FILES   = $(shell find $(PROTO_DIR)/sevitout -name '*.proto')
GATEWAY_PROTO = $(shell go env GOPATH)/pkg/mod/github.com/grpc-ecosystem/grpc-gateway/v2@v2.19.1

.PHONY: build test lint generate proto up down migrate psql check-env logs

proto:
	protoc \
	  -I $(PROTO_DIR) \
	  -I $(PROTO_DIR)/third_party \
	  -I $(GATEWAY_PROTO) \
	  --go_out=$(PB_OUT) --go_opt=module=github.com/g8rswimmer/sevitout \
	  --go-grpc_out=$(PB_OUT) --go-grpc_opt=module=github.com/g8rswimmer/sevitout \
	  --grpc-gateway_out=$(PB_OUT) --grpc-gateway_opt=module=github.com/g8rswimmer/sevitout \
	  --openapiv2_out=internal/api/pb \
	  $(PROTO_FILES)
	# Second, separate invocation: protoc doesn't honor two --openapiv2_out
	# flags in one call (the later one wins for both), so the per-service
	# specs above (used individually — e.g. for reviewing one service's
	# shape in isolation) and this merged, all-services spec need their own
	# protoc runs. This one feeds cmd/server/openapi/openapi.json, the spec
	# embedded into the server binary and served at GET /openapi.json / GET
	# /docs (see cmd/server/main.go and openapi.proto for the merged
	# document's title/version/description).
	protoc \
	  -I $(PROTO_DIR) \
	  -I $(PROTO_DIR)/third_party \
	  -I $(GATEWAY_PROTO) \
	  --openapiv2_out=cmd/server/openapi \
	  --openapiv2_opt=allow_merge=true,merge_file_name=openapi \
	  $(PROTO_FILES)
	mv cmd/server/openapi/openapi.swagger.json cmd/server/openapi/openapi.json

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

# Detached (-d): the stack keeps running independent of this terminal.
# Without it, closing the terminal or pressing Ctrl-C sends every container
# (including postgres) a graceful shutdown — easy to trigger by accident,
# and easy to mistake for an application bug (e.g. a blank page) when it's
# actually just "nothing is running anymore." Use `make logs` to follow
# output, `make down` to actually stop it.
up: check-env
	$(COMPOSE) up -d --build

down: check-env
	$(COMPOSE) down

logs: check-env
	$(COMPOSE) logs -f

migrate: check-env
	$(COMPOSE) run --rm migrate

migrate-down: check-env
	$(COMPOSE) run --rm migrate -path=/migrations -database "postgres://$${POSTGRES_USER}:$${POSTGRES_PASSWORD}@postgres:5432/$${POSTGRES_DB}?sslmode=disable" down 1

# ALLOW_DESTRUCTIVE_DB_TESTS is a required, separate opt-in — this suite
# TRUNCATEs every application table at DATABASE_URL. Only run this against a
# database you just created for this purpose; see CLAUDE.md's "Database
# safety" section.
test-integration: check-env
	ALLOW_DESTRUCTIVE_DB_TESTS=1 go test -tags integration -v ./internal/store/...

psql: check-env
	$(COMPOSE) exec postgres bash -c 'psql -U $$POSTGRES_USER -d $$POSTGRES_DB'
