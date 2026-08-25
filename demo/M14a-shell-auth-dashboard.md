# M14a Demo — Shell, Auth & Dashboard

## What was built

The first slice of the React frontend (`docs/project-plan.md`'s M14a): the app shell,
authentication, and the dashboard page, in a new `web/` Vite project.

- **Stack**: Vite + React 18 + TypeScript + Tailwind CSS v4 + hand-rolled shadcn/ui-
  style primitives (`web/src/components/ui/` — `Button`, `Card`, `Input`, `Label`,
  `Badge`, `Skeleton`; shadcn/ui isn't a component *dependency*, it's a copy-into-your-
  repo generator, so these are written by hand in that same style rather than pulled
  in as a package) + React Router v6 + TanStack Query.
- **Login and Register pages** (`/login`, `/register`) — email + password forms
  against `POST /auth/login` / `POST /auth/register`. **This deviates from
  `docs/project-plan.md`'s M14a line item, which describes a "Google/GitHub OAuth
  redirect" login page**: `docs/architecture.md` §12's resolved decisions record that
  OAuth was deliberately dropped in favor of internal email+password
  (`internal/auth/password.go`) before M03 was ever built, and no OAuth provider
  exists anywhere in the Go backend to redirect to. The frontend follows the backend
  that actually exists. A Register page isn't explicitly listed in M14a either, but
  without one there'd be no way to create the first (Admin-bootstrapping) user through
  the UI at all — see `docs/requirements.md` §14.
- **Auth state** (`web/src/auth/`): `AuthProvider` calls `GET /v1/auth/me` (`WhoAmI`)
  once on load to hydrate/validate the session, and again right after login/register.
  **This also deviates from `docs/architecture.md` §9's "JWT ... set as an `httpOnly`
  cookie"**: `internal/auth/password.go`'s `/auth/login` only ever returns the token in
  the JSON body — no cookie is set anywhere in the Go backend (an `httpOnly` cookie
  can only be set server-side; JS can't create one after the fact). The token is held
  in `localStorage` instead and sent as `Authorization: Bearer <token>` on every
  request, the same header every other client of this API already uses (the Slack
  bot, every curl example in `demo/M0*.md`) and the same one the gRPC-gateway's
  `WithMetadata` option and `internal/auth/interceptor.go` expect. A 401 response from
  any request drops the session and redirects to `/login` (`ProtectedRoute`).
- **Dashboard page** (`/`) — active SEV count by severity (from
  `ReportService.GetDashboardMetrics`), an MTTR trend bar chart (7/30/90-day windows),
  overdue task count, postmortem completion rate, and a live list of active SEVs
  (`SEVService.ListSEVs` filtered to every non-`postmortem_complete` status), each
  linking to `/sevs/:id`.
- **Shared layout** (`web/src/components/layout/AppLayout.tsx`) — top nav with the
  Sevitout brand, nav links, and a user menu (name, role, log out). `/sevs/:id` is
  registered now as a placeholder page (`SevDetailPage`) purely so the dashboard's
  links resolve to something real — the actual detail view is M14b.
- **No CORS, anywhere.** The Go API server has none (`internal/api/ws/handler.go`
  notes this is intentional), so `web/src/lib/api.ts` makes only same-origin,
  relative-path requests (`/v1/...`, `/auth/...`). `web/vite.config.ts`'s dev-time
  `server.proxy` forwards those paths to `localhost:8080` locally; `web/nginx.conf`
  does the equivalent in the new `web` Docker Compose service. Setting
  `VITE_API_BASE_URL` to point at a different origin is supported by
  `web/src/lib/api.ts` but would need CORS support added server-side to actually work.
- **Docker**: `web/Dockerfile` (multi-stage `npm run build` → `nginx:alpine` serving
  `dist/`) and a `web` service added to `deploy/docker-compose.yml`, port `3000:80`.

---

## Prerequisites

- M02 (SEV API), M03 (auth), M13 (`ReportService`) complete — this demo creates a SEV
  and reads it back through `GetDashboardMetrics`
- Node.js 22+ and `npm` (for running the frontend outside Docker)
- `JWT_SECRET` set in `.env` — or `ALLOW_INSECURE_JWT_SECRET=true` to explicitly opt
  into the insecure dev default; the server now refuses to start with neither set

---

## Start the stack

### Option A — Docker Compose (closest to production)

```bash
cp .env.example .env   # if you don't already have one
make up                # postgres, migrate, api, slackbot (exits cleanly, no Slack tokens), web
```

Open **http://localhost:3000**.

### Option B — local dev servers (faster iteration on the frontend)

```bash
# Terminal 1 — API server, in-memory store (no DATABASE_URL needed)
JWT_SECRET=dev-secret-please-change JWT_TTL_HOURS=24 go run ./cmd/server

# Terminal 2 — frontend dev server, proxies /v1, /auth, /s/, /ws, /openapi.json to :8080
cd web
npm install
npm run dev
```

Open **http://localhost:5173**.

---

## Walkthrough

1. **Register the first user.** Open the app — you're redirected to `/login`. Click
   **Register**, fill in name/email/an 8+ character password, submit. The response
   from `docs/requirements.md` §14 ("first user to register is Admin") applies: this
   account is Admin. You land on `/` immediately (`AuthProvider` sets the session as
   soon as register/login and the follow-up `WhoAmI` both resolve).

   Equivalent, to see the exact wire contract the form uses:

   ```bash
   curl -s -X POST http://localhost:3000/auth/register \
     -H 'Content-Type: application/json' \
     -d '{"email":"admin@example.com","name":"Admin User","password":"password123"}' | jq
   ```

   Expected: `{"token": "...", "user": {"id": "...", "email": "admin@example.com", "name": "Admin User", "org_role": "admin"}}`.

2. **Reload the page.** You stay logged in — `AuthProvider` re-validates the token
   stored in `localStorage` against `WhoAmI` on every load rather than trusting it
   blindly. Open devtools → Application → Local Storage and confirm a
   `sevitout.token` key holds the JWT.

3. **Log out, then log back in** via the user menu (top right) and the `/login` form,
   confirming both directions of the auth flow work, not just register.

4. **Seed a SEV** so the dashboard has something to show (there's no "create SEV" UI
   yet — that's M14b):

   ```bash
   TOKEN=<paste the token from step 1, or grab a fresh one from localStorage>
   curl -s -X POST http://localhost:3000/v1/sevs \
     -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"title":"Database outage","description":"primary db down","severity_level":1,"affected_services":["checkout"]}' | jq
   ```

5. **Reload the dashboard.** Confirm:
   - The active-SEV count card shows the SEV-1 badge incremented and a total matching
     it.
   - The MTTR trend chart renders three bars (7/30/90-day), all near-zero until SEVs
     are actually resolved.
   - "Database outage" appears in the Active SEVs list with an `SEV-1` badge and
     status `Open`, and its link points at `/sevs/SEV-2026-00xx`.
6. **Click the SEV in the list.** It navigates to `/sevs/:id` and renders the M14b
   placeholder — confirming routing (and the fact that this ID round-trips correctly
   from the dashboard query into the URL) without yet needing the real detail page.
7. **Try an unknown route** (e.g. `/nonsense`) — redirects to a "Page not found"
   screen with a link back to the dashboard.

---

## Verify tests pass

```bash
cd web
npm run build   # tsc -b && vite build
npx oxlint
npm test        # NODE_OPTIONS=--no-experimental-webstorage vitest run
```

The Go side is untouched by this sub-milestone; `go build ./...`, `go test ./...`,
and `golangci-lint run` all still pass as they did at the end of M13.

Key coverage (Vitest + React Testing Library, 26 tests):

- `lib/api.ts` — Bearer header attached only when a token is stored, protojson-style
  (`{message}`) and plaintext `http.Error` (`{error}`) error bodies both parse into
  `ApiError`, non-JSON error bodies fall back to `statusText`, a 401 invokes the
  unauthorized handler in addition to throwing, array filters serialize as repeated
  query params, empty/undefined filters are omitted.
- `auth/AuthContext.tsx` — cold start with no token stays logged out and makes no
  request; a stored token hydrates via `WhoAmI`; a token `WhoAmI` rejects gets
  cleared; `login`/`logout` round-trip the token and user state.
- `auth/ProtectedRoute.tsx` — redirects to `/login` unauthenticated; renders children
  once authenticated; blocks (without rendering) a route gated on a role higher than
  the user holds.
- `pages/LoginPage.tsx` — submits the exact `{email, password}` body to `/auth/login`;
  renders the server's error message on a failed login.
- `pages/DashboardPage.tsx` — renders metrics and the active-SEV list from mocked
  responses; empty-active-SEVs state; a dashboard-metrics load failure is surfaced
  without blocking the rest of the page.
- `lib/format.ts` — duration formatting at second/minute/hour/day granularity and its
  em-dash fallback for missing/zero/negative input; date formatting and its fallback
  for missing/invalid input.

A full Docker Compose run (`postgres`, `migrate`, `api`, `web`) was smoke-tested for
this demo: register, `WhoAmI`, and `GetDashboardMetrics` all round-tripped correctly
through the `web` container's nginx proxy at `http://localhost:3000`, and
`/sevs/:id` correctly falls through to `index.html` (not proxied to the API — see the
next section for why that almost wasn't true).

---

## Known limitations

- **protojson omits zero-valued fields entirely** rather than emitting an explicit
  zero/empty value (confirmed against the live server, not just assumed from the
  spec) — a freshly-created SEV's response has no `tags`, `mttr_seconds`,
  `overdue_task_count`, etc. at all when they're empty/zero. Every such field in
  `web/src/types/api.ts` is typed optional for this reason, and reads go through
  `?? fallback` — this matters more once M14b starts rendering many more SEV fields
  directly.
- The `/s` vs `/s/` proxy-prefix collision (a bare `/s` prefix in both
  `vite.config.ts` and `nginx.conf` would silently swallow every `/sevs/...` request
  and route it to the API instead of the SPA) is called out in code comments in both
  files — worth knowing if a future milestone adds another top-level route starting
  with `s`.
- No "create SEV" UI yet — SEVs are seeded via curl in this demo. Lands in M14b.
- No WebSocket client yet — the dashboard is query-cache-only; it does not live-update
  when a SEV changes elsewhere. Lands in M14b.
- `right_people_present`/`sensitive`/`ai_disabled` and most other SEV fields aren't
  rendered anywhere yet (the dashboard only reads `id`, `title`, `severity_level`,
  `status`, `started_at`) — full field coverage is M14b's SEV detail page.
- The Register page has no invite/approval flow — it's open self-registration exactly
  as `docs/requirements.md` §14 specifies for v1 (first user Admin, everyone after
  Viewer); there's no UI yet for an Admin to promote a Viewer (that's M14d, User
  Management, which already has a backend via `ConfigService`).
