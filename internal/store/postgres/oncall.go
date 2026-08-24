package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.OnCallStore = (*OnCallStore)(nil)

// OnCallStore implements store.OnCallStore against PostgreSQL.
type OnCallStore struct {
	pool *pgxpool.Pool
}

// NewOnCallStore returns an OnCallStore backed by pool.
func NewOnCallStore(pool *pgxpool.Pool) *OnCallStore {
	return &OnCallStore{pool: pool}
}

func (s *OnCallStore) Create(ctx context.Context, r *store.OnCallRotation) error {
	q := queries.New(s.pool)

	id, err := q.InsertOnCallRotation(ctx, queries.InsertOnCallRotationParams{
		Name:                r.Name,
		ServiceID:           r.ServiceID,
		PagerdutyScheduleID: r.PagerDutyScheduleID,
		ManualUserID:        r.ManualUserID,
		ManualDisplayName:   r.ManualDisplayName,
		OverrideStart:       timeToDB(r.OverrideStart),
		OverrideEnd:         timeToDB(r.OverrideEnd),
		CreatedAt:           pgtype.Timestamptz{Time: r.CreatedAt.UTC(), Valid: true},
		UpdatedAt:           pgtype.Timestamptz{Time: r.UpdatedAt.UTC(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("postgres oncall: insert: %w", err)
	}
	r.ID = id
	return nil
}

func (s *OnCallStore) Get(ctx context.Context, id int64) (*store.OnCallRotation, error) {
	q := queries.New(s.pool)

	row, err := q.GetOnCallRotation(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres oncall: get: %w", err)
	}
	return mapOnCallRow(row), nil
}

func (s *OnCallStore) Update(ctx context.Context, r *store.OnCallRotation) error {
	q := queries.New(s.pool)

	if _, err := q.GetOnCallRotation(ctx, r.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres oncall: pre-update get: %w", err)
	}

	if err := q.UpdateOnCallRotation(ctx, queries.UpdateOnCallRotationParams{
		ID:                  r.ID,
		Name:                r.Name,
		ServiceID:           r.ServiceID,
		PagerdutyScheduleID: r.PagerDutyScheduleID,
		ManualUserID:        r.ManualUserID,
		ManualDisplayName:   r.ManualDisplayName,
		OverrideStart:       timeToDB(r.OverrideStart),
		OverrideEnd:         timeToDB(r.OverrideEnd),
		UpdatedAt:           pgtype.Timestamptz{Time: r.UpdatedAt.UTC(), Valid: true},
	}); err != nil {
		return fmt.Errorf("postgres oncall: update: %w", err)
	}
	return nil
}

func (s *OnCallStore) Delete(ctx context.Context, id int64) error {
	q := queries.New(s.pool)

	if _, err := q.GetOnCallRotation(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres oncall: pre-delete get: %w", err)
	}
	if err := q.DeleteOnCallRotation(ctx, id); err != nil {
		return fmt.Errorf("postgres oncall: delete: %w", err)
	}
	return nil
}

func (s *OnCallStore) List(ctx context.Context) ([]*store.OnCallRotation, error) {
	q := queries.New(s.pool)

	rows, err := q.ListOnCallRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres oncall: list: %w", err)
	}
	out := make([]*store.OnCallRotation, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapOnCallRow(r))
	}
	return out, nil
}

// GetCurrentOnCall delegates the "prefer an active manual override, else
// fall back to a normal rotation" logic entirely to
// GetCurrentOnCallForService's SQL (internal/store/sql/oncall.sql) — see
// memory.OnCallStore.GetCurrentOnCall's doc comment for the same semantics
// implemented in Go for the in-memory store.
func (s *OnCallStore) GetCurrentOnCall(ctx context.Context, serviceID string) (*store.OnCallRotation, error) {
	q := queries.New(s.pool)

	row, err := q.GetCurrentOnCallForService(ctx, &serviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres oncall: get current: %w", err)
	}
	return mapOnCallRow(row), nil
}

func mapOnCallRow(r queries.OncallRotation) *store.OnCallRotation {
	return &store.OnCallRotation{
		ID:                  r.ID,
		Name:                r.Name,
		ServiceID:           r.ServiceID,
		PagerDutyScheduleID: r.PagerdutyScheduleID,
		ManualUserID:        r.ManualUserID,
		ManualDisplayName:   r.ManualDisplayName,
		OverrideStart:       timeFromDB(r.OverrideStart),
		OverrideEnd:         timeFromDB(r.OverrideEnd),
		CreatedAt:           r.CreatedAt.Time,
		UpdatedAt:           r.UpdatedAt.Time,
	}
}
