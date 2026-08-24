# M12 Demo — AI Plugin System

## What was built

The pluggable AI provider system described in `docs/requirements.md` §11:

- **`internal/ai`** — a `Provider` interface (`Summarize`, `SuggestRootCause`,
  `DraftPostmortem`, `SuggestTasks`, `FindSimilar`, `SuggestResponders`,
  `DraftAnnouncement`, `StreamAction`) with two built-in implementations:
  `AnthropicProvider` (a real HTTP client for Anthropic's Messages API) and
  `HTTPProvider` (calls a generic externally hosted endpoint — what this demo uses,
  since it needs no API key).
- **`Dispatcher`** — routes both proactive lifecycle triggers and on-demand requests
  to whichever plugin is enabled, enforces a per-plugin requests-per-minute rate
  limit, stores every result in `ai_outputs`, and broadcasts an `ai.output` WebSocket
  event.
- **Proactive triggers** (§11.1), wired into `SEVService`/`PostmortemService`'s
  existing mutation handlers — no polling, no separate event bus:
  - SEV opened, **SEV-1 or SEV-2 only** → `SuggestResponders`
  - SEV transitions to Mitigated → `Summarize` + `SuggestRootCause`
  - SEV transitions to Resolved → `DraftPostmortem`
  - Postmortem transitions to In Review → `SuggestTasks`
  - Sensitive SEVs are always excluded; a SEV's own `ai_disabled` flag (§11.3)
    excludes it too.
- **On-demand actions** (§11.2) — `AIService.TriggerAction` (synchronous) and
  `AIService.StreamAction` (server-streaming; re-emits the completed result in a
  few word-chunked pieces rather than true token-level streaming — see Known
  limitations) let a caller run any action against a SEV whenever they want.
  `AIService.ListOutputs` reads back everything stored for a SEV;
  `AIService.ListPlugins` lists enabled plugins without exposing credentials.
- **Admin registration** (§18.6) — `ConfigService.CreateAIPlugin` /
  `GetAIPlugin` / `UpdateAIPlugin` / `DeleteAIPlugin` / `ListAIPlugins`. A
  plugin's `api_key` is sealed with the same AES-256-GCM encryption M10
  introduced for integration credentials — never returned by any RPC, only
  whether one is configured.

Every `ConfigService` AI-plugin RPC is Admin-only; `AIService.TriggerAction`/
`StreamAction` need Responder+; `ListOutputs`/`ListPlugins` are open to any
authenticated user.

---

## Prerequisites

- M05 (postmortem) and M10 (Config API, `ENCRYPTION_KEY`) complete
- `ENCRYPTION_KEY` set — required to register a plugin with an `api_key`; this demo's
  HTTP plugin doesn't need one, so it works without it too
- `curl` and `jq` installed (the mock AI endpoint below is plain Go, so no extra
  runtime is needed beyond the Go toolchain this repo already requires)

---

## Start the stack

```bash
cp .env.example .env
# Fill in JWT_SECRET and ENCRYPTION_KEY
make up
```

Or for local development without Docker:

```bash
JWT_SECRET=changeme ENCRYPTION_KEY=$(openssl rand -base64 32) go run ./cmd/server
```

---

## Walkthrough

All commands below assume the server is running on `localhost:8080`.

### 0. Log in as Admin

```bash
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123","name":"Admin"}' | jq .

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"changeme123"}' | jq -r .token)
```

### 1. Start a mock AI endpoint

`HTTPProvider` POSTs `{"action": "...", "sev": {...}}` to a configured endpoint and
expects back whichever field matches the action (see `internal/ai/provider_http.go`).
This small script answers every action the same way, which is enough to exercise the
whole pipeline without a real AI vendor. It listens on `0.0.0.0:8899` rather than
`127.0.0.1:8899` — this **must** run on the host (not inside a container), and if
the API server itself is running via `make up`/Docker Compose, the `api` container
needs to reach *this host* process, not something inside its own network namespace:

```bash
cat > /tmp/mock-ai.go <<'EOF'
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	responses := map[string]any{
		"summarize":          map[string]string{"text": "The checkout service returned elevated 500s after a bad deploy."},
		"draft_announcement": map[string]string{"text": "We are investigating elevated errors on checkout."},
		"suggest_root_cause": map[string]any{"root_causes": []map[string]string{
			{"category": "deployment", "rationale": "coincides with a recent release"},
		}},
		"draft_postmortem": map[string]any{"postmortem": map[string]string{
			"summary": "Checkout degraded for 20 minutes.", "root_cause": "TBD", "action_items": "Add a canary stage.",
		}},
		"suggest_tasks": map[string]any{"tasks": []map[string]string{
			{"title": "Add canary deploy stage", "priority": "critical", "relationship_type": "action-item"},
		}},
		"find_similar": map[string]any{"similar": []any{}},
		"suggest_responders": map[string]any{"responders": []map[string]string{
			{"role": "Incident Commander", "rationale": "SEV-1, needs coordination"},
		}},
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, ok := responses[req.Action]
		if !ok {
			resp = map[string]string{"text": "ok"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	log.Fatal(http.ListenAndServe("0.0.0.0:8899", nil))
}
EOF

go run /tmp/mock-ai.go &
MOCK_PID=$!
```

(Run `kill $MOCK_PID` when you're done with the demo.)

Pick the hostname the **API server** will use to reach it, based on how you started
the stack in the previous section:

```bash
# make up / docker compose: the api container reaches the host via Docker
# Desktop's special DNS name (Mac/Windows only — see Known limitations for Linux).
AI_ENDPOINT=http://host.docker.internal:8899

# go run ./cmd/server (no Docker): the server is just another process on
# localhost, so plain loopback works.
# AI_ENDPOINT=http://127.0.0.1:8899
```

Using `127.0.0.1` while the server runs in Docker is exactly what produces
`"connect: connection refused"` in the `api` container's logs — `127.0.0.1` inside
that container refers to the container itself, which has nothing listening on 8899.

### 2. Register the plugin

```bash
PLUGIN=$(curl -s -X POST http://localhost:8080/v1/config/ai-plugins \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "name": "demo-mock",
    "version": "1.0.0",
    "handler_type": "http",
    "http_endpoint": "'"$AI_ENDPOINT"'",
    "enabled": true,
    "trigger_on_open": true,
    "trigger_on_mitigated": true,
    "trigger_on_resolved": true,
    "trigger_on_postmortem_review": true,
    "rate_limit_per_minute": 30
  }')
echo "$PLUGIN" | jq .
PLUGIN_ID=$(echo "$PLUGIN" | jq -r .id)

# Any authenticated user can see it's available, with no internals exposed:
curl -s http://localhost:8080/v1/ai/plugins -H "Authorization: Bearer $TOKEN" | jq .
```

### 3. Proactive trigger: open a SEV-1

Opening a SEV-1 (or SEV-2) automatically fires `SuggestResponders` in the background —
no explicit AI call needed:

```bash
SEV_ID=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout returning 500s","severity_level":1,"affected_services":["checkout"]}' \
  | jq -r .id)

sleep 1 # dispatch is async — give the worker pool a moment

curl -s "http://localhost:8080/v1/sevs/$SEV_ID/ai/outputs" \
  -H "Authorization: Bearer $TOKEN" | jq .
# → one output, action=AI_ACTION_SUGGEST_RESPONDERS, trigger_event=sev.opened
```

### 4. Proactive trigger: mitigate and resolve

```bash
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/transition" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"to_status":"mitigated"}' | jq '{status}'

curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/transition" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"to_status":"resolved"}' | jq '{status}'

sleep 1

curl -s "http://localhost:8080/v1/sevs/$SEV_ID/ai/outputs" \
  -H "Authorization: Bearer $TOKEN" | jq '[.outputs[] | {action, trigger_event}]'
# → suggest_responders (sev.opened), summarize + suggest_root_cause (sev.mitigated),
#   draft_postmortem (sev.resolved)
```

### 5. On-demand action

```bash
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/ai/actions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"action":"AI_ACTION_DRAFT_ANNOUNCEMENT"}' | jq .
# → trigger_event: "manual"
```

### 6. Per-SEV AI disable (§11.3)

```bash
SEV2_ID=$(curl -s -X POST http://localhost:8080/v1/sevs \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"noisy alert","severity_level":1,"ai_disabled":true}' | jq -r .id)

sleep 1
curl -s "http://localhost:8080/v1/sevs/$SEV2_ID/ai/outputs" \
  -H "Authorization: Bearer $TOKEN" | jq '.outputs | length'
# → 0, even though it's a SEV-1

curl -s -X POST "http://localhost:8080/v1/sevs/$SEV2_ID/ai/actions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"action":"AI_ACTION_SUMMARIZE"}'
# → 400 FAILED_PRECONDITION: AI is disabled for this SEV
```

### 7. Rate limiting

The limiter counts every call against a plugin within its current 60s window,
proactive and manual alike — steps 3–4 above already made 4 calls against this
plugin, so dropping its limit to 1 rejects the very next call, not just a second one:

```bash
curl -s -X PATCH "http://localhost:8080/v1/config/ai-plugins/$PLUGIN_ID" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"rate_limit_per_minute":1}' | jq '{rate_limit_per_minute}'

curl -s -w '\nhttp_status=%{http_code}\n' -X POST "http://localhost:8080/v1/sevs/$SEV_ID/ai/actions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"action\":\"AI_ACTION_SUMMARIZE\",\"plugin_id\":$PLUGIN_ID}"
# → 429 RESOURCE_EXHAUSTED: AI plugin rate limit exceeded, try again shortly

# Wait out the window and it succeeds again:
sleep 60
curl -s -X POST "http://localhost:8080/v1/sevs/$SEV_ID/ai/actions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"action\":\"AI_ACTION_SUMMARIZE\",\"plugin_id\":$PLUGIN_ID}" | jq '{action}'
```

---

## Verify tests pass

```bash
go test ./internal/ai/... ./internal/api/grpc/... ./internal/store/... -v
go test -race ./...
golangci-lint run
```

Key coverage:

- `internal/ai`: rate limiter (burst allowed, limit enforced, resets after the
  window, independent per plugin); dispatcher (manual trigger stores output and
  publishes, `ai_disabled` SEV rejected before any provider call, no-enabled-plugin
  rejected, rate limit enforced, SEV-opened only dispatches for SEV-1/SEV-2, a
  plugin's `trigger_on_*` flag is honored, `StreamOne`'s final chunk is stored);
  `AnthropicProvider` and `HTTPProvider` against `httptest` mocks (no real network
  calls), including the markdown-fence-stripping fallback for structured JSON
  responses.
- `internal/api/grpc`: `AIServer` (action/error-code mapping, streaming, output
  listing, available-plugins filtering); `ConfigServer` AI plugin CRUD (API key
  encryption round trip, partial update, duplicate name rejected); `SEVServer`/
  `PostmortemServer` dispatch proactive triggers on exactly the right transitions and
  skip sensitive/`ai_disabled` SEVs.

---

## Known limitations

- `host.docker.internal` (step 1's `AI_ENDPOINT` for the `make up` case) is resolved
  automatically inside containers by Docker **Desktop** (Mac/Windows) only. On Linux
  Docker Engine it isn't available unless `deploy/docker-compose.yml`'s `api` service
  adds `extra_hosts: ["host.docker.internal:host-gateway"]` (Docker Engine 20.10+),
  which it doesn't today — on plain Linux Docker, run the server via
  `go run ./cmd/server` for this demo instead, or run the mock endpoint as its own
  container on the compose network and point `http_endpoint` at its service name.
- ~~`AIPluginStore`/`AIOutputStore` are in-memory only even when `DATABASE_URL` is
  set...~~ **`AIPluginStore` fixed during M14d** (`internal/store/postgres/
  aiplugin.go` — see `demo/M14d-admin-pages.md`'s "Bug fix" section).
  `AIOutputStore` is still in-memory only — it wasn't part of that fix (AI outputs
  aren't managed from an admin page; they're the actual generated
  summaries/drafts/etc. shown inline on a SEV, M12's `ai_outputs` table has existed
  unused the same way since M01).
- `StreamAction` doesn't do real token-level streaming from Anthropic's API — both
  built-in providers run the action to completion and then re-emit the result as a
  handful of word-chunked pieces. Wiring true SSE streaming end-to-end (provider →
  `Dispatcher` → gRPC server-stream) is deferred past v1.
- `TriggerActionRequest.plugin_id = 0` picks "the first enabled plugin found" — v1
  assumes at most one active plugin at a time; there's no per-action plugin routing
  or fallback/failover between multiple enabled plugins.
- The rate limiter is a fixed-window counter, not a token bucket or sliding window —
  a plugin can briefly exceed its configured limit right at a window boundary.
- `SuggestRootCause`/`SuggestTasks`/`FindSimilar`/`SuggestResponders` results are
  stored as a JSON string in `ai_outputs.content`; there's no dedicated structured
  column, so any consumer (e.g. the eventual web UI) must know to parse it per-action.
- `Dispatcher`'s `FindSimilar`/`SuggestRootCause` candidate pool is "other SEVs
  sharing an affected service," capped at 5 — no semantic search yet (`docs/requirements.md`
  §11.2's "Find similar SEVs" describes this as a future direction).
