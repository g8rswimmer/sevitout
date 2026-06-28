package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.RoleStore = (*RoleStore)(nil)

// RoleStore implements store.RoleStore against PostgreSQL.
type RoleStore struct {
	pool *pgxpool.Pool
}

// NewRoleStore returns a RoleStore backed by pool.
func NewRoleStore(pool *pgxpool.Pool) *RoleStore {
	return &RoleStore{pool: pool}
}

func (r *RoleStore) Assign(ctx context.Context, role *store.SEVRole) error {
	q := queries.New(r.pool)
	id, err := q.InsertSEVRole(ctx, queries.InsertSEVRoleParams{
		SevID:       role.SEVID,
		RoleType:    string(role.RoleType),
		UserID:      role.UserID,
		DisplayName: role.DisplayName,
		CreatedAt:   pgtype.Timestamptz{Time: role.CreatedAt.UTC(), Valid: true},
		CreatedBy:   role.CreatedBy,
	})
	if err != nil {
		return fmt.Errorf("postgres role: assign: %w", err)
	}
	role.ID = id
	return nil
}

func (r *RoleStore) Remove(ctx context.Context, sevID string, id int64) error {
	q := queries.New(r.pool)
	tag, err := q.DeleteSEVRole(ctx, queries.DeleteSEVRoleParams{ID: id, SevID: sevID})
	if err != nil {
		return fmt.Errorf("postgres role: remove: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r *RoleStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.SEVRole, error) {
	q := queries.New(r.pool)
	rows, err := q.ListRolesBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres role: list: %w", err)
	}
	out := make([]*store.SEVRole, 0, len(rows))
	for _, row := range rows {
		out = append(out, &store.SEVRole{
			ID:          row.ID,
			SEVID:       row.SevID,
			RoleType:    store.SEVRoleType(row.RoleType),
			UserID:      row.UserID,
			DisplayName: row.DisplayName,
			CreatedAt:   row.CreatedAt.Time,
			CreatedBy:   row.CreatedBy,
		})
	}
	return out, nil
}
