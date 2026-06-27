package main

import (
	"context"
	_ "embed"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/encoding/protojson"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/store/postgres"
)

//go:embed openapi/openapi.json
var openAPISpec []byte

func main() {
	ctx := context.Background()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	sevStore, auditStore, historyStore := buildStores(ctx, log)

	// --- gRPC server ---
	grpcSrv := grpc.NewServer()
	pb.RegisterSEVServiceServer(grpcSrv, grpchandler.NewSEVServer(sevStore, auditStore, historyStore))
	pb.RegisterAuditServiceServer(grpcSrv, grpchandler.NewAuditServer(auditStore))
	reflection.Register(grpcSrv)

	// --- REST gateway (dials back to the gRPC server on the same port) ---
	// UseProtoNames emits snake_case field names matching the proto definitions.
	gwMux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{UseProtoNames: true},
		}),
	)
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	const addr = ":8080"
	if err := pb.RegisterSEVServiceHandlerFromEndpoint(ctx, gwMux, "localhost"+addr, dialOpts); err != nil {
		log.Error("register sev gateway", "err", err)
		os.Exit(1)
	}
	if err := pb.RegisterAuditServiceHandlerFromEndpoint(ctx, gwMux, "localhost"+addr, dialOpts); err != nil {
		log.Error("register audit gateway", "err", err)
		os.Exit(1)
	}

	httpMux := http.NewServeMux()
	httpMux.Handle("/", gwMux)
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

func buildStores(ctx context.Context, log *slog.Logger) (store.SEVStore, store.AuditStore, store.StatusHistoryStore) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Warn("DATABASE_URL not set — using in-memory store (data will not persist)")
		return memory.NewSEVStore(), memory.NewAuditStore(), memory.NewStatusHistoryStore()
	}
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		log.Error("postgres connect", "err", err)
		os.Exit(1)
	}
	log.Info("using postgres store")
	return postgres.NewSEVStore(pool), postgres.NewAuditStore(pool), postgres.NewStatusHistoryStore(pool)
}
