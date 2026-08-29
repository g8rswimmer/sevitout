/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'node:path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    // Dev-time proxy so the browser only ever talks to the Vite origin —
    // avoids CORS entirely rather than adding CORS headers to the Go API
    // server (which intentionally has none; see internal/api/ws/handler.go).
    // The same same-origin trick is done in production by nginx (see
    // web/nginx.conf) proxying these same path prefixes to the api service.
    proxy: {
      '/v1': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
      // Trailing slash matters: the public share view is GET /s/{token}, and
      // a bare '/s' prefix would also swallow the SPA's own /sevs/:id route
      // (Vite's proxy match is a plain string-prefix test, not path-segment
      // aware) — every request under /sevs would silently 404 against the
      // API instead of falling through to index.html for React Router.
      //
      // Content-negotiated like nginx.conf's matching /s/ block (see its
      // comment for the full rationale): a browser's initial navigation to
      // a share link sends Accept: text/html, so `bypass` hands it
      // index.html instead of proxying — PublicSharePage then mounts and
      // fetches this same URL itself (default Accept: */*), which isn't
      // bypassed and reaches the real backend.
      '/s/': {
        target: 'http://localhost:8080',
        bypass(req) {
          if (req.headers.accept?.includes('text/html')) return '/index.html'
        },
      },
      '/ws': { target: 'ws://localhost:8080', ws: true },
      '/openapi.json': 'http://localhost:8080',
    },
  },
  test: {
    // Node 22+'s own experimental global `localStorage` (see `node --help`'s
    // --localstorage-file) occupies the same globalThis slot the jsdom
    // environment wants to install its Storage implementation into, leaving
    // `localStorage`/`window.localStorage` undefined in tests. The `test`
    // and `test:watch` npm scripts run with
    // NODE_OPTIONS=--no-experimental-webstorage to disable Node's version
    // so jsdom's takes over — `npx vitest` directly, without that env var,
    // will fail any test touching localStorage.
    environment: 'jsdom',
    environmentOptions: {
      jsdom: { url: 'http://localhost/' },
    },
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: true,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'lcov'],
      reportsDirectory: './coverage',
      // roadmap Phase 5: gate set a few points below the actual aggregate
      // measured when this was added (statements 77.86%, branches 72.58%,
      // functions 67.63%, lines 79.82% — see demo/test-coverage-ci-gate.md)
      // rather than at it, so normal fluctuation doesn't fail `main` on a
      // borderline day. `npm test -- --coverage` (frontend-ci.yml) already
      // fails non-zero when any of these aren't met — global, not
      // per-file, so one weak file doesn't block on its own. Ratchet
      // upward in a follow-up once there's real headroom above it.
      thresholds: {
        statements: 75,
        branches: 70,
        functions: 65,
        lines: 77,
      },
    },
  },
})
