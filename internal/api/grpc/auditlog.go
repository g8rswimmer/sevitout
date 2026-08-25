package grpc

import (
	"context"
	"log/slog"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// auditAppendBestEffort appends entry to the audit log, logging (rather than
// returning) any failure. Every RPC handler treats the audit trail as
// best-effort — a failing audit write must never fail the mutation it's
// attached to — but silently discarding the error defeats the point of an
// append-only compliance log, so this at minimum surfaces it in the
// structured logs. Mirrors the pattern already used for the sensitive-flip
// auto-grant in SEVServer.UpdateSEV.
func auditAppendBestEffort(ctx context.Context, audit store.AuditStore, entry *store.AuditEntry) {
	if err := audit.Append(ctx, entry); err != nil {
		slog.ErrorContext(ctx, "audit append failed", "sev_id", entry.SEVID, "action", entry.Action, "err", err)
	}
}
