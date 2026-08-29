package grpc_test

import (
	"context"
	"testing"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

func TestRequestIDUnaryInterceptor_MintsIDWhenNoneSupplied(t *testing.T) {
	interceptor := grpchandler.RequestIDUnaryInterceptor()

	var gotID string
	var gotOK bool
	handler := func(ctx context.Context, req any) (any, error) {
		gotID, gotOK = telemetry.RequestIDFromContext(ctx)
		return "ok", nil
	}
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	if _, err := interceptor(context.Background(), nil, info, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotOK {
		t.Fatal("handler saw no request ID in ctx")
	}
	if gotID == "" {
		t.Error("minted request ID is empty")
	}
}

func TestRequestIDUnaryInterceptor_ReusesCallerSuppliedID(t *testing.T) {
	interceptor := grpchandler.RequestIDUnaryInterceptor()

	var gotID string
	handler := func(ctx context.Context, req any) (any, error) {
		gotID, _ = telemetry.RequestIDFromContext(ctx)
		return "ok", nil
	}
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	md := metadata.Pairs(grpchandler.RequestIDMetadataKey, "caller-supplied-id")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	if _, err := interceptor(ctx, nil, info, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotID != "caller-supplied-id" {
		t.Errorf("request ID = %q, want %q (caller-supplied value should be reused, not overwritten)", gotID, "caller-supplied-id")
	}
}

func TestRequestIDUnaryInterceptor_DifferentCallsGetDifferentIDs(t *testing.T) {
	interceptor := grpchandler.RequestIDUnaryInterceptor()

	var ids []string
	handler := func(ctx context.Context, req any) (any, error) {
		id, _ := telemetry.RequestIDFromContext(ctx)
		ids = append(ids, id)
		return "ok", nil
	}
	info := &grpclib.UnaryServerInfo{FullMethod: "/sevitout.v1.SEVService/GetSEV"}

	for range 2 {
		if _, err := interceptor(context.Background(), nil, info, handler); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ids[0] == ids[1] {
		t.Errorf("two independent calls minted the same request ID: %q", ids[0])
	}
}

func TestRequestIDStreamInterceptor_AttachesRequestID(t *testing.T) {
	interceptor := grpchandler.RequestIDStreamInterceptor()

	var gotID string
	var gotOK bool
	handler := func(srv any, ss grpclib.ServerStream) error {
		gotID, gotOK = telemetry.RequestIDFromContext(ss.Context())
		return nil
	}
	info := &grpclib.StreamServerInfo{FullMethod: "/sevitout.v1.AIService/StreamAction"}

	if err := interceptor(nil, &fakeServerStream{ctx: context.Background()}, info, handler); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotOK || gotID == "" {
		t.Error("stream handler saw no request ID in its stream's context")
	}
}

// fakeServerStream is a minimal grpc.ServerStream stand-in for exercising a
// stream interceptor without a real network connection.
type fakeServerStream struct {
	grpclib.ServerStream
	ctx context.Context
}

func (s *fakeServerStream) Context() context.Context { return s.ctx }
