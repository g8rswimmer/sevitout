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
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

//go:embed openapi/openapi.json
var openAPISpec []byte

func main() {
	ctx := context.Background()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	sevStore, auditStore, historyStore, userStore := buildStores(ctx, log)

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

	// --- gRPC server with auth interceptors ---
	sevServer := grpchandler.NewSEVServer(sevStore, auditStore, historyStore)
	auditServer := grpchandler.NewAuditServer(auditStore)
	authServer := grpchandler.NewAuthServer(userStore)

	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(auth.UnaryInterceptor(jwtSigner)),
		grpc.StreamInterceptor(auth.StreamInterceptor(jwtSigner)),
	)
	pb.RegisterSEVServiceServer(grpcSrv, sevServer)
	pb.RegisterAuditServiceServer(grpcSrv, auditServer)
	pb.RegisterAuthServiceServer(grpcSrv, authServer)
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
	// RegisterXxxHandlerFromEndpoint dials the gRPC server over loopback so that
	// REST requests routed through the gateway still run through the gRPC
	// interceptors (JWT validation, RBAC). HandlerServer bypasses interceptors
	// and must not be used here.
	const addr = ":8080"
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := pb.RegisterSEVServiceHandlerFromEndpoint(ctx, gwMux, addr, dialOpts); err != nil {
		log.Error("register sev gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterAuditServiceHandlerFromEndpoint(ctx, gwMux, addr, dialOpts); err != nil {
		log.Error("register audit gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterAuthServiceHandlerFromEndpoint(ctx, gwMux, addr, dialOpts); err != nil {
		log.Error("register auth gateway", "err", err)
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

func buildStores(ctx context.Context, log *slog.Logger) (store.SEVStore, store.AuditStore, store.StatusHistoryStore, store.UserStore) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Warn("DATABASE_URL not set — using in-memory store (data will not persist)")
		return memory.NewSEVStore(), memory.NewAuditStore(), memory.NewStatusHistoryStore(), memory.NewUserStore()
	}
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		log.Error("postgres connect", "err", err)
		os.Exit(1)
	}
	log.Info("using postgres store")
	return postgres.NewSEVStore(pool), postgres.NewAuditStore(pool), postgres.NewStatusHistoryStore(pool), postgres.NewUserStore(pool)
}
