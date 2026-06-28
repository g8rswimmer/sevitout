package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.AuditStore = (*AuditStore)(nil)

// AuditStore implements store.AuditStore against PostgreSQL.
type AuditStore struct {
	pool *pgxpool.Pool
}

// NewAuditStore returns an AuditStore backed by pool.
func NewAuditStore(pool *pgxpool.Pool) *AuditStore {
	return &AuditStore{pool: pool}
}

func (a *AuditStore) Append(ctx context.Context, entry *store.AuditEntry) error {
	q := queries.New(a.pool)

	id, err := q.AppendAuditEntry(ctx, queries.AppendAuditEntryParams{
		SevID:     entry.SEVID,
		UserID:    entry.UserID,
		Action:    entry.Action,
		FieldName: entry.FieldName,
		OldValue:  entry.OldValue,
		NewValue:  entry.NewValue,
		CreatedAt: pgtype.Timestamptz{Time: entry.CreatedAt.UTC(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("postgres audit: append: %w", err)
	}
	entry.ID = id
	return nil
}

func (a *AuditStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.AuditEntry, error) {
	q := queries.New(a.pool)

	rows, err := q.ListAuditEntriesBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres audit: list: %w", err)
	}

	result := make([]*store.AuditEntry, 0, len(rows))
	for _, r := range rows {
		result = append(result, &store.AuditEntry{
			ID:        r.ID,
			SEVID:     r.SevID,
			UserID:    r.UserID,
			Action:    r.Action,
			FieldName: r.FieldName,
			OldValue:  r.OldValue,
			NewValue:  r.NewValue,
			CreatedAt: r.CreatedAt.Time,
		})
	}
	return result, nil
}
