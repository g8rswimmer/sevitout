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

var _ store.UserStore = (*UserStore)(nil)

// UserStore implements store.UserStore against PostgreSQL.
type UserStore struct {
	pool *pgxpool.Pool
}

// NewUserStore returns a UserStore backed by pool.
func NewUserStore(pool *pgxpool.Pool) *UserStore {
	return &UserStore{pool: pool}
}

func (s *UserStore) Create(ctx context.Context, user *store.User) error {
	q := queries.New(s.pool)
	err := q.InsertUser(ctx, queries.InsertUserParams{
		ID:           user.ID,
		Email:        user.Email,
		Name:         user.Name,
		AvatarUrl:    user.AvatarURL,
		OrgRole:      string(user.OrgRole),
		Active:       user.Active,
		PasswordHash: user.PasswordHash,
		CreatedAt:    pgtype.Timestamptz{Time: user.CreatedAt.UTC(), Valid: true},
		UpdatedAt:    pgtype.Timestamptz{Time: user.UpdatedAt.UTC(), Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres user: insert: %w", err)
	}
	return nil
}

func (s *UserStore) Get(ctx context.Context, id string) (*store.User, error) {
	q := queries.New(s.pool)
	row, err := q.GetUser(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres user: get: %w", err)
	}
	return &store.User{
		ID:           row.ID,
		Email:        row.Email,
		Name:         row.Name,
		AvatarURL:    row.AvatarUrl,
		OrgRole:      store.OrgRole(row.OrgRole),
		Active:       row.Active,
		PasswordHash: row.PasswordHash,
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}, nil
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	q := queries.New(s.pool)
	row, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres user: get by email: %w", err)
	}
	return &store.User{
		ID:           row.ID,
		Email:        row.Email,
		Name:         row.Name,
		AvatarURL:    row.AvatarUrl,
		OrgRole:      store.OrgRole(row.OrgRole),
		Active:       row.Active,
		PasswordHash: row.PasswordHash,
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}, nil
}

func (s *UserStore) Update(ctx context.Context, user *store.User) error {
	q := queries.New(s.pool)
	if _, err := q.GetUser(ctx, user.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres user: pre-update get: %w", err)
	}
	if err := q.UpdateUser(ctx, queries.UpdateUserParams{
		ID:        user.ID,
		Name:      user.Name,
		AvatarUrl: user.AvatarURL,
		OrgRole:   string(user.OrgRole),
		Active:    user.Active,
		UpdatedAt: pgtype.Timestamptz{Time: user.UpdatedAt.UTC(), Valid: true},
	}); err != nil {
		return fmt.Errorf("postgres user: update: %w", err)
	}
	return nil
}

func (s *UserStore) List(ctx context.Context) ([]*store.User, error) {
	q := queries.New(s.pool)
	rows, err := q.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres user: list: %w", err)
	}
	users := make([]*store.User, 0, len(rows))
	for _, r := range rows {
		users = append(users, &store.User{
			ID:           r.ID,
			Email:        r.Email,
			Name:         r.Name,
			AvatarURL:    r.AvatarUrl,
			OrgRole:      store.OrgRole(r.OrgRole),
			Active:       r.Active,
			PasswordHash: r.PasswordHash,
			CreatedAt:    r.CreatedAt.Time,
			UpdatedAt:    r.UpdatedAt.Time,
		})
	}
	return users, nil
}
