package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics live in this package alongside the context helpers (WithRequestID,
// WithLogger) because both are cross-cutting per-request/operational
// infrastructure, not domain logic — internal/sev.ComputeMetrics computes
// business metrics (MTTD/MTTM/MTTR), a deliberately different and unrelated
// meaning of "metrics" from the ones here.
//
// Every metric below is registered against prometheus's default registry via
// promauto at package init, and served at GET /metrics
// (cmd/server/main.go, promhttp.Handler() against prometheus.DefaultGatherer)
// — deliberately unauthenticated, matching standard Prometheus scrape
// convention and the existing unauthenticated GET /openapi.json.

var (
	// RPCRequestsTotal counts every gRPC/REST call, labeled by method and
	// resulting status code. Incremented once per call in
	// internal/api/grpc.logRPC, which already resolves both labels exactly
	// once per call for its own log line — every REST call reaches this too,
	// since it's proxied through grpc-gateway to a loopback gRPC call that
	// passes through the same interceptor.
	RPCRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sevitout_rpc_requests_total",
		Help: "Total gRPC/REST requests, labeled by method and resulting gRPC status code.",
	}, []string{"method", "code"})

	// RPCDurationSeconds observes each call's duration, labeled the same way
	// as RPCRequestsTotal and from the same call site.
	RPCDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "sevitout_rpc_duration_seconds",
		Help: "gRPC/REST request duration in seconds, labeled by method and resulting gRPC status code.",
	}, []string{"method", "code"})

	// WSConnections tracks the number of currently open WebSocket
	// connections — Inc'd once a connection is upgraded and subscribed, and
	// Dec'd once it disconnects, in internal/api/ws.Handler.ServeHTTP.
	WSConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sevitout_ws_connections",
		Help: "Number of currently open WebSocket connections.",
	})

	// AIActionsTotal counts every AI plugin action dispatch, labeled by
	// outcome: "success" (stored and published), "error" (rate-limited or
	// failed at any step), or "skipped" (a proactive trigger's eligibility
	// gates — sensitive/AI-disabled/severity-too-low — rejected it before an
	// action ever ran). Incremented from internal/ai.Dispatcher's shared run
	// core (success/error) and runTrigger's eligibility gates (skipped).
	AIActionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sevitout_ai_action_total",
		Help: "Total AI plugin action dispatches, labeled by outcome (success, error, skipped).",
	}, []string{"outcome"})

	// OpenSEVs reports the current count of open SEVs (status Open,
	// Investigating, or Mitigated — the same set SearchService's "open"
	// quick-view preset uses), labeled by severity level. Populated by a
	// periodic background refresher (cmd/server/main.go) that reads the SEV
	// store directly, rather than incremented/decremented at every SEV
	// status-transition call site — far less invasive, at the cost of up to
	// one refresh interval of staleness (acceptable for a dashboard gauge,
	// not something anything alerts on with sub-refresh-interval latency).
	OpenSEVs = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "sevitout_open_sevs",
		Help: "Current number of open SEVs, labeled by severity level.",
	}, []string{"severity"})

	// DBPoolIdleConns, DBPoolUsedConns, and DBPoolMaxConns report the
	// PostgreSQL connection pool's state, populated by the same periodic
	// refresher from pgxpool.Pool.Stat() — preferred over per-query latency
	// histograms for now, since wrapping every sqlc-generated query call
	// individually is a large surface for a speculative win; per-query
	// instrumentation can follow if a real slow-query investigation needs
	// it. Left at zero when running against the in-memory dev fallback,
	// which has no pool to report on.
	DBPoolIdleConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sevitout_db_pool_idle_conns",
		Help: "Idle connections in the PostgreSQL connection pool.",
	})
	DBPoolUsedConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sevitout_db_pool_used_conns",
		Help: "In-use (acquired) connections in the PostgreSQL connection pool.",
	})
	DBPoolMaxConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sevitout_db_pool_max_conns",
		Help: "Maximum size of the PostgreSQL connection pool.",
	})
)
