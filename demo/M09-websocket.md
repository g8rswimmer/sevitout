# M09 — WebSocket / Real-time

## What was built

An authenticated WebSocket endpoint (`GET /ws`) backed by a room-per-SEV publish/subscribe
hub (`internal/api/ws`). Clients subscribe to one or more SEV IDs — either via `?sev_id=`
query parameters on connect, or by sending `{"action":"subscribe","sev_id":"..."}` /
`{"action":"unsubscribe","sev_id":"..."}` control frames afterwards — and receive a typed
JSON envelope for every subsequent mutation on those SEVs:

```json
{"type":"sev.updated","sev_id":"SEV-2026-0001","payload":{...}}
```

`payload` is the same JSON shape the REST API returns for that resource (marshaled with
the same field naming as the gRPC gateway), so a client can reuse its existing response
parsing.

**Event types** (matching `docs/architecture.md` §3.2):

| Event | Published by |
|---|---|
| `sev.updated` | `UpdateSEV` |
| `sev.status_changed` | `TransitionStatus` |
| `announcement.created` | `CreateAnnouncement` |
| `chat.created` | `AddChatEntry` |
| `role.changed` | `AssignRole`, `RemoveRole` |
| `task.linked` | `LinkTask`, `CreateGitHubIssue` |
| `task.updated` | `UpdateTaskDueDate`, `UnlinkTask` |
| `postmortem.updated` | `UpdatePostmortem`, `TransitionPostmortemStatus` |

Each gRPC handler publishes to the hub only after its store write (and audit entry, where
applicable) succeeds — a failed mutation never produces a phantom event. Publishing is
fire-and-forget: a stalled or slow WebSocket client never blocks the mutation that
triggered the event, and a handler with no `Publisher` wired up (e.g. in unit tests) is a
silent no-op.

## Prerequisites

- M03 (auth) complete — the WebSocket handshake requires the same JWT as any other API call.
- `make up` started (or in-memory server via `go run ./cmd/server`).
- A WebSocket client. Examples below use [`websocat`](https://github.com/vi/websocat)
  (`brew install websocat`); `wscat` or a browser's `new WebSocket(...)` console work too.

## Start the stack

```bash
cp .env.example .env          # fill in JWT_SECRET
make up
```

Or run the server locally without Docker:

```bash
JWT_SECRET=changeme go run ./cmd/server
```

## Walkthrough

### 0. Log in

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)
```

(See `demo/M07-linked-tasks.md` §0 if you need to bootstrap `admin@example.com` first.)

### 1. Create a SEV to subscribe to

```bash
SEV=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout latency spike","description":"P99 latency exceeded 5s","severity_level":1}' \
  | jq -r .id)
echo "SEV=$SEV"
```

### 2. Connect and subscribe

Open a WebSocket connection subscribed to `$SEV` from the start (`?sev_id=` may repeat for
multiple SEVs):

```bash
websocat "ws://localhost:8080/ws?sev_id=$SEV" -H "Authorization: Bearer $TOKEN"
```

Leave this connection open in its own terminal for the remaining steps.

### 3. Trigger a mutation from another terminal

```bash
curl -s -X PATCH "http://localhost:8080/v1/sevs/$SEV" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout latency spike (mitigated)"}' > /dev/null
```

The `websocat` terminal from step 2 immediately prints:

```json
{"type":"sev.updated","sev_id":"SEV-2026-0001","payload":{"id":"SEV-2026-0001","title":"Checkout latency spike (mitigated)",...}}
```

### 4. Try an event from a different service — announcement

```bash
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV/announcements" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"message":"Mitigation deployed.","audience":"internal"}' > /dev/null
```

→ `{"type":"announcement.created","sev_id":"SEV-2026-0001","payload":{...}}`

### 5. Confirm room isolation

Open a second `websocat` connection subscribed to a **different** SEV ID (create one with
step 1 again, or reuse any other existing SEV) and repeat step 3 against the original
`$SEV`. The second connection receives nothing — events are scoped to the SEVs a
connection is actually subscribed to.

### 6. Dynamic subscribe/unsubscribe

With a connection open (from step 2, no query-string subscriptions needed), send a control
frame to add or drop a room without reconnecting:

```json
{"action":"subscribe","sev_id":"SEV-2026-0002"}
{"action":"unsubscribe","sev_id":"SEV-2026-0002"}
```

`websocat`'s interactive mode lets you type these directly into the same terminal as the
connection from step 2.

## Verify tests pass

```bash
go test ./internal/api/ws/... ./internal/api/grpc/... -race
golangci-lint run
```

## Known limitations

- No presence/typing indicators — the hub only fans out the eight mutation events listed
  above, not connection-level metadata.
- No message backlog: a client that connects (or reconnects) after a mutation has already
  been published will not receive it retroactively. Clients needing the current state on
  connect should call the relevant REST endpoint first, then subscribe for subsequent
  changes.
- A client whose receive buffer fills (16 undelivered events) has the oldest-blocking event
  silently dropped rather than the connection being disconnected or throttled — acceptable
  at single-org scale, but a client that falls far behind should re-fetch state via REST
  rather than trust the WebSocket stream alone.
- Sensitive SEVs are not filtered from WebSocket events: subscribing to a sensitive SEV's ID
  still delivers its events. This mirrors the same pre-existing, repo-wide gap noted in
  `demo/M08-search.md` (`GetSEV`/`ListSEVs` don't restrict sensitive-SEV visibility either) —
  there is no per-user visibility/ACL mechanism yet (requirements §14).
- `SEVLinkService` (`LinkSEVs`/`UnlinkSEVs`) has no corresponding event type in
  `docs/architecture.md`'s event table and does not publish anything.
- AI plugin output (`ai.output`) is deferred to M12, which introduces the AI dispatcher that
  will produce it.
