# Sevitout — Frontend

React + TypeScript + Vite, consuming the API server in `cmd/server`. See
`demo/M14a-shell-auth-dashboard.md` (and later `demo/M14*.md` files as they land) for
a full walkthrough, and `docs/architecture.md` §9 for the intended route map.

## Development

```bash
npm install
npm run dev      # http://localhost:5173, proxies /v1, /auth, /s/, /ws, /admin, /openapi.json to :8080
```

Run the API server separately (see the repo root README/Makefile) — this project
makes no cross-origin requests itself; `vite.config.ts`'s dev proxy and
`nginx.conf` (production, via the `web` Docker Compose service) both forward API
paths to the same origin the page was loaded from. See `src/lib/api.ts`'s header
comment for why.

## Scripts

| Script | Does |
|---|---|
| `npm run dev` | Vite dev server with HMR |
| `npm run build` | `tsc -b` type-check, then `vite build` to `dist/` |
| `npm run preview` | Serve the production build locally |
| `npm run lint` | `oxlint` |
| `npm test` | `vitest run` (Vitest + React Testing Library) |
| `npm run test:watch` | `vitest` in watch mode |

`npm test`/`test:watch` set `NODE_OPTIONS=--no-experimental-webstorage` — see the
comment above `test.environmentOptions` in `vite.config.ts` for why that's needed on
Node 22+.

## Structure

```
src/
  auth/        # AuthContext, useAuth, ProtectedRoute
  components/
    ui/        # shadcn/ui-style primitives (hand-written, not a dependency)
    layout/    # AppLayout (nav, user menu)
  lib/         # api.ts (typed fetch client), format.ts, utils.ts (cn), queryClient.ts
  pages/       # one file per route
  types/       # wire types mirroring proto/sevitout/v1/*.proto
  test/        # test setup + shared render helpers
```
