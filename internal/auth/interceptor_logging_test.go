package auth_test

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// noopUnaryHandler stands in for a real RPC handler; the tests below only
// exercise rejections, so it must never actually be reached.
func noopUnaryHandler(_ context.Context, _ any) (any, error) {
	return "should not be called", nil
}

func TestUnaryInterceptor_RejectsAndLogs_MissingAuthorizationHeader(t *testing.T) {
	buf := withCapturedDefaultLog(t)
	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	interceptor := auth.UnaryInterceptor(signer, memory.NewUserStore())

	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	info := &grpc.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	if _, err := interceptor(ctx, nil, info, noopUnaryHandler); err == nil {
		t.Fatal("expected an error for a request with no authorization header")
	}

	fields := lastLogFields(t, buf)
	if fields["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", fields["level"])
	}
	if fields["msg"] != "rpc rejected: missing authorization header" {
		t.Errorf("msg = %v, want %q", fields["msg"], "rpc rejected: missing authorization header")
	}
	if fields["method"] != info.FullMethod {
		t.Errorf("method = %v, want %s", fields["method"], info.FullMethod)
	}
}

func TestUnaryInterceptor_RejectsAndLogs_InvalidToken(t *testing.T) {
	buf := withCapturedDefaultLog(t)
	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	interceptor := auth.UnaryInterceptor(signer, memory.NewUserStore())

	md := metadata.Pairs("authorization", "Bearer not-a-real-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpc.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	if _, err := interceptor(ctx, nil, info, noopUnaryHandler); err == nil {
		t.Fatal("expected an error for an invalid token")
	}

	fields := lastLogFields(t, buf)
	if fields["msg"] != "rpc rejected: invalid or expired token" {
		t.Errorf("msg = %v, want %q", fields["msg"], "rpc rejected: invalid or expired token")
	}
}

// TestUnaryInterceptor_RejectsAndLogs_IncludesRequestID guards the point of
// threading request_id through authenticate at all: a rejection is exactly
// the case where LoggingUnaryInterceptor never runs (see authenticate's doc
// comment), so if this interceptor didn't read the request ID back out of
// ctx itself, a rejected call would be the one kind of log line with no
// correlation ID on it.
func TestUnaryInterceptor_RejectsAndLogs_IncludesRequestID(t *testing.T) {
	buf := withCapturedDefaultLog(t)
	signer := auth.NewJWTSigner("test-secret-key-32-chars-long!!", 24)
	interceptor := auth.UnaryInterceptor(signer, memory.NewUserStore())

	ctx := telemetry.WithRequestID(context.Background(), "req-rejected-123")
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{})
	info := &grpc.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	if _, err := interceptor(ctx, nil, info, noopUnaryHandler); err == nil {
		t.Fatal("expected an error for a request with no authorization header")
	}

	fields := lastLogFields(t, buf)
	if fields["request_id"] != "req-rejected-123" {
		t.Errorf("request_id = %v, want req-rejected-123", fields["request_id"])
	}
}
