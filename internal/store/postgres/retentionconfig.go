package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.RetentionConfigStore = (*RetentionConfigStore)(nil)

// RetentionConfigStore implements store.RetentionConfigStore against
// PostgreSQL. Unlike the in-memory implementation, this one does not
// pre-seed rows for severity 1-4 — migrations/000002_schema.up.sql already
// does that once, at schema creation time.
type RetentionConfigStore struct {
	pool *pgxpool.Pool
}

// NewRetentionConfigStore returns a RetentionConfigStore backed by pool.
func NewRetentionConfigStore(pool *pgxpool.Pool) *RetentionConfigStore {
	return &RetentionConfigStore{pool: pool}
}

func (s *RetentionConfigStore) Get(ctx context.Context, severityLevel int16) (*store.RetentionConfig, error) {
	q := queries.New(s.pool)

	row, err := q.GetRetentionConfig(ctx, severityLevel)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres retention config: get: %w", err)
	}
	return mapRetentionConfigRow(row), nil
}

func (s *RetentionConfigStore) Upsert(ctx context.Context, cfg *store.RetentionConfig) error {
	q := queries.New(s.pool)

	id, err := q.UpsertRetentionConfig(ctx, queries.UpsertRetentionConfigParams{
		SeverityLevel: cfg.SeverityLevel,
		RetentionDays: int32(cfg.RetentionDays),
		HardDelete:    cfg.HardDelete,
	})
	if err != nil {
		return fmt.Errorf("postgres retention config: upsert: %w", err)
	}
	cfg.ID = id
	return nil
}

func (s *RetentionConfigStore) List(ctx context.Context) ([]*store.RetentionConfig, error) {
	q := queries.New(s.pool)

	rows, err := q.ListRetentionConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres retention config: list: %w", err)
	}
	out := make([]*store.RetentionConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapRetentionConfigRow(r))
	}
	return out, nil
}

func mapRetentionConfigRow(r queries.RetentionConfig) *store.RetentionConfig {
	return &store.RetentionConfig{
		ID:            r.ID,
		SeverityLevel: r.SeverityLevel,
		RetentionDays: int(r.RetentionDays),
		HardDelete:    r.HardDelete,
		CreatedAt:     r.CreatedAt.Time,
		UpdatedAt:     r.UpdatedAt.Time,
	}
}
