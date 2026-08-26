// Package telemetry provides cross-cutting, per-request infrastructure — a
// correlation ID and a pre-bound structured logger — carried through
// context.Context. It's used by the gRPC interceptor chain
// (internal/api/grpc), internal/auth's own rejection logging, and the
// standalone net/http handlers wired up in cmd/server/main.go alike.
//
// It has no dependency on internal/auth or internal/store: a request ID and
// a bound logger are unrelated to authentication, so every caller that
// wants either should be able to import this package without pulling in
// auth or the store layer too.
package telemetry

import (
	"context"
	"log/slog"
)

type contextKey int

const (
	requestIDKey contextKey = iota
	loggerKey
)

// WithRequestID returns a copy of ctx carrying id as the request's
// correlation ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext retrieves the request ID attached to ctx by
// WithRequestID. The second return value is false when none has been
// attached, or it was attached as an empty string.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey).(string)
	return id, ok && id != ""
}

// WithLogger returns a copy of ctx carrying log as the request's bound
// logger — typically log.With("request_id", ..., "user_id", ...), attached
// once (by internal/api/grpc.LoggingUnaryInterceptor/LoggingStreamInterceptor,
// or by cmd/server/main.go's loggingMiddleware for the standalone net/http
// handlers) so downstream code can retrieve an already-enriched logger via
// LoggerFromContext instead of re-deriving request/user identifiers by hand
// at every call site.
func WithLogger(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, log)
}

// LoggerFromContext retrieves the logger attached to ctx by WithLogger, or
// slog.Default() if none is attached — e.g. inside internal/ai.Dispatcher's
// background worker pool, which runs against the process-lifetime context
// rather than any single request's, so it never has a bound logger to
// retrieve. slog.Default() is still correctly configured (JSON, LOG_LEVEL)
// since cmd/server/main.go calls slog.SetDefault at startup, so this
// fallback degrades gracefully rather than silently dropping output.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(loggerKey).(*slog.Logger); ok && log != nil {
		return log
	}
	return slog.Default()
}
