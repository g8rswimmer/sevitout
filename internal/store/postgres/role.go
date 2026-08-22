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

// ListSEVIDsByUser is a dynamic filter query (optional role type) rather than
// a static sqlc query, matching the pattern used by SEVStore's List/Count.
func (r *RoleStore) ListSEVIDsByUser(ctx context.Context, user string, roleType *store.SEVRoleType) ([]string, error) {
	if user == "" {
		return nil, nil
	}
	q := `SELECT DISTINCT sev_id FROM sev_roles WHERE (user_id = $1 OR display_name = $1)`
	args := []any{user}
	if roleType != nil {
		q += " AND role_type = $2"
		args = append(args, string(*roleType))
	}

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres role: list sev ids by user: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("postgres role: scan sev id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
