package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.SEVAccessStore = (*SEVAccessStore)(nil)

// SEVAccessStore implements store.SEVAccessStore against PostgreSQL.
type SEVAccessStore struct {
	pool *pgxpool.Pool
}

// NewSEVAccessStore returns a SEVAccessStore backed by pool.
func NewSEVAccessStore(pool *pgxpool.Pool) *SEVAccessStore {
	return &SEVAccessStore{pool: pool}
}

func (s *SEVAccessStore) Grant(ctx context.Context, access *store.SEVAccess) error {
	q := queries.New(s.pool)
	id, err := q.InsertSEVAccess(ctx, queries.InsertSEVAccessParams{
		SevID:     access.SEVID,
		UserID:    access.UserID,
		CreatedAt: pgtype.Timestamptz{Time: access.CreatedAt.UTC(), Valid: true},
		CreatedBy: access.CreatedBy,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres sev_access: grant: %w", err)
	}
	access.ID = id
	return nil
}

func (s *SEVAccessStore) Revoke(ctx context.Context, sevID string, id int64) error {
	q := queries.New(s.pool)
	tag, err := q.DeleteSEVAccess(ctx, queries.DeleteSEVAccessParams{ID: id, SevID: sevID})
	if err != nil {
		return fmt.Errorf("postgres sev_access: revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SEVAccessStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.SEVAccess, error) {
	q := queries.New(s.pool)
	rows, err := q.ListSEVAccessBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres sev_access: list: %w", err)
	}
	out := make([]*store.SEVAccess, 0, len(rows))
	for _, row := range rows {
		out = append(out, &store.SEVAccess{
			ID:        row.ID,
			SEVID:     row.SevID,
			UserID:    row.UserID,
			CreatedAt: row.CreatedAt.Time,
			CreatedBy: row.CreatedBy,
		})
	}
	return out, nil
}

func (s *SEVAccessStore) HasAccess(ctx context.Context, sevID, userID string) (bool, error) {
	q := queries.New(s.pool)
	ok, err := q.SEVAccessExists(ctx, queries.SEVAccessExistsParams{SevID: sevID, UserID: userID})
	if err != nil {
		return false, fmt.Errorf("postgres sev_access: has access: %w", err)
	}
	return ok, nil
}

func (s *SEVAccessStore) ListSEVIDsByUser(ctx context.Context, userID string) ([]string, error) {
	if userID == "" {
		return nil, nil
	}
	q := queries.New(s.pool)
	ids, err := q.ListSEVIDsByAccessUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres sev_access: list sev ids by user: %w", err)
	}
	return ids, nil
}
