package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/g8rswimmer/sevitout/internal/ai"
	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/api/ws"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/config"
	"github.com/g8rswimmer/sevitout/internal/integrations/pagerduty"
	"github.com/g8rswimmer/sevitout/internal/integrations/slack"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/github"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/jira"
	"github.com/g8rswimmer/sevitout/internal/postmortem"
	"github.com/g8rswimmer/sevitout/internal/share"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

//go:embed openapi/openapi.json
var openAPISpec []byte

func main() {
	ctx := context.Background()

	// cfg.Load reads every env var main() needs up front and never calls
	// os.Exit itself (see internal/config's doc comment) — but there's no
	// logger yet to report a problem through, since the logger's own level
	// comes from cfg. A malformed value (today, only JWT_TTL_HOURS) is
	// therefore reported directly to stderr rather than via slog.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     cfg.LogLevel,
	}))
	// Package-level slog.InfoContext/WarnContext/ErrorContext calls scattered
	// across internal/api/grpc and internal/auth (rather than a threaded
	// *slog.Logger field) rely on this to end up in the same structured JSON
	// stream as everything logged through log itself — without it they'd
	// fall back to slog's plain-text-to-stderr default and never carry
	// LOG_LEVEL or AddSource.
	slog.SetDefault(log)

	stores, err := buildStores(ctx, log, cfg.DatabaseURL)
	if err != nil {
		log.Error("postgres connect", "err", err)
		os.Exit(1)
	}

	// --- PagerDuty client (optional) ---
	var onCaller grpchandler.OnCaller
	if cfg.PagerDutyAPIKey != "" {
		onCaller = pagerduty.NewClient(cfg.PagerDutyAPIKey)
		log.Info("PagerDuty on-call integration enabled")
	}

	// --- GitHub client (optional) ---
	var issueClient grpchandler.IssueClient
	if cfg.GitHubToken != "" {
		issueClient = &githubIssueClient{c: github.NewClient(cfg.GitHubToken)}
		log.Info("GitHub Issues integration enabled")
	} else {
		log.Info("GitHub Issues integration DISABLED")
	}

	// --- Jira client (optional) --- all three of JIRA_BASE_URL/JIRA_EMAIL/
	// JIRA_API_TOKEN are required together (unlike GitHub's single
	// GITHUB_TOKEN — see config.Config.JiraBaseURL's doc comment for why);
	// partial configuration is treated the same as none rather than
	// starting with a client that would fail every call.
	var jiraClient grpchandler.JiraIssueClient
	if cfg.JiraBaseURL != "" && cfg.JiraEmail != "" && cfg.JiraAPIToken != "" {
		jiraClient = &jiraIssueClient{c: jira.NewClient(cfg.JiraBaseURL, cfg.JiraEmail, cfg.JiraAPIToken)}
		log.Info("Jira integration enabled")
	} else {
		log.Info("Jira integration DISABLED")
	}

	// --- JWT signer ---
	// A fixed, source-visible signing secret would let anyone forge a valid
	// session/unlock/share-link token, so this refuses to start rather than
	// silently falling back to one. ALLOW_INSECURE_JWT_SECRET=true is the
	// explicit, deliberate opt-in for local dev/CI convenience — the choice
	// to run insecurely has to be made by whoever starts the process, not
	// defaulted to by whoever forgets to set JWT_SECRET.
	jwtSecret := cfg.JWTSecret
	if jwtSecret == "" {
		if !cfg.AllowInsecureJWTSecret {
			log.Error("JWT_SECRET not set — refusing to start with a fixed signing secret. Set JWT_SECRET, or set ALLOW_INSECURE_JWT_SECRET=true to accept the insecure dev default")
			os.Exit(1)
		}
		log.Warn("JWT_SECRET not set — using insecure default (ALLOW_INSECURE_JWT_SECRET=true was set)")
		jwtSecret = "insecure-default-secret-change-before-deploying"
	}
	jwtSigner := auth.NewJWTSigner(jwtSecret, cfg.JWTTTLHours)

	// --- Unlock token signer (reuses JWT_SECRET; 15-min TTL) ---
	unlockSigner := postmortem.NewUnlockSigner(jwtSecret)

	// --- Shareable link signer (reuses JWT_SECRET; per-link expiry) ---
	shareSigner := share.NewSigner(jwtSecret)

	// --- Credential encryptor (optional): integration credentials are only
	// encryptable/decryptable when ENCRYPTION_KEY is set. Config API writes
	// that include credentials are rejected while it's absent. ---
	var encryptor grpchandler.Encryptor
	if cfg.EncryptionKey != "" {
		key, err := crypto.DecodeKey(cfg.EncryptionKey)
		if err != nil {
			log.Error("ENCRYPTION_KEY invalid (must be base64-encoded 32 bytes)", "err", err)
			os.Exit(1)
		}
		encryptor = crypto.NewKeyEncryptor(key)
		log.Info("integration credential encryption enabled")
	} else {
		log.Warn("ENCRYPTION_KEY not set — integration credentials cannot be stored")
	}

	// --- WebSocket hub: room-per-SEV pub/sub fed by the mutation handlers below ---
	wsHub := ws.NewHub()

	// --- AI plugin dispatcher (§11, M12): routes lifecycle triggers and
	// on-demand actions to whatever AI plugin(s) are registered via
	// ConfigService. encryptor (nil when ENCRYPTION_KEY is unset) doubles as
	// its Decryptor — grpchandler.Encryptor's method set is a superset of
	// ai.Decryptor's single Decrypt method. With no plugins registered yet,
	// the dispatcher is simply always a no-op; there's no separate "AI
	// disabled" toggle at this layer. ctx (the process-lifetime context, not
	// any single request's) governs its background worker pool. ---
	aiDispatcher := ai.NewDispatcher(ctx, stores.SEV, stores.StatusHistory, stores.Announcement, stores.AIPlugin, stores.AIOutput, encryptor, wsHub, log, 0)

	// --- gRPC server with auth interceptors ---
	sevServer := grpchandler.NewSEVServer(grpchandler.SEVServerParams{
		SEVs:        stores.SEV,
		Audit:       stores.Audit,
		History:     stores.StatusHistory,
		Roles:       stores.Role,
		Services:    stores.Service,
		Postmortems: stores.Postmortem,
		Links:       stores.SEVLink,
		Access:      stores.SEVAccess,
		OnCaller:    onCaller,
		Unlock:      unlockSigner,
		Publisher:   wsHub,
		AIDispatch:  aiDispatcher,
	})
	sevAccessServer := grpchandler.NewSEVAccessServer(stores.SEVAccess, stores.SEV, stores.Audit)
	reportServer := grpchandler.NewReportServer(stores.SEV, stores.Postmortem, stores.Task)
	shareServer := grpchandler.NewShareServer(grpchandler.ShareServerParams{
		Shares: stores.Share,
		SEVs:   stores.SEV,
		Audit:  stores.Audit,
		Signer: shareSigner,
	})
	auditServer := grpchandler.NewAuditServer(stores.Audit, stores.SEV, stores.SEVAccess)
	authServer := grpchandler.NewAuthServer(stores.User)
	roleServer := grpchandler.NewRoleServer(grpchandler.RoleServerParams{
		Roles: stores.Role, SEVs: stores.SEV, Access: stores.SEVAccess, Audit: stores.Audit, Publisher: wsHub,
	})
	postmortemServer := grpchandler.NewPostmortemServer(grpchandler.PostmortemServerParams{
		Postmortems: stores.Postmortem,
		SEVs:        stores.SEV,
		Access:      stores.SEVAccess,
		Audit:       stores.Audit,
		Unlock:      unlockSigner,
		Publisher:   wsHub,
		AIDispatch:  aiDispatcher,
	})
	announcementServer := grpchandler.NewAnnouncementServer(stores.Announcement, stores.SEV, stores.SEVAccess, wsHub)
	chatServer := grpchandler.NewChatServer(stores.Chat, stores.SEV, stores.SEVAccess, wsHub)
	sevLinkServer := grpchandler.NewSEVLinkServer(stores.SEVLink, stores.SEV, stores.SEVAccess, stores.Audit)
	taskServer := grpchandler.NewTaskServer(grpchandler.TaskServerParams{
		Tasks: stores.Task, SEVs: stores.SEV, Access: stores.SEVAccess, Audit: stores.Audit,
		GitHub: issueClient, Jira: jiraClient, Publisher: wsHub,
	})
	searchServer := grpchandler.NewSearchServer(stores.SEV, stores.Role, stores.Announcement)
	configServer := grpchandler.NewConfigServer(grpchandler.ConfigServerParams{
		Services:     stores.Service,
		Users:        stores.User,
		OnCall:       stores.OnCall,
		Integrations: stores.IntegrationConfig,
		Retention:    stores.RetentionConfig,
		AIPlugins:    stores.AIPlugin,
		Crypto:       encryptor,
		RateLimits:   aiDispatcher,
	})
	aiServer := grpchandler.NewAIServer(aiDispatcher, stores.AIOutput, stores.AIPlugin)

	grpcSrv := grpc.NewServer(
		// Three deep, outermost to innermost: request-ID, then auth, then
		// logging. Each attaches its own value to a *new* context.Context
		// (context.Context is immutable), which only propagates to
		// interceptors/handlers further in — an interceptor can't see a
		// context value added by something it calls. So request-ID has to run
		// before auth for auth.authenticate's own rejection logs to carry it,
		// and auth has to run before logging for its user_id attribution to
		// work; logging is therefore innermost. auth.authenticate itself logs
		// its own rejections (Warn, with request_id when available) so a call
		// that never reaches the logging interceptor is still visible.
		grpc.ChainUnaryInterceptor(
			grpchandler.RequestIDUnaryInterceptor(),
			auth.UnaryInterceptor(jwtSigner, stores.User),
			grpchandler.LoggingUnaryInterceptor(log),
		),
		grpc.ChainStreamInterceptor(
			grpchandler.RequestIDStreamInterceptor(),
			auth.StreamInterceptor(jwtSigner, stores.User),
			grpchandler.LoggingStreamInterceptor(log),
		),
	)
	pb.RegisterSEVServiceServer(grpcSrv, sevServer)
	pb.RegisterAuditServiceServer(grpcSrv, auditServer)
	pb.RegisterAuthServiceServer(grpcSrv, authServer)
	pb.RegisterRoleServiceServer(grpcSrv, roleServer)
	pb.RegisterPostmortemServiceServer(grpcSrv, postmortemServer)
	pb.RegisterAnnouncementServiceServer(grpcSrv, announcementServer)
	pb.RegisterChatServiceServer(grpcSrv, chatServer)
	pb.RegisterSEVLinkServiceServer(grpcSrv, sevLinkServer)
	pb.RegisterTaskServiceServer(grpcSrv, taskServer)
	pb.RegisterSearchServiceServer(grpcSrv, searchServer)
	pb.RegisterConfigServiceServer(grpcSrv, configServer)
	pb.RegisterAIServiceServer(grpcSrv, aiServer)
	pb.RegisterReportServiceServer(grpcSrv, reportServer)
	pb.RegisterShareServiceServer(grpcSrv, shareServer)
	pb.RegisterSEVAccessServiceServer(grpcSrv, sevAccessServer)
	reflection.Register(grpcSrv)

	// --- REST gateway ---
	// WithMetadata extracts the JWT from either the Authorization header or the
	// "token" httpOnly cookie and forwards it as gRPC metadata so the auth
	// interceptors can validate it. It also bridges an incoming X-Request-Id
	// HTTP header into the same gRPC metadata key
	// grpchandler.RequestIDUnaryInterceptor checks, so one correlation ID
	// survives the REST→loopback-gRPC hop instead of a fresh one being minted
	// at this boundary.
	gwMux := runtime.NewServeMux(
		// HTTPBodyMarshaler falls back to the wrapped JSONPb marshaler for
		// every response except google.api.HttpBody (ReportService.ExportSEVs'
		// CSV response), which it writes as raw bytes with the message's own
		// content_type instead of JSON-encoding it.
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.HTTPBodyMarshaler{
			Marshaler: &runtime.JSONPb{
				MarshalOptions: protojson.MarshalOptions{UseProtoNames: true},
			},
		}),
		runtime.WithMetadata(func(_ context.Context, r *http.Request) metadata.MD {
			return gatewayMetadata(r)
		}),
	)
	// Dial a single loopback connection shared across all gateway services.
	// RegisterXxxHandlerClient does not spawn cleanup goroutines, so the
	// context lifetime does not affect connection cleanup.
	const addr = ":8080"
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("dial loopback gRPC", "err", err)
		os.Exit(1)
	}
	// Every gRPC service above needs its REST gateway registered against the
	// same loopback conn; each Register*HandlerClient call takes a
	// differently-typed client, so they're listed as name+closure pairs
	// rather than something more mechanical like a slice of clients.
	gatewayServices := []struct {
		name     string
		register func() error
	}{
		{"sev", func() error { return pb.RegisterSEVServiceHandlerClient(ctx, gwMux, pb.NewSEVServiceClient(conn)) }},
		{"audit", func() error { return pb.RegisterAuditServiceHandlerClient(ctx, gwMux, pb.NewAuditServiceClient(conn)) }},
		{"auth", func() error { return pb.RegisterAuthServiceHandlerClient(ctx, gwMux, pb.NewAuthServiceClient(conn)) }},
		{"role", func() error { return pb.RegisterRoleServiceHandlerClient(ctx, gwMux, pb.NewRoleServiceClient(conn)) }},
		{"postmortem", func() error {
			return pb.RegisterPostmortemServiceHandlerClient(ctx, gwMux, pb.NewPostmortemServiceClient(conn))
		}},
		{"announcement", func() error {
			return pb.RegisterAnnouncementServiceHandlerClient(ctx, gwMux, pb.NewAnnouncementServiceClient(conn))
		}},
		{"chat", func() error { return pb.RegisterChatServiceHandlerClient(ctx, gwMux, pb.NewChatServiceClient(conn)) }},
		{"sev-link", func() error {
			return pb.RegisterSEVLinkServiceHandlerClient(ctx, gwMux, pb.NewSEVLinkServiceClient(conn))
		}},
		{"task", func() error { return pb.RegisterTaskServiceHandlerClient(ctx, gwMux, pb.NewTaskServiceClient(conn)) }},
		{"search", func() error {
			return pb.RegisterSearchServiceHandlerClient(ctx, gwMux, pb.NewSearchServiceClient(conn))
		}},
		{"config", func() error {
			return pb.RegisterConfigServiceHandlerClient(ctx, gwMux, pb.NewConfigServiceClient(conn))
		}},
		{"ai", func() error { return pb.RegisterAIServiceHandlerClient(ctx, gwMux, pb.NewAIServiceClient(conn)) }},
		{"report", func() error {
			return pb.RegisterReportServiceHandlerClient(ctx, gwMux, pb.NewReportServiceClient(conn))
		}},
		{"share", func() error { return pb.RegisterShareServiceHandlerClient(ctx, gwMux, pb.NewShareServiceClient(conn)) }},
		{"sev-access", func() error {
			return pb.RegisterSEVAccessServiceHandlerClient(ctx, gwMux, pb.NewSEVAccessServiceClient(conn))
		}},
	}
	for _, svc := range gatewayServices {
		if err := svc.register(); err != nil {
			log.Error("register "+svc.name+" gateway", "err", err)
			os.Exit(1)
		}
	}

	// --- Password auth handler ---
	passwordHandler := auth.NewPasswordHandler(jwtSigner, stores.User)

	// --- Integration health-check handler (GET /admin/integrations/health) ---
	healthCheckers := map[string]grpchandler.HealthChecker{
		"pagerduty": pagerdutyHealthChecker{},
		"github":    githubHealthChecker{},
		"slack":     slackHealthChecker{},
		"jira":      jiraHealthChecker{},
	}
	integrationsHealthHandler := grpchandler.NewIntegrationsHealthHandler(grpchandler.IntegrationsHealthHandlerParams{
		Integrations: stores.IntegrationConfig,
		Crypto:       encryptor,
		Checkers:     healthCheckers,
		Signer:       jwtSigner,
		Users:        stores.User,
	})

	// --- HTTP mux ---
	httpMux := http.NewServeMux()
	// passwordHandler, gwMux (the gRPC-gateway) and the WebSocket upgrade all
	// have their own request-scoped logging already: passwordHandler logs
	// login/register outcomes directly (internal/auth/password.go), and
	// every gwMux-proxied call is a real loopback gRPC call that passes
	// through grpchandler.LoggingUnaryInterceptor above. The three plain
	// http.Handlers below have neither, so they're wrapped individually
	// rather than logging the whole mux (which would double-log every
	// gateway request).
	passwordHandler.RegisterRoutes(httpMux)                                                           // POST /auth/register, POST /auth/login
	httpMux.Handle("/ws", loggingMiddleware(log, "ws", ws.NewHandler(wsHub, jwtSigner, stores.User))) // WebSocket subscriptions
	httpMux.Handle("/admin/integrations/health", loggingMiddleware(log, "integrations-health", integrationsHealthHandler))
	// GET /s/{token}: public shareable-link view (§14.1) — no auth, so it
	// can't be a gRPC/grpc-gateway route (see share.proto's doc comment).
	// Go's ServeMux prefers this more specific pattern over "/" below.
	shareViewHandler := grpchandler.NewShareViewHandler(grpchandler.ShareViewHandlerParams{
		Shares: stores.Share, SEVs: stores.SEV, Announcements: stores.Announcement, Validator: shareSigner,
	})
	httpMux.Handle("/s/{token}", loggingMiddleware(log, "share-view", shareViewHandler))
	httpMux.Handle("/", gwMux) // gRPC-gateway routes
	httpMux.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAPISpec)
	})
	// GET /metrics: Prometheus scrape target. Deliberately unauthenticated
	// and un-logged (a scraper polls this every few seconds; an access-log
	// line per scrape would be pure noise), matching /openapi.json's
	// treatment above and standard Prometheus scrape convention.
	httpMux.Handle("/metrics", promhttp.Handler())
	// GET /healthz: liveness/readiness probe for a container orchestrator.
	// Deliberately unauthenticated and un-logged on success, same rationale
	// as /metrics above — see grpchandler.NewHealthzHandler's doc comment for
	// how this differs from the authenticated, admin-only
	// /admin/integrations/health.
	httpMux.Handle("/healthz", grpchandler.NewHealthzHandler(stores))

	// --- Background metrics refresher: sevitout_open_sevs and (when
	// running against real Postgres) sevitout_db_pool_* — see
	// startMetricsRefresher's doc comment.
	go startMetricsRefresher(ctx, log, stores.SEV, stores.Pool)

	// --- cmux: gRPC and HTTP/1.1 on the same TCP port ---
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	mx := cmux.New(lis)
	grpcL := mx.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	httpL := mx.Match(cmux.Any())

	log.Info("sevitout api starting", "addr", addr)

	go func() {
		if err := grpcSrv.Serve(grpcL); err != nil {
			log.Error("grpc serve", "err", err)
		}
	}()
	go func() {
		if err := (&http.Server{Handler: httpMux}).Serve(httpL); err != nil && err != http.ErrServerClosed {
			log.Error("http serve", "err", err)
		}
	}()
	if err := mx.Serve(); err != nil {
		log.Error("cmux", "err", err)
		os.Exit(1)
	}
}

// Stores bundles every store interface the server depends on. buildStores
// constructs one, backed either by PostgreSQL or (DATABASE_URL unset) the
// in-memory fallback, and main threads it into whichever gRPC
// servers/handlers need a given field.
type Stores struct {
	SEV               store.SEVStore
	Audit             store.AuditStore
	StatusHistory     store.StatusHistoryStore
	User              store.UserStore
	Role              store.RoleStore
	Service           store.ServiceStore
	Postmortem        store.PostmortemStore
	Announcement      store.AnnouncementStore
	Chat              store.ChatStore
	SEVLink           store.SEVLinkStore
	Task              store.TaskStore
	OnCall            store.OnCallStore
	IntegrationConfig store.IntegrationConfigStore
	RetentionConfig   store.RetentionConfigStore
	AIPlugin          store.AIPluginStore
	AIOutput          store.AIOutputStore
	Share             store.ShareStore
	SEVAccess         store.SEVAccessStore

	// Pool is the underlying PostgreSQL connection pool, or nil when running
	// against the in-memory dev fallback (DATABASE_URL unset). Exposed
	// alongside the store interfaces above so main() can read
	// pgxpool.Pool.Stat() for the sevitout_db_pool_* metrics without every
	// caller needing it threaded through separately.
	Pool *pgxpool.Pool
}

// Ping reports whether the backing store is reachable, satisfying
// grpchandler.Pinger for GET /healthz. Against real Postgres it delegates to
// pgxpool.Pool.Ping; against the in-memory dev fallback (Pool nil) it's a
// no-op that always succeeds — there's no connection to lose.
func (s *Stores) Ping(ctx context.Context) error {
	if s.Pool == nil {
		return nil
	}
	return s.Pool.Ping(ctx)
}

// buildStores selects the store backend: in-memory when dsn is empty,
// PostgreSQL (connecting and verifying reachability via postgres.Open)
// otherwise. It reports a connect failure via its error return rather than
// exiting itself, so the exit decision stays in main alongside the other
// startup fail-closed checks (JWT_SECRET, ENCRYPTION_KEY) — and so this is
// testable in-process without a real Postgres for the failure path.
func buildStores(ctx context.Context, log *slog.Logger, dsn string) (*Stores, error) {
	if dsn == "" {
		log.Warn("DATABASE_URL not set — using in-memory store (data will not persist)")
		return &Stores{
			SEV:               memory.NewSEVStore(),
			Audit:             memory.NewAuditStore(),
			StatusHistory:     memory.NewStatusHistoryStore(),
			User:              memory.NewUserStore(),
			Role:              memory.NewRoleStore(),
			Service:           memory.NewServiceStore(),
			Postmortem:        memory.NewPostmortemStore(),
			Announcement:      memory.NewAnnouncementStore(),
			Chat:              memory.NewChatStore(),
			SEVLink:           memory.NewSEVLinkStore(),
			Task:              memory.NewTaskStore(),
			OnCall:            memory.NewOnCallStore(),
			IntegrationConfig: memory.NewIntegrationConfigStore(),
			RetentionConfig:   memory.NewRetentionConfigStore(),
			AIPlugin:          memory.NewAIPluginStore(),
			AIOutput:          memory.NewAIOutputStore(),
			Share:             memory.NewShareStore(),
			SEVAccess:         memory.NewSEVAccessStore(),
		}, nil
	}
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	log.Info("using postgres store")
	return &Stores{
		Pool:              pool,
		SEV:               postgres.NewSEVStore(pool),
		Audit:             postgres.NewAuditStore(pool),
		StatusHistory:     postgres.NewStatusHistoryStore(pool),
		User:              postgres.NewUserStore(pool),
		Role:              postgres.NewRoleStore(pool),
		Service:           postgres.NewServiceStore(pool),
		Postmortem:        postgres.NewPostmortemStore(pool),
		Announcement:      postgres.NewAnnouncementStore(pool),
		Chat:              postgres.NewChatStore(pool),
		SEVLink:           postgres.NewSEVLinkStore(pool),
		Task:              postgres.NewTaskStore(pool),
		OnCall:            postgres.NewOnCallStore(pool),
		IntegrationConfig: postgres.NewIntegrationConfigStore(pool),
		RetentionConfig:   postgres.NewRetentionConfigStore(pool),
		AIPlugin:          postgres.NewAIPluginStore(pool),
		AIOutput:          postgres.NewAIOutputStore(pool),
		Share:             postgres.NewShareStore(pool),
		SEVAccess:         postgres.NewSEVAccessStore(pool),
	}, nil
}

// githubIssueClient adapts *github.Client to grpchandler.IssueClient,
// keeping the grpc package's interface decoupled from this integration's
// concrete request/response types.
type githubIssueClient struct {
	c *github.Client
}

func (a *githubIssueClient) CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (*grpchandler.CreatedIssue, error) {
	issue, err := a.c.CreateIssue(ctx, github.CreateIssueRequest{
		Owner:  owner,
		Repo:   repo,
		Title:  title,
		Body:   body,
		Labels: labels,
	})
	if err != nil {
		return nil, err
	}
	return &grpchandler.CreatedIssue{
		Number: issue.Number,
		Title:  issue.Title,
		Body:   issue.Body,
		URL:    issue.HTMLURL,
	}, nil
}

// jiraIssueClient adapts *jira.Client to grpchandler.JiraIssueClient, mirroring
// githubIssueClient above.
type jiraIssueClient struct {
	c *jira.Client
}

func (a *jiraIssueClient) CreateIssue(ctx context.Context, projectKey, issueType, summary, description string, labels []string) (*grpchandler.CreatedIssue, error) {
	issue, err := a.c.CreateIssue(ctx, jira.CreateIssueRequest{
		ProjectKey:  projectKey,
		IssueType:   issueType,
		Summary:     summary,
		Description: description,
		Labels:      labels,
	})
	if err != nil {
		return nil, err
	}
	return &grpchandler.CreatedIssue{
		Key:   issue.Key,
		Title: issue.Summary,
		Body:  issue.Description,
		URL:   issue.URL,
	}, nil
}

// pagerdutyHealthChecker adapts pagerduty.Client to grpchandler.HealthChecker,
// building a fresh client per check from the configured integration's own
// decrypted credentials (rather than the singleton client built from
// PAGERDUTY_API_KEY above, which config-API-managed credentials are separate
// from).
type pagerdutyHealthChecker struct{}

func (pagerdutyHealthChecker) Check(ctx context.Context, credentials map[string]string, _ map[string]any) error {
	apiKey := credentials["api_key"]
	if apiKey == "" {
		return fmt.Errorf("pagerduty: no api_key configured")
	}
	return pagerduty.NewClient(apiKey).Ping(ctx)
}

// githubHealthChecker adapts github.Client to grpchandler.HealthChecker; see
// pagerdutyHealthChecker for why a fresh client is built per check.
type githubHealthChecker struct{}

func (githubHealthChecker) Check(ctx context.Context, credentials map[string]string, _ map[string]any) error {
	token := credentials["token"]
	if token == "" {
		return fmt.Errorf("github: no token configured")
	}
	return github.NewClient(token).Ping(ctx)
}

// jiraHealthChecker adapts jira.Client to grpchandler.HealthChecker; see
// pagerdutyHealthChecker for why a fresh client is built per check. Unlike
// pagerduty/github's single-credential Check, this needs three config-API-
// managed values (base_url, email, api_token) — settings, not credentials,
// for base_url specifically, since it identifies which Jira tenant to call
// rather than authenticating to it, mirroring how internal/config.Config's
// JIRA_BASE_URL is a required companion to the credential pair at the
// process-level integration too.
type jiraHealthChecker struct{}

func (jiraHealthChecker) Check(ctx context.Context, credentials map[string]string, settings map[string]any) error {
	email, apiToken := credentials["email"], credentials["api_token"]
	if email == "" || apiToken == "" {
		return fmt.Errorf("jira: no email/api_token configured")
	}
	baseURL, _ := settings["base_url"].(string)
	if baseURL == "" {
		return fmt.Errorf("jira: no base_url configured")
	}
	return jira.NewClient(baseURL, email, apiToken).Ping(ctx)
}

// slackHealthChecker adapts slack.Client to grpchandler.HealthChecker. Its
// credentials are independent of the slackbot process's own SLACK_BOT_TOKEN
// (docs/project-plan.md M11) — this is a bot token an admin optionally
// stores via the Config API purely so the admin page can report Slack
// connectivity, same as pagerdutyHealthChecker/githubHealthChecker above.
type slackHealthChecker struct{}

func (slackHealthChecker) Check(ctx context.Context, credentials map[string]string, _ map[string]any) error {
	token := credentials["bot_token"]
	if token == "" {
		return fmt.Errorf("slack: no bot_token configured")
	}
	return slack.NewClient(token).Ping(ctx)
}

// openSEVSeverityLevels are the fixed SEV-1..SEV-4 severities
// (docs/requirements.md §3) refreshMetrics reports telemetry.OpenSEVs for,
// each cycle, even when a level currently has zero open SEVs — so a
// severity that just emptied out reads 0 on the next scrape instead of
// silently keeping its last nonzero value forever.
var openSEVSeverityLevels = [...]int16{1, 2, 3, 4}

// metricsRefreshInterval governs startMetricsRefresher: frequent enough for
// a dashboard to feel current, infrequent enough that the periodic SEV list
// query is negligible load. This is a gauge for humans looking at a
// dashboard, not something anything alerts on with sub-refresh latency
// requirements.
const metricsRefreshInterval = 30 * time.Second

// startMetricsRefresher runs until ctx is done, calling refreshMetrics
// immediately and then every metricsRefreshInterval. It's launched as its
// own goroutine from main() rather than folded into any request path, since
// neither of the metrics it maintains (telemetry.OpenSEVs,
// telemetry.DBPoolIdleConns/UsedConns/MaxConns) has a natural per-request
// call site to update from — they're periodic snapshots of store/pool
// state, not counters of things that happen.
func startMetricsRefresher(ctx context.Context, log *slog.Logger, sevs store.SEVStore, pool *pgxpool.Pool) {
	refreshMetrics(ctx, log, sevs, pool)
	ticker := time.NewTicker(metricsRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshMetrics(ctx, log, sevs, pool)
		}
	}
}

// refreshMetrics recomputes telemetry.OpenSEVs (by severity, using the same
// "open" status set — Open, Investigating, Mitigated — as SearchService's
// "open" quick-view preset in internal/api/grpc/search.go) and, when pool is
// non-nil (i.e. running against real Postgres, not the in-memory dev
// fallback), telemetry.DBPoolIdleConns/UsedConns/MaxConns from
// pgxpool.Pool.Stat().
func refreshMetrics(ctx context.Context, log *slog.Logger, sevs store.SEVStore, pool *pgxpool.Pool) {
	open, err := sevs.List(ctx, store.SEVFilter{
		Statuses:         []store.SEVStatus{store.SEVStatusOpen, store.SEVStatusInvestigating, store.SEVStatusMitigated},
		ExcludeSensitive: true,
	})
	if err != nil {
		log.ErrorContext(ctx, "metrics refresh: list open SEVs failed", "err", err)
	} else {
		counts := make(map[int16]int, len(openSEVSeverityLevels))
		for _, sv := range open {
			counts[sv.SeverityLevel]++
		}
		for _, level := range openSEVSeverityLevels {
			telemetry.OpenSEVs.WithLabelValues(strconv.Itoa(int(level))).Set(float64(counts[level]))
		}
	}

	if pool != nil {
		stat := pool.Stat()
		telemetry.DBPoolIdleConns.Set(float64(stat.IdleConns()))
		telemetry.DBPoolUsedConns.Set(float64(stat.AcquiredConns()))
		telemetry.DBPoolMaxConns.Set(float64(stat.MaxConns()))
	}
}

// gatewayMetadata builds the gRPC metadata grpc-gateway attaches to every
// loopback call it makes on behalf of an incoming REST request: the caller's
// bearer token (from either the Authorization header or the "token"
// httpOnly cookie) so the auth interceptors can validate it, and an
// X-Request-Id header, if present, forwarded as
// grpchandler.RequestIDMetadataKey so RequestIDUnaryInterceptor reuses it
// instead of minting a fresh ID at this hop. Returns nil (grpc-gateway's
// documented "no extra metadata" value) when neither is present, rather
// than an empty-but-non-nil MD.
func gatewayMetadata(r *http.Request) metadata.MD {
	var pairs []string
	if v := r.Header.Get("Authorization"); v != "" {
		pairs = append(pairs, "authorization", v)
	} else if c, err := r.Cookie("token"); err == nil {
		pairs = append(pairs, "authorization", "Bearer "+c.Value)
	}
	if v := r.Header.Get("X-Request-Id"); v != "" {
		pairs = append(pairs, grpchandler.RequestIDMetadataKey, v)
	}
	if len(pairs) == 0 {
		return nil
	}
	return metadata.Pairs(pairs...)
}

// loggingMiddleware wraps next with a request-level access log — method,
// path, resulting status code, and duration — tagged with name so the three
// plain http.Handlers that sit outside the gRPC server (WebSocket upgrades,
// the integration health check, and the public share view) get the same
// visibility grpchandler.LoggingUnaryInterceptor gives every gRPC/REST call.
//
// It also gives these three handlers the same request-scoped logging the
// gRPC path has: it reuses an incoming X-Request-Id header or mints a fresh
// UUID, echoes it back in the response header, and binds a *slog.Logger
// carrying that request_id into the request's context (via
// telemetry.WithLogger) so next can retrieve it with
// telemetry.LoggerFromContext(r.Context()) — this is what lets
// share_view.go and integrations_health.go log their own internal failures
// instead of only ever calling http.Error.
func loggingMiddleware(log *slog.Logger, name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		reqID := r.Header.Get("X-Request-Id")
		if reqID == "" {
			reqID = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", reqID)
		reqLog := log.With("request_id", reqID)
		ctx := telemetry.WithRequestID(r.Context(), reqID)
		ctx = telemetry.WithLogger(ctx, reqLog)
		r = r.WithContext(ctx)

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		reqLog.InfoContext(r.Context(), "http request",
			"handler", name, "method", r.Method, "path", r.URL.Path,
			"status", sw.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

// statusWriter captures the status code passed to WriteHeader so
// loggingMiddleware can log it — http.ResponseWriter doesn't expose it
// directly, and a handler that never calls WriteHeader explicitly (the
// common case for a 200) gets the http.StatusOK default set above.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
