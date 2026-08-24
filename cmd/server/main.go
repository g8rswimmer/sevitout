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
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
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
	"github.com/g8rswimmer/sevitout/internal/integrations/pagerduty"
	"github.com/g8rswimmer/sevitout/internal/integrations/slack"
	"github.com/g8rswimmer/sevitout/internal/integrations/tasktracker/github"
	"github.com/g8rswimmer/sevitout/internal/postmortem"
	"github.com/g8rswimmer/sevitout/internal/share"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

//go:embed openapi/openapi.json
var openAPISpec []byte

func main() {
	ctx := context.Background()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     parseLogLevel(os.Getenv("LOG_LEVEL")),
	}))
	// Package-level slog.InfoContext/WarnContext/ErrorContext calls scattered
	// across internal/api/grpc and internal/auth (rather than a threaded
	// *slog.Logger field) rely on this to end up in the same structured JSON
	// stream as everything logged through log itself — without it they'd
	// fall back to slog's plain-text-to-stderr default and never carry
	// LOG_LEVEL or AddSource.
	slog.SetDefault(log)

	sevStore, auditStore, historyStore, userStore, roleStore, serviceStore, postmortemStore,
		announcementStore, chatStore, sevLinkStore, taskStore, onCallStore, integrationConfigStore,
		retentionConfigStore, aiPluginStore, aiOutputStore, shareStore := buildStores(ctx, log)

	// --- PagerDuty client (optional) ---
	var onCaller grpchandler.OnCaller
	if apiKey := os.Getenv("PAGERDUTY_API_KEY"); apiKey != "" {
		onCaller = pagerduty.NewClient(apiKey)
		log.Info("PagerDuty on-call integration enabled")
	}

	// --- GitHub client (optional) ---
	var issueClient grpchandler.IssueClient
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		issueClient = &githubIssueClient{c: github.NewClient(token)}
		log.Info("GitHub Issues integration enabled")
	} else {
		log.Info("GitHub Issues integration DISABLED")
	}

	// --- JWT signer ---
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Warn("JWT_SECRET not set — using insecure default (change before deploying)")
		jwtSecret = "insecure-default-secret-change-before-deploying"
	}
	jwtTTLHours := 24
	if v := os.Getenv("JWT_TTL_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			jwtTTLHours = n
		}
	}
	jwtSigner := auth.NewJWTSigner(jwtSecret, jwtTTLHours)

	// --- Unlock token signer (reuses JWT_SECRET; 15-min TTL) ---
	unlockSigner := postmortem.NewUnlockSigner(jwtSecret)

	// --- Shareable link signer (reuses JWT_SECRET; per-link expiry) ---
	shareSigner := share.NewSigner(jwtSecret)

	// --- Credential encryptor (optional): integration credentials are only
	// encryptable/decryptable when ENCRYPTION_KEY is set. Config API writes
	// that include credentials are rejected while it's absent. ---
	var encryptor grpchandler.Encryptor
	if raw := os.Getenv("ENCRYPTION_KEY"); raw != "" {
		key, err := crypto.DecodeKey(raw)
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
	aiDispatcher := ai.NewDispatcher(ctx, sevStore, historyStore, announcementStore, aiPluginStore, aiOutputStore, encryptor, wsHub, log, 0)

	// --- gRPC server with auth interceptors ---
	sevServer := grpchandler.NewSEVServer(sevStore, auditStore, historyStore, roleStore, serviceStore, postmortemStore, sevLinkStore, onCaller, unlockSigner, wsHub, aiDispatcher)
	reportServer := grpchandler.NewReportServer(sevStore, postmortemStore, taskStore)
	shareServer := grpchandler.NewShareServer(shareStore, sevStore, auditStore, shareSigner)
	auditServer := grpchandler.NewAuditServer(auditStore)
	authServer := grpchandler.NewAuthServer(userStore)
	roleServer := grpchandler.NewRoleServer(roleStore, sevStore, auditStore, wsHub)
	postmortemServer := grpchandler.NewPostmortemServer(postmortemStore, sevStore, auditStore, unlockSigner, wsHub, aiDispatcher)
	announcementServer := grpchandler.NewAnnouncementServer(announcementStore, sevStore, wsHub)
	chatServer := grpchandler.NewChatServer(chatStore, sevStore, wsHub)
	sevLinkServer := grpchandler.NewSEVLinkServer(sevLinkStore, sevStore, auditStore)
	taskServer := grpchandler.NewTaskServer(taskStore, sevStore, auditStore, issueClient, wsHub)
	searchServer := grpchandler.NewSearchServer(sevStore, roleStore, announcementStore)
	configServer := grpchandler.NewConfigServer(serviceStore, userStore, onCallStore, integrationConfigStore, retentionConfigStore, aiPluginStore, encryptor, aiDispatcher)
	aiServer := grpchandler.NewAIServer(aiDispatcher, aiOutputStore, aiPluginStore)

	grpcSrv := grpc.NewServer(
		// auth runs outermost (not logging): it attaches *auth.UserContext to
		// ctx via a new context.Context value, which only propagates to
		// interceptors/handlers further in — an interceptor can't see a
		// context value added by something it calls. Logging needs to run
		// after auth for its user_id attribution to work, so it's innermost
		// here; auth.authenticate itself logs its own rejections (Warn) so a
		// call that never reaches this logging interceptor is still visible.
		grpc.ChainUnaryInterceptor(auth.UnaryInterceptor(jwtSigner, userStore), grpchandler.LoggingUnaryInterceptor(log)),
		grpc.ChainStreamInterceptor(auth.StreamInterceptor(jwtSigner, userStore), grpchandler.LoggingStreamInterceptor(log)),
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
	reflection.Register(grpcSrv)

	// --- REST gateway ---
	// WithMetadata extracts the JWT from either the Authorization header or the
	// "token" httpOnly cookie and forwards it as gRPC metadata so the auth
	// interceptors can validate it.
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
			if v := r.Header.Get("Authorization"); v != "" {
				return metadata.Pairs("authorization", v)
			}
			if c, err := r.Cookie("token"); err == nil {
				return metadata.Pairs("authorization", "Bearer "+c.Value)
			}
			return nil
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
	if err := pb.RegisterSEVServiceHandlerClient(ctx, gwMux, pb.NewSEVServiceClient(conn)); err != nil {
		log.Error("register sev gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterAuditServiceHandlerClient(ctx, gwMux, pb.NewAuditServiceClient(conn)); err != nil {
		log.Error("register audit gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterAuthServiceHandlerClient(ctx, gwMux, pb.NewAuthServiceClient(conn)); err != nil {
		log.Error("register auth gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterRoleServiceHandlerClient(ctx, gwMux, pb.NewRoleServiceClient(conn)); err != nil {
		log.Error("register role gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterPostmortemServiceHandlerClient(ctx, gwMux, pb.NewPostmortemServiceClient(conn)); err != nil {
		log.Error("register postmortem gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterAnnouncementServiceHandlerClient(ctx, gwMux, pb.NewAnnouncementServiceClient(conn)); err != nil {
		log.Error("register announcement gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterChatServiceHandlerClient(ctx, gwMux, pb.NewChatServiceClient(conn)); err != nil {
		log.Error("register chat gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterSEVLinkServiceHandlerClient(ctx, gwMux, pb.NewSEVLinkServiceClient(conn)); err != nil {
		log.Error("register sev-link gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterTaskServiceHandlerClient(ctx, gwMux, pb.NewTaskServiceClient(conn)); err != nil {
		log.Error("register task gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterSearchServiceHandlerClient(ctx, gwMux, pb.NewSearchServiceClient(conn)); err != nil {
		log.Error("register search gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterConfigServiceHandlerClient(ctx, gwMux, pb.NewConfigServiceClient(conn)); err != nil {
		log.Error("register config gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterAIServiceHandlerClient(ctx, gwMux, pb.NewAIServiceClient(conn)); err != nil {
		log.Error("register ai gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterReportServiceHandlerClient(ctx, gwMux, pb.NewReportServiceClient(conn)); err != nil {
		log.Error("register report gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterShareServiceHandlerClient(ctx, gwMux, pb.NewShareServiceClient(conn)); err != nil {
		log.Error("register share gateway", "err", err)
		os.Exit(1)
	}

	// --- Password auth handler ---
	passwordHandler := auth.NewPasswordHandler(jwtSigner, userStore)

	// --- Integration health-check handler (GET /admin/integrations/health) ---
	healthCheckers := map[string]grpchandler.HealthChecker{
		"pagerduty": pagerdutyHealthChecker{},
		"github":    githubHealthChecker{},
		"slack":     slackHealthChecker{},
	}
	integrationsHealthHandler := grpchandler.NewIntegrationsHealthHandler(
		integrationConfigStore, encryptor, healthCheckers, jwtSigner, userStore)

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
	passwordHandler.RegisterRoutes(httpMux)                                                         // POST /auth/register, POST /auth/login
	httpMux.Handle("/ws", loggingMiddleware(log, "ws", ws.NewHandler(wsHub, jwtSigner, userStore))) // WebSocket subscriptions
	httpMux.Handle("/admin/integrations/health", loggingMiddleware(log, "integrations-health", integrationsHealthHandler))
	// GET /s/{token}: public shareable-link view (§14.1) — no auth, so it
	// can't be a gRPC/grpc-gateway route (see share.proto's doc comment).
	// Go's ServeMux prefers this more specific pattern over "/" below.
	httpMux.Handle("/s/{token}", loggingMiddleware(log, "share-view", grpchandler.NewShareViewHandler(shareStore, sevStore, announcementStore, shareSigner)))
	httpMux.Handle("/", gwMux) // gRPC-gateway routes
	httpMux.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAPISpec)
	})

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

func buildStores(ctx context.Context, log *slog.Logger) (
	store.SEVStore,
	store.AuditStore,
	store.StatusHistoryStore,
	store.UserStore,
	store.RoleStore,
	store.ServiceStore,
	store.PostmortemStore,
	store.AnnouncementStore,
	store.ChatStore,
	store.SEVLinkStore,
	store.TaskStore,
	store.OnCallStore,
	store.IntegrationConfigStore,
	store.RetentionConfigStore,
	store.AIPluginStore,
	store.AIOutputStore,
	store.ShareStore,
) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Warn("DATABASE_URL not set — using in-memory store (data will not persist)")
		return memory.NewSEVStore(),
			memory.NewAuditStore(),
			memory.NewStatusHistoryStore(),
			memory.NewUserStore(),
			memory.NewRoleStore(),
			memory.NewServiceStore(),
			memory.NewPostmortemStore(),
			memory.NewAnnouncementStore(),
			memory.NewChatStore(),
			memory.NewSEVLinkStore(),
			memory.NewTaskStore(),
			memory.NewOnCallStore(),
			memory.NewIntegrationConfigStore(),
			memory.NewRetentionConfigStore(),
			memory.NewAIPluginStore(),
			memory.NewAIOutputStore(),
			memory.NewShareStore()
	}
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		log.Error("postgres connect", "err", err)
		os.Exit(1)
	}
	log.Info("using postgres store")
	return postgres.NewSEVStore(pool),
		postgres.NewAuditStore(pool),
		postgres.NewStatusHistoryStore(pool),
		postgres.NewUserStore(pool),
		postgres.NewRoleStore(pool),
		postgres.NewServiceStore(pool),
		postgres.NewPostmortemStore(pool),
		postgres.NewAnnouncementStore(pool),
		postgres.NewChatStore(pool),
		postgres.NewSEVLinkStore(pool),
		postgres.NewTaskStore(pool),
		postgres.NewOnCallStore(pool),
		postgres.NewIntegrationConfigStore(pool),
		postgres.NewRetentionConfigStore(pool),
		postgres.NewAIPluginStore(pool),
		postgres.NewAIOutputStore(pool),
		postgres.NewShareStore(pool)
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

// parseLogLevel maps LOG_LEVEL's value ("debug", "info", "warn"/"warning",
// "error", case-insensitive) to a slog.Level, defaulting to Info for an
// empty or unrecognized value so a typo degrades gracefully instead of
// silencing every log line.
func parseLogLevel(v string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// loggingMiddleware wraps next with a request-level access log — method,
// path, resulting status code, and duration — tagged with name so the three
// plain http.Handlers that sit outside the gRPC server (WebSocket upgrades,
// the integration health check, and the public share view) get the same
// visibility grpchandler.LoggingUnaryInterceptor gives every gRPC/REST call.
func loggingMiddleware(log *slog.Logger, name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.InfoContext(r.Context(), "http request",
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
