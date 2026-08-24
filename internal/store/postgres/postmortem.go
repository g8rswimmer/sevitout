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

var _ store.PostmortemStore = (*PostmortemStore)(nil)

// PostmortemStore implements store.PostmortemStore against PostgreSQL.
type PostmortemStore struct {
	pool *pgxpool.Pool
}

// NewPostmortemStore returns a PostmortemStore backed by pool.
func NewPostmortemStore(pool *pgxpool.Pool) *PostmortemStore {
	return &PostmortemStore{pool: pool}
}

func (s *PostmortemStore) Create(ctx context.Context, pm *store.Postmortem) error {
	q := queries.New(s.pool)

	id, err := q.InsertPostmortem(ctx, queries.InsertPostmortemParams{
		SevID:     pm.SEVID,
		Status:    string(pm.Status),
		Content:   pm.Content,
		CreatedAt: pgtype.Timestamptz{Time: pm.CreatedAt.UTC(), Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: pm.UpdatedAt.UTC(), Valid: true},
		UpdatedBy: pm.UpdatedBy,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres postmortem: insert: %w", err)
	}
	pm.ID = id
	return nil
}

func (s *PostmortemStore) GetBySEVID(ctx context.Context, sevID string) (*store.Postmortem, error) {
	q := queries.New(s.pool)

	row, err := q.GetPostmortemBySEVID(ctx, sevID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres postmortem: get: %w", err)
	}
	return mapPostmortemRow(row), nil
}

func (s *PostmortemStore) Update(ctx context.Context, pm *store.Postmortem) error {
	q := queries.New(s.pool)

	// Pre-check so callers get a clean ErrNotFound rather than a silent
	// no-op — UpdatePostmortem's WHERE sev_id = $1 matches zero rows for an
	// unknown SEV without erroring on its own.
	if _, err := q.GetPostmortemBySEVID(ctx, pm.SEVID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres postmortem: pre-update get: %w", err)
	}

	if err := q.UpdatePostmortem(ctx, queries.UpdatePostmortemParams{
		SevID:     pm.SEVID,
		Status:    string(pm.Status),
		Content:   pm.Content,
		UpdatedAt: pgtype.Timestamptz{Time: pm.UpdatedAt.UTC(), Valid: true},
		UpdatedBy: pm.UpdatedBy,
	}); err != nil {
		return fmt.Errorf("postgres postmortem: update: %w", err)
	}
	return nil
}

func (s *PostmortemStore) CountByStatus(ctx context.Context) (map[store.PostmortemStatus]int, error) {
	q := queries.New(s.pool)

	rows, err := q.CountPostmortemsByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres postmortem: count by status: %w", err)
	}
	counts := make(map[store.PostmortemStatus]int, len(rows))
	for _, r := range rows {
		counts[store.PostmortemStatus(r.Status)] = int(r.Count)
	}
	return counts, nil
}

func mapPostmortemRow(r queries.Postmortem) *store.Postmortem {
	pm := &store.Postmortem{
		ID:        r.ID,
		SEVID:     r.SevID,
		Status:    store.PostmortemStatus(r.Status),
		Content:   r.Content,
		CreatedAt: r.CreatedAt.Time,
		UpdatedAt: r.UpdatedAt.Time,
		UpdatedBy: r.UpdatedBy,
	}
	return pm
}
