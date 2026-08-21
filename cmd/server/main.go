package main

import (
	"context"
	_ "embed"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/encoding/protojson"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/integrations/github"
	"github.com/g8rswimmer/sevitout/internal/integrations/pagerduty"
	"github.com/g8rswimmer/sevitout/internal/postmortem"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

//go:embed openapi/openapi.json
var openAPISpec []byte

func main() {
	ctx := context.Background()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	sevStore, auditStore, historyStore, userStore, roleStore, serviceStore, postmortemStore,
		announcementStore, chatStore, sevLinkStore, taskStore := buildStores(ctx, log)

	// --- PagerDuty client (optional) ---
	var onCaller grpchandler.OnCaller
	if apiKey := os.Getenv("PAGERDUTY_API_KEY"); apiKey != "" {
		onCaller = pagerduty.NewClient(apiKey)
		log.Info("PagerDuty on-call integration enabled")
	}

	// --- GitHub client (optional) ---
	var issueClient grpchandler.IssueClient
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		gh := github.NewClient(token)
		issueClient = &githubClientAdapter{gh}
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

	// --- gRPC server with auth interceptors ---
	sevServer := grpchandler.NewSEVServer(sevStore, auditStore, historyStore, roleStore, serviceStore, postmortemStore, onCaller, unlockSigner)
	auditServer := grpchandler.NewAuditServer(auditStore)
	authServer := grpchandler.NewAuthServer(userStore)
	roleServer := grpchandler.NewRoleServer(roleStore, sevStore, auditStore)
	postmortemServer := grpchandler.NewPostmortemServer(postmortemStore, sevStore, auditStore, unlockSigner)
	announcementServer := grpchandler.NewAnnouncementServer(announcementStore, sevStore)
	chatServer := grpchandler.NewChatServer(chatStore, sevStore)
	sevLinkServer := grpchandler.NewSEVLinkServer(sevLinkStore, sevStore, auditStore)
	taskServer := grpchandler.NewTaskServer(taskStore, sevStore, auditStore, issueClient)

	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(auth.UnaryInterceptor(jwtSigner, userStore)),
		grpc.StreamInterceptor(auth.StreamInterceptor(jwtSigner, userStore)),
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
	reflection.Register(grpcSrv)

	// --- REST gateway ---
	// WithMetadata extracts the JWT from either the Authorization header or the
	// "token" httpOnly cookie and forwards it as gRPC metadata so the auth
	// interceptors can validate it.
	gwMux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{UseProtoNames: true},
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

	// --- Password auth handler ---
	passwordHandler := auth.NewPasswordHandler(jwtSigner, userStore)

	// --- HTTP mux ---
	httpMux := http.NewServeMux()
	passwordHandler.RegisterRoutes(httpMux) // POST /auth/register, POST /auth/login
	httpMux.Handle("/", gwMux)              // gRPC-gateway routes
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
			memory.NewTaskStore()
	}
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		log.Error("postgres connect", "err", err)
		os.Exit(1)
	}
	log.Info("using postgres store")
	log.Warn("service, postmortem, announcement, chat, sev-link, and task stores are in-memory — data will not persist across restarts (postgres implementations deferred)")
	return postgres.NewSEVStore(pool),
		postgres.NewAuditStore(pool),
		postgres.NewStatusHistoryStore(pool),
		postgres.NewUserStore(pool),
		postgres.NewRoleStore(pool),
		memory.NewServiceStore(),
		memory.NewPostmortemStore(),
		memory.NewAnnouncementStore(),
		memory.NewChatStore(),
		memory.NewSEVLinkStore(),
		memory.NewTaskStore()
}

// githubClientAdapter adapts the github.Client to the grpchandler.IssueClient interface.
type githubClientAdapter struct {
	c *github.Client
}

func (a *githubClientAdapter) GetIssue(ctx context.Context, owner, repo string, number int) (*grpchandler.GitHubIssue, error) {
	issue, err := a.c.GetIssue(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	return &grpchandler.GitHubIssue{
		Number:  issue.Number,
		Title:   issue.Title,
		Body:    issue.Body,
		State:   issue.State,
		HTMLURL: issue.HTMLURL,
	}, nil
}

func (a *githubClientAdapter) CreateIssue(ctx context.Context, owner, repo, title, body string) (*grpchandler.GitHubIssue, error) {
	issue, err := a.c.CreateIssue(ctx, owner, repo, title, body)
	if err != nil {
		return nil, err
	}
	return &grpchandler.GitHubIssue{
		Number:  issue.Number,
		Title:   issue.Title,
		Body:    issue.Body,
		State:   issue.State,
		HTMLURL: issue.HTMLURL,
	}, nil
}
