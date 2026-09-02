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

var _ store.ServiceLevelingCriteriaStore = (*ServiceLevelingCriteriaStore)(nil)

// ServiceLevelingCriteriaStore implements store.ServiceLevelingCriteriaStore
// against PostgreSQL.
type ServiceLevelingCriteriaStore struct {
	pool *pgxpool.Pool
}

// NewServiceLevelingCriteriaStore returns a ServiceLevelingCriteriaStore
// backed by pool.
func NewServiceLevelingCriteriaStore(pool *pgxpool.Pool) *ServiceLevelingCriteriaStore {
	return &ServiceLevelingCriteriaStore{pool: pool}
}

func (s *ServiceLevelingCriteriaStore) Upsert(ctx context.Context, c *store.ServiceLevelingCriteria) error {
	q := queries.New(s.pool)

	id, err := q.UpsertServiceLevelingCriteria(ctx, queries.UpsertServiceLevelingCriteriaParams{
		ServiceID:     c.ServiceID,
		SeverityLevel: c.SeverityLevel,
		Criteria:      c.Criteria,
	})
	if err != nil {
		return fmt.Errorf("postgres service leveling criteria: upsert: %w", err)
	}
	c.ID = id
	return nil
}

func (s *ServiceLevelingCriteriaStore) Get(ctx context.Context, serviceID string, severityLevel int16) (*store.ServiceLevelingCriteria, error) {
	q := queries.New(s.pool)

	row, err := q.GetServiceLevelingCriteria(ctx, queries.GetServiceLevelingCriteriaParams{ServiceID: serviceID, SeverityLevel: severityLevel})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres service leveling criteria: get: %w", err)
	}
	return mapServiceLevelingCriteriaRow(row), nil
}

func (s *ServiceLevelingCriteriaStore) Delete(ctx context.Context, serviceID string, severityLevel int16) error {
	q := queries.New(s.pool)

	// DeleteServiceLevelingCriteria (sqlc :exec) doesn't report affected-row
	// count, so existence is checked first — same "Get, then act" shape as
	// ServiceSLAStore.Delete and every other Delete in this package.
	if _, err := s.Get(ctx, serviceID, severityLevel); err != nil {
		return err
	}
	if err := q.DeleteServiceLevelingCriteria(ctx, queries.DeleteServiceLevelingCriteriaParams{ServiceID: serviceID, SeverityLevel: severityLevel}); err != nil {
		return fmt.Errorf("postgres service leveling criteria: delete: %w", err)
	}
	return nil
}

func (s *ServiceLevelingCriteriaStore) ListByService(ctx context.Context, serviceID string) ([]*store.ServiceLevelingCriteria, error) {
	q := queries.New(s.pool)

	rows, err := q.ListServiceLevelingCriteriaByService(ctx, serviceID)
	if err != nil {
		return nil, fmt.Errorf("postgres service leveling criteria: list by service: %w", err)
	}
	out := make([]*store.ServiceLevelingCriteria, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapServiceLevelingCriteriaRow(r))
	}
	return out, nil
}

func (s *ServiceLevelingCriteriaStore) ListForServices(ctx context.Context, serviceIDs []string, severityLevel int16) ([]*store.ServiceLevelingCriteria, error) {
	if len(serviceIDs) == 0 {
		return nil, nil
	}
	q := queries.New(s.pool)

	rows, err := q.ListServiceLevelingCriteriaForServices(ctx, queries.ListServiceLevelingCriteriaForServicesParams{
		Column1:       serviceIDs,
		SeverityLevel: severityLevel,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres service leveling criteria: list for services: %w", err)
	}
	out := make([]*store.ServiceLevelingCriteria, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapServiceLevelingCriteriaRow(r))
	}
	return out, nil
}

func mapServiceLevelingCriteriaRow(r queries.ServiceLevelingCriterium) *store.ServiceLevelingCriteria {
	return &store.ServiceLevelingCriteria{
		ID:            r.ID,
		ServiceID:     r.ServiceID,
		SeverityLevel: r.SeverityLevel,
		Criteria:      r.Criteria,
		CreatedAt:     r.CreatedAt.Time,
		UpdatedAt:     r.UpdatedAt.Time,
	}
}
