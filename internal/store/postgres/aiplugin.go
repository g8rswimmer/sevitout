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

var _ store.AIPluginStore = (*AIPluginStore)(nil)

// AIPluginStore implements store.AIPluginStore against PostgreSQL.
type AIPluginStore struct {
	pool *pgxpool.Pool
}

// NewAIPluginStore returns an AIPluginStore backed by pool.
func NewAIPluginStore(pool *pgxpool.Pool) *AIPluginStore {
	return &AIPluginStore{pool: pool}
}

func (s *AIPluginStore) Create(ctx context.Context, p *store.AIPlugin) error {
	q := queries.New(s.pool)

	id, err := q.InsertAIPlugin(ctx, queries.InsertAIPluginParams{
		Name:                      p.Name,
		Version:                   p.Version,
		Description:               p.Description,
		HandlerType:               string(p.HandlerType),
		HttpEndpoint:              p.HTTPEndpoint,
		Provider:                  p.Provider,
		Model:                     p.Model,
		EncryptedApiKey:           p.EncryptedAPIKey,
		Enabled:                   p.Enabled,
		TriggerOnOpen:             p.TriggerOnOpen,
		TriggerOnMitigated:        p.TriggerOnMitigated,
		TriggerOnResolved:         p.TriggerOnResolved,
		TriggerOnPostmortemReview: p.TriggerOnPostmortemReview,
		RateLimitPerMinute:        p.RateLimitPerMinute,
		CreatedAt:                 pgtype.Timestamptz{Time: p.CreatedAt.UTC(), Valid: true},
		UpdatedAt:                 pgtype.Timestamptz{Time: p.UpdatedAt.UTC(), Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres ai plugin: insert: %w", err)
	}
	p.ID = id
	return nil
}

func (s *AIPluginStore) Get(ctx context.Context, id int64) (*store.AIPlugin, error) {
	q := queries.New(s.pool)

	row, err := q.GetAIPlugin(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres ai plugin: get: %w", err)
	}
	return &store.AIPlugin{
		ID:                        row.ID,
		Name:                      row.Name,
		Version:                   row.Version,
		Description:               row.Description,
		HandlerType:               store.AIHandlerType(row.HandlerType),
		HTTPEndpoint:              row.HttpEndpoint,
		Provider:                  row.Provider,
		Model:                     row.Model,
		EncryptedAPIKey:           row.EncryptedApiKey,
		Enabled:                   row.Enabled,
		TriggerOnOpen:             row.TriggerOnOpen,
		TriggerOnMitigated:        row.TriggerOnMitigated,
		TriggerOnResolved:         row.TriggerOnResolved,
		TriggerOnPostmortemReview: row.TriggerOnPostmortemReview,
		RateLimitPerMinute:        row.RateLimitPerMinute,
		CreatedAt:                 row.CreatedAt.Time,
		UpdatedAt:                 row.UpdatedAt.Time,
	}, nil
}

func (s *AIPluginStore) Update(ctx context.Context, p *store.AIPlugin) error {
	q := queries.New(s.pool)

	if _, err := q.GetAIPlugin(ctx, p.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres ai plugin: pre-update get: %w", err)
	}

	if err := q.UpdateAIPlugin(ctx, queries.UpdateAIPluginParams{
		ID:                        p.ID,
		Name:                      p.Name,
		Version:                   p.Version,
		Description:               p.Description,
		HandlerType:               string(p.HandlerType),
		HttpEndpoint:              p.HTTPEndpoint,
		Provider:                  p.Provider,
		Model:                     p.Model,
		EncryptedApiKey:           p.EncryptedAPIKey,
		Enabled:                   p.Enabled,
		TriggerOnOpen:             p.TriggerOnOpen,
		TriggerOnMitigated:        p.TriggerOnMitigated,
		TriggerOnResolved:         p.TriggerOnResolved,
		TriggerOnPostmortemReview: p.TriggerOnPostmortemReview,
		RateLimitPerMinute:        p.RateLimitPerMinute,
		UpdatedAt:                 pgtype.Timestamptz{Time: p.UpdatedAt.UTC(), Valid: true},
	}); err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres ai plugin: update: %w", err)
	}
	return nil
}

func (s *AIPluginStore) Delete(ctx context.Context, id int64) error {
	q := queries.New(s.pool)

	if _, err := q.GetAIPlugin(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres ai plugin: pre-delete get: %w", err)
	}
	if err := q.DeleteAIPlugin(ctx, id); err != nil {
		return fmt.Errorf("postgres ai plugin: delete: %w", err)
	}
	return nil
}

func (s *AIPluginStore) List(ctx context.Context) ([]*store.AIPlugin, error) {
	q := queries.New(s.pool)

	rows, err := q.ListAIPlugins(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres ai plugin: list: %w", err)
	}
	out := make([]*store.AIPlugin, 0, len(rows))
	for _, r := range rows {
		out = append(out, &store.AIPlugin{
			ID:                        r.ID,
			Name:                      r.Name,
			Version:                   r.Version,
			Description:               r.Description,
			HandlerType:               store.AIHandlerType(r.HandlerType),
			HTTPEndpoint:              r.HttpEndpoint,
			Provider:                  r.Provider,
			Model:                     r.Model,
			EncryptedAPIKey:           r.EncryptedApiKey,
			Enabled:                   r.Enabled,
			TriggerOnOpen:             r.TriggerOnOpen,
			TriggerOnMitigated:        r.TriggerOnMitigated,
			TriggerOnResolved:         r.TriggerOnResolved,
			TriggerOnPostmortemReview: r.TriggerOnPostmortemReview,
			RateLimitPerMinute:        r.RateLimitPerMinute,
			CreatedAt:                 r.CreatedAt.Time,
			UpdatedAt:                 r.UpdatedAt.Time,
		})
	}
	return out, nil
}
