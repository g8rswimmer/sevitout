package grpc

import (
	"context"
	"log/slog"
	"time"

	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// LoggingUnaryInterceptor returns a gRPC unary interceptor that logs every
// RPC call: method, duration, resulting status code, and the caller's
// request ID and user ID when available. It must sit inside
// auth.UnaryInterceptor in the chain (cmd/server/main.go), not outside it —
// auth attaches *auth.UserContext to a new context.Context value that only
// propagates to interceptors nested inside it, so this interceptor can only
// read it back via auth.UserFromContext if auth ran first. The same is true
// of RequestIDUnaryInterceptor, which must be outermost of all three — see
// its doc comment. A request auth itself rejects never reaches here at all;
// auth.authenticate logs those rejections directly for that reason (still
// able to include the request ID, since auth runs inside
// RequestIDUnaryInterceptor too).
//
// Before calling handler, it binds a *slog.Logger — pre-enriched with
// request_id and user_id, when available — into ctx via telemetry.WithLogger,
// so handler code can retrieve it with telemetry.LoggerFromContext(ctx)
// instead of re-deriving those fields from auth.UserFromContext by hand at
// every call site.
//
// Every RPC handled by this server is also reachable over REST through the
// grpc-gateway (cmd/server/main.go's gwMux), which proxies each HTTP request
// into an actual loopback gRPC call — so this one interceptor covers both
// the native gRPC API and the REST surface built on top of it.
func LoggingUnaryInterceptor(log *slog.Logger) grpclib.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpclib.UnaryServerInfo, handler grpclib.UnaryHandler) (any, error) {
		start := time.Now()
		ctx = bindLogger(ctx, log)
		resp, err := handler(ctx, req)
		logRPC(ctx, info.FullMethod, time.Since(start), err)
		return resp, err
	}
}

// LoggingStreamInterceptor is LoggingUnaryInterceptor's counterpart for
// streaming RPCs (currently only WebSocket-adjacent services would use
// this, but it's wired up for parity with auth.StreamInterceptor).
func LoggingStreamInterceptor(log *slog.Logger) grpclib.StreamServerInterceptor {
	return func(srv any, ss grpclib.ServerStream, info *grpclib.StreamServerInfo, handler grpclib.StreamHandler) error {
		start := time.Now()
		ctx := bindLogger(ss.Context(), log)
		err := handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
		logRPC(ctx, info.FullMethod, time.Since(start), err)
		return err
	}
}

// bindLogger returns a copy of ctx carrying a *slog.Logger derived from log
// via log.With(...), pre-bound with request_id (when
// RequestIDUnaryInterceptor/RequestIDStreamInterceptor ran first in the
// chain) and user_id (when auth.UnaryInterceptor/StreamInterceptor ran
// first) — so every log line written through the bound logger for the rest
// of this request's lifetime carries both without repeating the lookup.
func bindLogger(ctx context.Context, log *slog.Logger) context.Context {
	var attrs []any
	if reqID, ok := telemetry.RequestIDFromContext(ctx); ok {
		attrs = append(attrs, "request_id", reqID)
	}
	if uc, ok := auth.UserFromContext(ctx); ok {
		attrs = append(attrs, "user_id", uc.UserID)
	}
	if len(attrs) > 0 {
		log = log.With(attrs...)
	}
	return telemetry.WithLogger(ctx, log)
}

// logRPC logs at a level chosen by the resulting gRPC status code: OK is
// Info, an expected client-caused code (NotFound, InvalidArgument,
// PermissionDenied, Unauthenticated, AlreadyExists, ...) is Warn so it
// doesn't read as a server bug, and anything else (Internal, Unknown, a
// non-status error) is Error since those are the ones worth paging on. It
// logs through telemetry.LoggerFromContext(ctx) — the logger bindLogger
// attached above — so method/duration/code join whatever request_id/user_id
// fields are already bound to it.
func logRPC(ctx context.Context, method string, dur time.Duration, err error) {
	log := telemetry.LoggerFromContext(ctx)
	attrs := []any{"method", method, "duration_ms", dur.Milliseconds()}

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
