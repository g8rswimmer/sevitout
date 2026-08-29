package grpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/prometheus/client_golang/prometheus/testutil"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// lastLogLine returns the fields of the last JSON log line written to buf,
// or fails the test if buf holds no valid JSON line.
func lastLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		t.Fatalf("no log output, buf=%q", buf.String())
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &fields); err != nil {
		t.Fatalf("log line is not valid JSON: %v, line=%q", err, lines[len(lines)-1])
	}
	return fields
}

func TestLoggingUnaryInterceptor_Success_LogsInfo(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	interceptor := grpchandler.LoggingUnaryInterceptor(log)

	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	if _, err := interceptor(context.Background(), nil, info, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fields := lastLogLine(t, &buf)
	if fields["level"] != "INFO" {
		t.Errorf("level = %v, want INFO", fields["level"])
	}
	if fields["method"] != info.FullMethod {
		t.Errorf("method = %v, want %s", fields["method"], info.FullMethod)
	}
	if fields["code"] != codes.OK.String() {
		t.Errorf("code = %v, want %s", fields["code"], codes.OK.String())
	}
}

func TestLoggingUnaryInterceptor_ExpectedClientError_LogsWarn(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	interceptor := grpchandler.LoggingUnaryInterceptor(log)

	wantErr := status.Error(codes.NotFound, "sev not found")
	handler := func(ctx context.Context, req any) (any, error) { return nil, wantErr }
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	_, err := interceptor(context.Background(), nil, info, handler)
	if !errors.Is(err, wantErr) {
		t.Fatalf("interceptor changed the returned error: got %v, want %v", err, wantErr)
	}

	fields := lastLogLine(t, &buf)
	if fields["level"] != "WARN" {
		t.Errorf("level = %v, want WARN (NotFound is a client error, not a bug)", fields["level"])
	}
	if fields["code"] != codes.NotFound.String() {
		t.Errorf("code = %v, want %s", fields["code"], codes.NotFound.String())
	}
}

func TestLoggingUnaryInterceptor_InternalError_LogsError(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	interceptor := grpchandler.LoggingUnaryInterceptor(log)

	handler := func(ctx context.Context, req any) (any, error) {
		return nil, status.Error(codes.Internal, "store blew up")
	}
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/CreateSEV"}

	if _, err := interceptor(context.Background(), nil, info, handler); err == nil {
		t.Fatal("expected an error")
	}

	fields := lastLogLine(t, &buf)
	if fields["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR (Internal is a server bug worth paging on)", fields["level"])
	}
}

func TestLoggingUnaryInterceptor_IncludesAuthenticatedUserID(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	interceptor := grpchandler.LoggingUnaryInterceptor(log)

	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-42", OrgRole: store.OrgRoleAdmin})
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	if _, err := interceptor(ctx, nil, info, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fields := lastLogLine(t, &buf)
	if fields["user_id"] != "user-42" {
		t.Errorf("user_id = %v, want user-42", fields["user_id"])
	}
}

func TestLoggingUnaryInterceptor_UnauthenticatedRequest_LogsWithoutUserID(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	interceptor := grpchandler.LoggingUnaryInterceptor(log)

	wantErr := status.Error(codes.Unauthenticated, "missing authorization header")
	handler := func(ctx context.Context, req any) (any, error) { return nil, wantErr }
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	// No auth.WithUser in ctx: in the real chain (auth.UnaryInterceptor
	// wraps this one — see cmd/server/main.go) a request auth itself
	// rejects never reaches here at all, so this exercises the interceptor's
	// own handling of "no user attached" rather than an auth rejection.
	if _, err := interceptor(context.Background(), nil, info, handler); !errors.Is(err, wantErr) {
		t.Fatalf("interceptor changed the returned error: got %v, want %v", err, wantErr)
	}

	fields := lastLogLine(t, &buf)
	if _, ok := fields["user_id"]; ok {
		t.Errorf("user_id should be absent for an unauthenticated call, got %v", fields["user_id"])
	}
	if fields["code"] != codes.Unauthenticated.String() {
		t.Errorf("code = %v, want %s", fields["code"], codes.Unauthenticated.String())
	}
}

// TestLoggingUnaryInterceptor_RecordsRPCMetrics uses a method name unique to
// this test (not shared with any other test in this package's test binary)
// so its before/after Prometheus assertions can't be perturbed by another
// test incrementing the same method+code label combination.
func TestLoggingUnaryInterceptor_RecordsRPCMetrics(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	interceptor := grpchandler.LoggingUnaryInterceptor(log)

	const method = "/test.Metrics/RecordsRPCMetrics"
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpclib.UnaryServerInfo{FullMethod: method}

	beforeCount := testutil.ToFloat64(telemetry.RPCRequestsTotal.WithLabelValues(method, codes.OK.String()))
	beforeSamples := testutil.CollectAndCount(telemetry.RPCDurationSeconds)

	if _, err := interceptor(context.Background(), nil, info, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := testutil.ToFloat64(telemetry.RPCRequestsTotal.WithLabelValues(method, codes.OK.String())); got != beforeCount+1 {
		t.Errorf("RPCRequestsTotal[%s,OK] = %v, want %v", method, got, beforeCount+1)
	}
	if got := testutil.CollectAndCount(telemetry.RPCDurationSeconds); got != beforeSamples+1 {
		t.Errorf("RPCDurationSeconds sample count = %d, want %d", got, beforeSamples+1)
	}
}

// TestLoggingUnaryInterceptor_IncludesRequestID mirrors
// TestLoggingUnaryInterceptor_IncludesAuthenticatedUserID for the request-ID
// half of bindLogger — a ctx carrying a request ID (as
// RequestIDUnaryInterceptor would attach it, if chained ahead of this one)
// should show up on the logged line.
func TestLoggingUnaryInterceptor_IncludesRequestID(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	interceptor := grpchandler.LoggingUnaryInterceptor(log)

	ctx := telemetry.WithRequestID(context.Background(), "req-42")
	handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	if _, err := interceptor(ctx, nil, info, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fields := lastLogLine(t, &buf)
	if fields["request_id"] != "req-42" {
		t.Errorf("request_id = %v, want req-42", fields["request_id"])
	}
}

// TestLoggingUnaryInterceptor_HandlerCanRetrieveBoundLogger exercises the
// core point of bindLogger: handler code should be able to pull a
// pre-enriched logger out of ctx via telemetry.LoggerFromContext instead of
// re-deriving request_id/user_id by hand.
func TestLoggingUnaryInterceptor_HandlerCanRetrieveBoundLogger(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	interceptor := grpchandler.LoggingUnaryInterceptor(log)

	ctx := telemetry.WithRequestID(context.Background(), "req-99")
	ctx = auth.WithUser(ctx, &auth.UserContext{UserID: "user-7", OrgRole: store.OrgRoleAdmin})
	handler := func(ctx context.Context, req any) (any, error) {
		telemetry.LoggerFromContext(ctx).Info("handler-internal event")
		return "ok", nil
	}
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	if _, err := interceptor(ctx, nil, info, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2 (handler's own + the interceptor's rpc-completed line): %q", len(lines), buf.String())
	}
	var handlerLine map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &handlerLine); err != nil {
		t.Fatalf("handler's log line is not valid JSON: %v", err)
	}
	if handlerLine["msg"] != "handler-internal event" {
		t.Fatalf("first log line msg = %v, want %q", handlerLine["msg"], "handler-internal event")
	}
	if handlerLine["request_id"] != "req-99" {
		t.Errorf("handler's logger: request_id = %v, want req-99 (bound logger wasn't retrieved)", handlerLine["request_id"])
	}
	if handlerLine["user_id"] != "user-7" {
		t.Errorf("handler's logger: user_id = %v, want user-7 (bound logger wasn't retrieved)", handlerLine["user_id"])
	}
}

// TestLoggingAndAuthAndRequestIDInterceptorsChained_EndToEnd guards the
// exact three-deep ordering this chain depends on: RequestIDUnaryInterceptor
// outermost, then auth.UnaryInterceptor, then LoggingUnaryInterceptor
// innermost. auth.UnaryInterceptor attaches *auth.UserContext to a *new*
// context.Context value (context.Context is immutable), so
// LoggingUnaryInterceptor can only observe it — and the request ID
// RequestIDUnaryInterceptor attached — if both ran before it in the chain.
// Swap the order in cmd/server/main.go's grpc.ChainUnaryInterceptor and this
// test catches it by asserting request_id and user_id both actually appear
// on a real authenticated call.
func TestLoggingAndAuthAndRequestIDInterceptorsChained_EndToEnd(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	users := memory.NewUserStore()
	now := time.Now()
	seedUser := &store.User{
		ID: "user-abc", Email: "alice@example.com", Name: "Alice",
		OrgRole: store.OrgRoleAdmin, Active: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := users.Create(context.Background(), seedUser); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	srv := grpclib.NewServer(
		// Mirrors cmd/server/main.go's ordering exactly: request-ID
		// outermost, then auth, then logging innermost, so each layer's
		// enriched context reaches the ones nested inside it.
		grpclib.ChainUnaryInterceptor(
			grpchandler.RequestIDUnaryInterceptor(),
			auth.UnaryInterceptor(signer, users),
			grpchandler.LoggingUnaryInterceptor(log),
		),
	)
	pb.RegisterAuthServiceServer(srv, grpchandler.NewAuthServer(users))

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpclib.NewClient(lis.Addr().String(), grpclib.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	client := pb.NewAuthServiceClient(conn)

	token, err := signer.Sign(seedUser.ID, seedUser.Email, string(seedUser.OrgRole))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	md := metadata.Pairs("authorization", "Bearer "+token, grpchandler.RequestIDMetadataKey, "client-supplied-req-id")
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	if _, err := client.WhoAmI(ctx, &pb.WhoAmIRequest{}); err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}

	fields := lastLogLine(t, &buf)
	if fields["msg"] != "rpc completed" {
		t.Fatalf("msg = %v, want %q (log=%s)", fields["msg"], "rpc completed", buf.String())
	}
	if fields["user_id"] != seedUser.ID {
		t.Errorf("user_id = %v, want %s — the logging interceptor isn't seeing auth's enriched context", fields["user_id"], seedUser.ID)
	}
	if fields["request_id"] != "client-supplied-req-id" {
		t.Errorf("request_id = %v, want %s — the logging interceptor isn't seeing the request-ID interceptor's context, or the client-supplied ID wasn't reused", fields["request_id"], "client-supplied-req-id")
	}
}
