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

var _ store.ServiceStore = (*ServiceStore)(nil)

// ServiceStore implements store.ServiceStore against PostgreSQL.
type ServiceStore struct {
	pool *pgxpool.Pool
}

// NewServiceStore returns a ServiceStore backed by pool.
func NewServiceStore(pool *pgxpool.Pool) *ServiceStore {
	return &ServiceStore{pool: pool}
}

func (s *ServiceStore) Create(ctx context.Context, svc *store.Service) error {
	q := queries.New(s.pool)

	tags, err := tagsToDB(svc.Tags)
	if err != nil {
		return fmt.Errorf("postgres service: marshal tags: %w", err)
	}

	if err := q.InsertService(ctx, queries.InsertServiceParams{
		ID:                 svc.ID,
		Name:               svc.Name,
		Description:        svc.Description,
		OwningTeam:         svc.OwningTeam,
		PagerdutyServiceID: svc.PagerDutyServiceID,
		Tags:               tags,
		Active:             svc.Active,
		CreatedAt:          pgtype.Timestamptz{Time: svc.CreatedAt.UTC(), Valid: true},
		UpdatedAt:          pgtype.Timestamptz{Time: svc.UpdatedAt.UTC(), Valid: true},
	}); err != nil {
		// Covers both a duplicate id (primary key) and a duplicate name
		// (services.name UNIQUE) — the handler's own error message ("a
		// service with this id or name already exists") already treats
		// both the same way, matching memory.ServiceStore.Create.
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres service: insert: %w", err)
	}
	return nil
}

func (s *ServiceStore) Get(ctx context.Context, id string) (*store.Service, error) {
	q := queries.New(s.pool)

	row, err := q.GetService(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres service: get: %w", err)
	}
	return mapServiceRow(row)
}

func (s *ServiceStore) Update(ctx context.Context, svc *store.Service) error {
	q := queries.New(s.pool)

	// Pre-check so callers get a clean ErrNotFound rather than a silent
	// no-op — UpdateService's WHERE id = $1 matches zero rows for an
	// unknown id without erroring on its own.
	if _, err := q.GetService(ctx, svc.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres service: pre-update get: %w", err)
	}

	tags, err := tagsToDB(svc.Tags)
	if err != nil {
		return fmt.Errorf("postgres service: marshal tags: %w", err)
	}

	if err := q.UpdateService(ctx, queries.UpdateServiceParams{
		ID:                 svc.ID,
		Name:               svc.Name,
		Description:        svc.Description,
		OwningTeam:         svc.OwningTeam,
		PagerdutyServiceID: svc.PagerDutyServiceID,
		Tags:               tags,
		Active:             svc.Active,
		UpdatedAt:          pgtype.Timestamptz{Time: svc.UpdatedAt.UTC(), Valid: true},
	}); err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres service: update: %w", err)
	}
	return nil
}

func (s *ServiceStore) Delete(ctx context.Context, id string) error {
	q := queries.New(s.pool)

	if _, err := q.GetService(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres service: pre-delete get: %w", err)
	}
	if err := q.DeleteService(ctx, id); err != nil {
		return fmt.Errorf("postgres service: delete: %w", err)
	}
	return nil
}

func (s *ServiceStore) List(ctx context.Context, activeOnly bool) ([]*store.Service, error) {
	q := queries.New(s.pool)

	var rows []queries.Service
	var err error
	if activeOnly {
		rows, err = q.ListActiveServices(ctx)
	} else {
		rows, err = q.ListServices(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres service: list: %w", err)
	}

	out := make([]*store.Service, 0, len(rows))
	for _, r := range rows {
		svc, err := mapServiceRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, nil
}

func mapServiceRow(r queries.Service) (*store.Service, error) {
	tags, err := tagsFromDB(r.Tags)
	if err != nil {
		return nil, fmt.Errorf("postgres service: unmarshal tags: %w", err)
	}
	return &store.Service{
		ID:                 r.ID,
		Name:               r.Name,
		Description:        r.Description,
		OwningTeam:         r.OwningTeam,
		PagerDutyServiceID: r.PagerdutyServiceID,
		Tags:               tags,
		Active:             r.Active,
		CreatedAt:          r.CreatedAt.Time,
		UpdatedAt:          r.UpdatedAt.Time,
	}, nil
}
