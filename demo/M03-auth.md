# M03 Demo — Authentication & Authorization

## What was built

Email + password authentication. The API server issues HS256 JWTs on successful
register or login. Every gRPC endpoint is protected by a unary and stream
interceptor that validates the JWT and enforces RBAC (Viewer, Responder,
Incident Commander, Admin). The first registered user automatically receives the
Admin role for bootstrapping. The REST gateway extracts the JWT from either the
`Authorization: Bearer` header or the `token` httpOnly cookie. `WhoAmI`
(`GET /v1/auth/me`) returns the caller's identity.

## Prerequisites

- M02 complete (server builds and SEV API is working)
- `JWT_SECRET` set in `.env` (min 32 characters) — no external credentials needed

## Environment variables (M03 additions)

| Variable | Required | Description |
|---|---|---|
| `JWT_SECRET` | Yes | HMAC signing key, at minimum 32 chars |
| `JWT_TTL_HOURS` | No (default 24) | Token lifetime in hours |

## Start the stack

```bash
cp .env.example .env
# Edit .env — set JWT_SECRET

make up
```

## Walkthrough

### 1. Verify unauthenticated calls are rejected

```bash
# GetSEV without a token → 401 Unauthenticated
curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/sevs/SEV-2026-0001
# Expected: 401

# WhoAmI without a token → 401
curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/v1/auth/me
# Expected: 401
```

### 2. Register the first user (gets Admin role)

```bash
curl -s -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","name":"Admin User","password":"changeme123"}' | jq .
```

Expected response:

```json
{
  "token": "<jwt>",
  "user": {
    "id": "...",
    "email": "admin@example.com",
    "name": "Admin User",
    "org_role": "admin"
  }
}
```

Save the token:

```bash
export ADMIN_TOKEN="<paste token from above>"
```

### 3. Call WhoAmI

```bash
curl -s -H "Authorization: Bearer $ADMIN_TOKEN" http://localhost:8080/v1/auth/me | jq .
```

Expected:

```json
{
  "id": "...",
  "email": "admin@example.com",
  "name": "Admin User",
  "org_role": "admin"
}
```

### 4. Login with the same credentials

```bash
curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq .token
```

Returns a fresh JWT.

### 5. Test RBAC enforcement

Register a second user (gets Viewer role automatically):

```bash
curl -s -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"viewer@example.com","name":"Viewer User","password":"changeme123"}' | jq .

export VIEWER_TOKEN="<paste viewer token>"
```

Viewer can read SEVs but cannot create them:

```bash
# ListSEVs → allowed for Viewer
curl -s -H "Authorization: Bearer $VIEWER_TOKEN" \
  "http://localhost:8080/v1/sevs" | jq '.total'

# CreateSEV → denied for Viewer (403 Forbidden)
curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $VIEWER_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST http://localhost:8080/v1/sevs \
  -d '{"title":"Test","severity_level":3,"created_by":"me"}'
# Expected: 403
```

Admin can create SEVs:

```bash
curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST http://localhost:8080/v1/sevs \
  -d '{"title":"Production outage","severity_level":1,"created_by":"admin@example.com"}'
# Expected: 200
```

### 6. Cookie-based access (browser flow)

After login the `token` cookie is set automatically. Subsequent browser requests
include the cookie; the server extracts it for authentication:

```bash
curl -s --cookie "token=$ADMIN_TOKEN" http://localhost:8080/v1/auth/me | jq .name
```

## Verify tests pass

```bash
go test ./internal/auth/... -v
go test ./internal/api/grpc/... -v
go test ./...
```

## Known limitations

- User role changes require a direct database update; there is no Admin API for
  user management until M10.
- After `JWT_TTL_HOURS` the user must re-authenticate (login endpoint).
- Sensitive SEV field-level visibility restrictions are deferred to M10.
