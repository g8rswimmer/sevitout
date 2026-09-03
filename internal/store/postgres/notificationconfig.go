package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.NotificationConfigStore = (*NotificationConfigStore)(nil)

// NotificationConfigStore implements store.NotificationConfigStore against
// PostgreSQL.
type NotificationConfigStore struct {
	pool *pgxpool.Pool
}

// NewNotificationConfigStore returns a NotificationConfigStore backed by pool.
func NewNotificationConfigStore(pool *pgxpool.Pool) *NotificationConfigStore {
	return &NotificationConfigStore{pool: pool}
}

func (s *NotificationConfigStore) Upsert(ctx context.Context, cfg *store.NotificationConfig) error {
	q := queries.New(s.pool)

	row, err := q.UpsertNotificationConfig(ctx, queries.UpsertNotificationConfigParams{
		Role:             string(cfg.Role),
		Event:            cfg.Event,
		ChannelType:      string(cfg.ChannelType),
		ChannelTarget:    cfg.ChannelTarget,
		MaxSeverityLevel: cfg.MaxSeverityLevel,
	})
	if err != nil {
		return fmt.Errorf("postgres notification config: upsert: %w", err)
	}
	cfg.ID = row.ID
	cfg.CreatedAt = row.CreatedAt.Time
	cfg.UpdatedAt = row.UpdatedAt.Time
	return nil
}

func (s *NotificationConfigStore) Delete(ctx context.Context, role store.OrgRole, event string, channelType store.NotificationChannelType) error {
	q := queries.New(s.pool)

	n, err := q.DeleteNotificationConfig(ctx, queries.DeleteNotificationConfigParams{
		Role:        string(role),
		Event:       event,
		ChannelType: string(channelType),
	})
	if err != nil {
		return fmt.Errorf("postgres notification config: delete: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *NotificationConfigStore) List(ctx context.Context) ([]*store.NotificationConfig, error) {
	q := queries.New(s.pool)

	rows, err := q.ListNotificationConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres notification config: list: %w", err)
	}
	out := make([]*store.NotificationConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, &store.NotificationConfig{
			ID:               r.ID,
			Role:             store.OrgRole(r.Role),
			Event:            r.Event,
			ChannelType:      store.NotificationChannelType(r.ChannelType),
			ChannelTarget:    r.ChannelTarget,
			MaxSeverityLevel: r.MaxSeverityLevel,
			CreatedAt:        r.CreatedAt.Time,
			UpdatedAt:        r.UpdatedAt.Time,
		})
	}
	return out, nil
}

func (s *NotificationConfigStore) ListForEvent(ctx context.Context, event string, severityLevel *int16) ([]*store.NotificationConfig, error) {
	q := queries.New(s.pool)

	rows, err := q.ListNotificationConfigsForEvent(ctx, queries.ListNotificationConfigsForEventParams{
		Event:         event,
		SeverityLevel: severityLevel,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres notification config: list for event: %w", err)
	}
	out := make([]*store.NotificationConfig, 0, len(rows))
	for _, r := range rows {
		out = append(out, &store.NotificationConfig{
			ID:               r.ID,
			Role:             store.OrgRole(r.Role),
			Event:            r.Event,
			ChannelType:      store.NotificationChannelType(r.ChannelType),
			ChannelTarget:    r.ChannelTarget,
			MaxSeverityLevel: r.MaxSeverityLevel,
			CreatedAt:        r.CreatedAt.Time,
			UpdatedAt:        r.UpdatedAt.Time,
		})
	}
	return out, nil
}
