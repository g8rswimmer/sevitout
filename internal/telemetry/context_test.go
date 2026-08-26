package telemetry_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/g8rswimmer/sevitout/internal/telemetry"
)

func TestRequestID_RoundTrip(t *testing.T) {
	ctx := telemetry.WithRequestID(context.Background(), "req-123")

	id, ok := telemetry.RequestIDFromContext(ctx)
	if !ok {
		t.Fatal("RequestIDFromContext ok = false, want true")
	}
	if id != "req-123" {
		t.Errorf("RequestIDFromContext id = %q, want %q", id, "req-123")
	}
}

func TestRequestID_AbsentFromContext(t *testing.T) {
	if _, ok := telemetry.RequestIDFromContext(context.Background()); ok {
		t.Error("RequestIDFromContext ok = true for a context with no request ID attached, want false")
	}
}

func TestRequestID_EmptyStringTreatedAsAbsent(t *testing.T) {
	ctx := telemetry.WithRequestID(context.Background(), "")
	if _, ok := telemetry.RequestIDFromContext(ctx); ok {
		t.Error("RequestIDFromContext ok = true for an empty request ID, want false")
	}
}

func TestLogger_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	ctx := telemetry.WithLogger(context.Background(), log)

	got := telemetry.LoggerFromContext(ctx)
	got.Info("hello")
	if buf.Len() == 0 {
		t.Fatal("LoggerFromContext returned a logger that didn't write to buf — got the wrong logger back")
	}
}

func TestLogger_FallsBackToDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	got := telemetry.LoggerFromContext(context.Background())
	got.Info("hello")
	if buf.Len() == 0 {
		t.Fatal("LoggerFromContext(ctx with no bound logger) didn't fall back to slog.Default()")
	}
}
