package grpc

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

// internalError logs err at Error level (via telemetry.LoggerFromContext(ctx)
// — the request-scoped logger LoggingUnaryInterceptor binds into ctx, so
// this line carries the same request_id/user_id every other log line for
// this call does) and returns codes.Internal wrapping msg.
//
// msg is deliberately the only thing that crosses the wire to the caller —
// err's detail is never appropriate to hand back to an API client, but
// discarding it entirely (the pattern this replaces:
// status.Error(codes.Internal, msg) with err never referenced again) meant
// LoggingUnaryInterceptor's own "rpc failed" line only ever saw the already-
// generic msg too, so a real failure (a DB outage, a driver error) was
// nearly invisible in the logs beyond code=Internal. This is the one place
// that fixes that, for every call site that adopts it.
func internalError(ctx context.Context, msg string, err error) error {
	telemetry.LoggerFromContext(ctx).ErrorContext(ctx, msg, "err", err)
	return status.Error(codes.Internal, msg)
}
