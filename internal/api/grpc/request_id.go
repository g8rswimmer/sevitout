package grpc

import (
	"context"

	"github.com/google/uuid"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// RequestIDMetadataKey is the gRPC metadata key RequestIDUnaryInterceptor/
// RequestIDStreamInterceptor check for a caller-supplied request ID.
// cmd/server/main.go's grpc-gateway bridges an incoming HTTP request's
// X-Request-Id header into this same metadata key, so one correlation ID
// survives the REST→loopback-gRPC hop instead of a fresh one being minted
// at the gateway.
const RequestIDMetadataKey = "x-request-id"

// RequestIDUnaryInterceptor returns a gRPC unary interceptor that attaches a
// request ID to ctx via telemetry.WithRequestID — reusing one supplied by
// the caller via the RequestIDMetadataKey gRPC metadata entry when present,
// or minting a fresh UUID otherwise.
//
// It must be the outermost interceptor in the chain (cmd/server/main.go) —
// ahead of auth.UnaryInterceptor, which is itself ahead of
// LoggingUnaryInterceptor (see that file's doc comment for why interceptor
// order matters: context.WithValue only propagates to interceptors nested
// inside the one that set it). Putting request-ID generation outermost
// means even an auth rejection — logged directly by auth.authenticate,
// since a rejected call never reaches LoggingUnaryInterceptor — carries a
// request ID.
func RequestIDUnaryInterceptor() grpclib.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpclib.UnaryServerInfo, handler grpclib.UnaryHandler) (any, error) {
		ctx = telemetry.WithRequestID(ctx, requestID(ctx))
		return handler(ctx, req)
	}
}

// RequestIDStreamInterceptor is RequestIDUnaryInterceptor's counterpart for
// streaming RPCs; see wrappedStream for why a stream interceptor needs to
// wrap ServerStream rather than just pass a new ctx to handler directly.
func RequestIDStreamInterceptor() grpclib.StreamServerInterceptor {
	return func(srv any, ss grpclib.ServerStream, info *grpclib.StreamServerInfo, handler grpclib.StreamHandler) error {
		ctx := telemetry.WithRequestID(ss.Context(), requestID(ss.Context()))
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: ctx})
	}
}

// requestID returns the caller-supplied request ID from ctx's incoming gRPC
// metadata (RequestIDMetadataKey), or a freshly generated UUID if none was
// supplied.
func requestID(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get(RequestIDMetadataKey); len(values) > 0 && values[0] != "" {
			return values[0]
		}
	}
	return uuid.NewString()
}

// wrappedStream replaces the context on a gRPC ServerStream — the streaming
// counterpart of a unary interceptor's ability to just pass a new ctx to
// handler directly. Shared by RequestIDStreamInterceptor and
// LoggingStreamInterceptor (both in this package); mirrors
// internal/auth/interceptor.go's own private copy of the same pattern.
type wrappedStream struct {
	grpclib.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
