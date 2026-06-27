# M00 — Project Foundation Demo

## What was built

Project skeleton for Sevitout: Go module declaration, full directory structure
(`cmd/`, `internal/`, `proto/`, `migrations/`, `web/`, `deploy/`, `demo/`),
`Makefile` with standard targets, `golangci-lint` config, Docker Compose stack
(PostgreSQL 16 + golang-migrate), the initial baseline migration, and
`.env.example` with all required environment variable keys.

No application source code exists yet — this milestone establishes the
foundation that all subsequent milestones build upon.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose v2
- [Go 1.24+](https://go.dev/dl/)
- [golangci-lint](https://golangci-lint.run/usage/install/) (`brew install golangci-lint` on macOS)
- No prior milestones required

## Start the stack

```bash
# Copy env file and verify defaults look correct
cp .env.example .env

# Pull images and start PostgreSQL in the background
make up
```

PostgreSQL starts on port `5432`. Wait for it to report healthy (about 5 s).

## Walkthrough

### 1. Run the initial migration

```bash
make migrate
```

Expected output (last line):

```
migrate_1  | 1/u initial (Xms)
```

The `migrate` container exits with code 0. golang-migrate creates and tracks
the `schema_migrations` table automatically.

### 2. Verify the database

Connect and confirm the tracking table exists:

```bash
make psql
```

Once inside the psql prompt:

```sql
\dt
```

Expected output:

```
              List of relations
 Schema |       Name        | Type  |  Owner
--------+-------------------+-------+---------
 public | schema_migrations | table | sevitout
```

### 3. Verify Go tooling

```bash
# Module is valid — doc.go gives the toolchain a valid package to analyze
make build
# → (no output, exit 0)

make test
# → ?   github.com/g8rswimmer/sevitout  [no test files]

# Linter passes
make lint
# → (no output, exit 0)
```

All three exit 0.

## Verify tests pass

```bash
go test ./...
golangci-lint run
```

Both exit 0. `go test` reports `[no test files]` — M00 has no test code yet.

## Tear down

```bash
make down
```

Add `-v` to also remove the Postgres data volume:

```bash
docker compose -f deploy/docker-compose.yml down -v
```

## Known limitations

- No application code, no API, no server binary — those begin in M01.
- `doc.go` at the repo root is the only Go source file; it exists solely to
  give `go build`, `go test`, and `golangci-lint` a valid package to analyze.
- The `migrate` service uses the official `migrate/migrate` Docker Hub image.
  Later milestones will introduce a project `Dockerfile` and switch to
  `build: .` for the migrate service.
- `web/` contains only placeholder `.gitkeep` files; the React frontend is
  implemented in M14.
- `API_GRPC_ADDR=api:8080` in `.env.example` is a forward reference — the
  `api` Docker service is defined in a later milestone. Update this value
  when the service is added to `deploy/docker-compose.yml`.
- `DATABASE_URL` in `.env.example` uses `localhost` for host-machine tool
  connections (psql, migrations via CLI). The `migrate` Docker service uses
  the `postgres` service name directly in its command to avoid this mismatch.
