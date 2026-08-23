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
- `curl`, `jq`, and `python3` (to run a one-line mock AI endpoint) installed

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
This one-liner answers every action the same way, which is enough to exercise the
whole pipeline without a real AI vendor:

```bash
python3 -c '
import json, http.server

class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers["Content-Length"])
        req = json.loads(self.rfile.read(length))
        action = req["action"]
        resp = {
            "summarize": {"text": "The checkout service returned elevated 500s after a bad deploy."},
            "draft_announcement": {"text": "We are investigating elevated errors on checkout."},
            "suggest_root_cause": {"root_causes": [{"category": "deployment", "rationale": "coincides with a recent release"}]},
            "draft_postmortem": {"postmortem": {"summary": "Checkout degraded for 20 minutes.", "root_cause": "TBD", "action_items": "Add a canary stage."}},
            "suggest_tasks": {"tasks": [{"title": "Add canary deploy stage", "priority": "critical", "relationship_type": "action-item"}]},
            "find_similar": {"similar": []},
            "suggest_responders": {"responders": [{"role": "Incident Commander", "rationale": "SEV-1, needs coordination"}]},
        }.get(action, {"text": "ok"})
        body = json.dumps(resp).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *args): pass

http.server.HTTPServer(("127.0.0.1", 8899), H).serve_forever()
' &
MOCK_PID=$!
```

(Run `kill $MOCK_PID` when you're done with the demo.)

### 2. Register the plugin

```bash
PLUGIN=$(curl -s -X POST http://localhost:8080/v1/config/ai-plugins \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{
    "name": "demo-mock",
    "version": "1.0.0",
    "handler_type": "http",
    "http_endpoint": "http://127.0.0.1:8899",
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

- `AIPluginStore`/`AIOutputStore` are in-memory only even when `DATABASE_URL` is set —
  same "postgres implementation deferred" treatment M10 gave `IntegrationConfigStore`
  and `RetentionConfigStore` (the `ai_plugins`/`ai_outputs` tables themselves have
  existed since M01's migration).
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
