package grpc

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// Pinger reports whether the backing store is reachable. Declared here (the
// consumer), per this package's declare-interfaces-at-the-consumer
// convention (see CLAUDE.md's Design principles) — cmd/server's Stores type
// satisfies this implicitly (its Ping method delegates to *pgxpool.Pool.Ping,
// itself already shaped this way, or is a no-op against the in-memory dev
// fallback) with no import of this package required in return.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewHealthzHandler returns a handler for GET /healthz: an unauthenticated
// liveness/readiness probe that checks only DB reachability via pinger.
// Explicitly distinct from GET /admin/integrations/health
// (IntegrationsHealthHandler above), which is authenticated, admin-only, and
// checks *third-party* integration connectivity (PagerDuty/GitHub/Slack) —
// not process/store liveness. /healthz is what a container orchestrator's
// liveness/readiness probe should point at.
//
// Like GET /metrics (see cmd/server/main.go), this is deliberately
// unauthenticated (an orchestrator probe has no credentials to present) and
// not wrapped in loggingMiddleware (a probe polls every few seconds; an
// access-log line per poll would be pure noise) — a failure is still logged,
// at Error, since that's the one outcome worth knowing about.
func NewHealthzHandler(pinger Pinger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := pinger.Ping(r.Context()); err != nil {
			// err's detail (e.g. a Postgres driver error) stays server-side —
			// mirrors internalError's (errors.go) reasoning that error detail
			// is for the log, not an unauthenticated caller.
			telemetry.LoggerFromContext(r.Context()).ErrorContext(r.Context(), "healthz: store unreachable", "err", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}
