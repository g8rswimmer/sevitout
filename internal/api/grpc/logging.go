package grpc

import (
	"context"
	"log/slog"
	"time"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/auth"
)

// LoggingUnaryInterceptor returns a gRPC unary interceptor that logs every
// RPC call: method, duration, resulting status code, and the caller's user
// ID when authentication has already run. It must sit inside
// auth.UnaryInterceptor in the chain (cmd/server/main.go), not outside it —
// auth attaches *auth.UserContext to a new context.Context value that only
// propagates to interceptors nested inside it, so this interceptor can only
// read it back via auth.UserFromContext if auth ran first. A request auth
// itself rejects never reaches here at all; auth.authenticate logs those
// rejections directly for that reason.
//
// Every RPC handled by this server is also reachable over REST through the
// grpc-gateway (cmd/server/main.go's gwMux), which proxies each HTTP request
// into an actual loopback gRPC call — so this one interceptor covers both
// the native gRPC API and the REST surface built on top of it.
func LoggingUnaryInterceptor(log *slog.Logger) grpclib.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpclib.UnaryServerInfo, handler grpclib.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logRPC(log, ctx, info.FullMethod, time.Since(start), err)
		return resp, err
	}
}

// LoggingStreamInterceptor is LoggingUnaryInterceptor's counterpart for
// streaming RPCs (currently only WebSocket-adjacent services would use
// this, but it's wired up for parity with auth.StreamInterceptor).
func LoggingStreamInterceptor(log *slog.Logger) grpclib.StreamServerInterceptor {
	return func(srv any, ss grpclib.ServerStream, info *grpclib.StreamServerInfo, handler grpclib.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		logRPC(log, ss.Context(), info.FullMethod, time.Since(start), err)
		return err
	}
}

// logRPC logs at a level chosen by the resulting gRPC status code: OK is
// Info, an expected client-caused code (NotFound, InvalidArgument,
// PermissionDenied, Unauthenticated, AlreadyExists, ...) is Warn so it
// doesn't read as a server bug, and anything else (Internal, Unknown, a
// non-status error) is Error since those are the ones worth paging on.
func logRPC(log *slog.Logger, ctx context.Context, method string, dur time.Duration, err error) {
	attrs := []any{"method", method, "duration_ms", dur.Milliseconds()}
	if uc, ok := auth.UserFromContext(ctx); ok {
		attrs = append(attrs, "user_id", uc.UserID)
	}

	if err == nil {
		log.InfoContext(ctx, "rpc completed", append(attrs, "code", codes.OK.String())...)
		return
	}

	code := status.Code(err)
	attrs = append(attrs, "code", code.String(), "err", err)
	switch code {
	case codes.Internal, codes.Unknown, codes.DataLoss, codes.Unavailable:
		log.ErrorContext(ctx, "rpc failed", attrs...)
	default:
		log.WarnContext(ctx, "rpc failed", attrs...)
	}
}
