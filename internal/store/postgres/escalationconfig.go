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

var _ store.EscalationConfigStore = (*EscalationConfigStore)(nil)

// EscalationConfigStore implements store.EscalationConfigStore against
// PostgreSQL. Unlike the in-memory implementation, this one does not
// pre-seed rows for severity 1-4 — migrations/000020_notification_config.up.sql
// already does that once, at schema migration time (mirroring
// retention_config's seed precedent).
type EscalationConfigStore struct {
	pool *pgxpool.Pool
}

// NewEscalationConfigStore returns an EscalationConfigStore backed by pool.
func NewEscalationConfigStore(pool *pgxpool.Pool) *EscalationConfigStore {
	return &EscalationConfigStore{pool: pool}
}

func (s *EscalationConfigStore) Get(ctx context.Context, severityLevel int16) (*store.EscalationConfig, error) {
	q := queries.New(s.pool)

	row, err := q.GetEscalationConfig(ctx, severityLevel)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres escalation config: get: %w", err)
	}
	return mapEscalationConfigRow(row), nil
}

func (s *EscalationConfigStore) Upsert(ctx context.Context, cfg *store.EscalationConfig) error {
	q := queries.New(s.pool)

	row, err := q.UpsertEscalationConfig(ctx, queries.UpsertEscalationConfigParams{
		SeverityLevel:    cfg.SeverityLevel,
		ThresholdMinutes: cfg.ThresholdMinutes,
		Enabled:          cfg.Enabled,
	})
	if err != nil {
		return fmt.Errorf("postgres escalation config: upsert: %w", err)
	}
	cfg.ID = row.ID
	cfg.CreatedAt = row.CreatedAt.Time
	cfg.UpdatedAt = row.UpdatedAt.Time
	return nil
}

func (s *EscalationConfigStore) List(ctx context.Context) ([]*store.EscalationConfig, error) {
	q := queries.New(s.pool)

	rows, err := q.ListEscalationConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres escalation config: list: %w", err)
	}
	out := make([]*store.EscalationConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapEscalationConfigRow(r))
	}
	return out, nil
}

func mapEscalationConfigRow(r queries.EscalationConfig) *store.EscalationConfig {
	return &store.EscalationConfig{
		ID:               r.ID,
		SeverityLevel:    r.SeverityLevel,
		ThresholdMinutes: r.ThresholdMinutes,
		Enabled:          r.Enabled,
		CreatedAt:        r.CreatedAt.Time,
		UpdatedAt:        r.UpdatedAt.Time,
	}
}
