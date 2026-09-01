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

var _ store.ServiceSLAStore = (*ServiceSLAStore)(nil)

// ServiceSLAStore implements store.ServiceSLAStore against PostgreSQL.
type ServiceSLAStore struct {
	pool *pgxpool.Pool
}

// NewServiceSLAStore returns a ServiceSLAStore backed by pool.
func NewServiceSLAStore(pool *pgxpool.Pool) *ServiceSLAStore {
	return &ServiceSLAStore{pool: pool}
}

func (s *ServiceSLAStore) Upsert(ctx context.Context, sla *store.ServiceSLA) error {
	q := queries.New(s.pool)

	id, err := q.UpsertServiceSLA(ctx, queries.UpsertServiceSLAParams{
		ServiceID:         sla.ServiceID,
		SeverityLevel:     sla.SeverityLevel,
		MttdTargetSeconds: sla.MTTDTargetSeconds,
		MttmTargetSeconds: sla.MTTMTargetSeconds,
		MttrTargetSeconds: sla.MTTRTargetSeconds,
	})
	if err != nil {
		return fmt.Errorf("postgres service sla: upsert: %w", err)
	}
	sla.ID = id
	return nil
}

func (s *ServiceSLAStore) Get(ctx context.Context, serviceID string, severityLevel int16) (*store.ServiceSLA, error) {
	q := queries.New(s.pool)

	row, err := q.GetServiceSLA(ctx, queries.GetServiceSLAParams{ServiceID: serviceID, SeverityLevel: severityLevel})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres service sla: get: %w", err)
	}
	return mapServiceSLARow(row), nil
}

func (s *ServiceSLAStore) Delete(ctx context.Context, serviceID string, severityLevel int16) error {
	q := queries.New(s.pool)

	// DeleteServiceSLA (sqlc :exec) doesn't report affected-row count, so
	// existence is checked first — same "Get, then act" shape as every other
	// Delete in this package (e.g. ServiceStore.Delete).
	if _, err := s.Get(ctx, serviceID, severityLevel); err != nil {
		return err
	}
	if err := q.DeleteServiceSLA(ctx, queries.DeleteServiceSLAParams{ServiceID: serviceID, SeverityLevel: severityLevel}); err != nil {
		return fmt.Errorf("postgres service sla: delete: %w", err)
	}
	return nil
}

func (s *ServiceSLAStore) ListByService(ctx context.Context, serviceID string) ([]*store.ServiceSLA, error) {
	q := queries.New(s.pool)

	rows, err := q.ListServiceSLAsByService(ctx, serviceID)
	if err != nil {
		return nil, fmt.Errorf("postgres service sla: list by service: %w", err)
	}
	out := make([]*store.ServiceSLA, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapServiceSLARow(r))
	}
	return out, nil
}

func (s *ServiceSLAStore) ListForServices(ctx context.Context, serviceIDs []string, severityLevel int16) ([]*store.ServiceSLA, error) {
	if len(serviceIDs) == 0 {
		return nil, nil
	}
	q := queries.New(s.pool)

	rows, err := q.ListServiceSLAsForServices(ctx, queries.ListServiceSLAsForServicesParams{
		Column1:       serviceIDs,
		SeverityLevel: severityLevel,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres service sla: list for services: %w", err)
	}
	out := make([]*store.ServiceSLA, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapServiceSLARow(r))
	}
	return out, nil
}

func mapServiceSLARow(r queries.ServiceSla) *store.ServiceSLA {
	return &store.ServiceSLA{
		ID:                r.ID,
		ServiceID:         r.ServiceID,
		SeverityLevel:     r.SeverityLevel,
		MTTDTargetSeconds: r.MttdTargetSeconds,
		MTTMTargetSeconds: r.MttmTargetSeconds,
		MTTRTargetSeconds: r.MttrTargetSeconds,
		CreatedAt:         r.CreatedAt.Time,
		UpdatedAt:         r.UpdatedAt.Time,
	}
}
