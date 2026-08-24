package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.IntegrationConfigStore = (*IntegrationConfigStore)(nil)

// IntegrationConfigStore implements store.IntegrationConfigStore against
// PostgreSQL. Credentials are stored exactly as handed to it (already
// encrypted by the caller — see internal/api/grpc.ConfigServer) and are
// never decrypted here.
type IntegrationConfigStore struct {
	pool *pgxpool.Pool
}

// NewIntegrationConfigStore returns an IntegrationConfigStore backed by pool.
func NewIntegrationConfigStore(pool *pgxpool.Pool) *IntegrationConfigStore {
	return &IntegrationConfigStore{pool: pool}
}

func (s *IntegrationConfigStore) Get(ctx context.Context, integrationType string) (*store.IntegrationConfig, error) {
	q := queries.New(s.pool)

	row, err := q.GetIntegrationConfig(ctx, integrationType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres integration config: get: %w", err)
	}
	return mapIntegrationConfigRow(row)
}

func (s *IntegrationConfigStore) Upsert(ctx context.Context, cfg *store.IntegrationConfig) error {
	q := queries.New(s.pool)

	settings, err := settingsToDB(cfg.Settings)
	if err != nil {
		return fmt.Errorf("postgres integration config: marshal settings: %w", err)
	}

	id, err := q.UpsertIntegrationConfig(ctx, queries.UpsertIntegrationConfigParams{
		IntegrationType:      cfg.IntegrationType,
		EncryptedCredentials: cfg.EncryptedCredentials,
		Settings:             settings,
	})
	if err != nil {
		return fmt.Errorf("postgres integration config: upsert: %w", err)
	}
	cfg.ID = id
	return nil
}

func (s *IntegrationConfigStore) List(ctx context.Context) ([]*store.IntegrationConfig, error) {
	q := queries.New(s.pool)

	rows, err := q.ListIntegrationConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres integration config: list: %w", err)
	}
	out := make([]*store.IntegrationConfig, 0, len(rows))
	for _, r := range rows {
		cfg, err := mapIntegrationConfigRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, nil
}

func mapIntegrationConfigRow(r queries.IntegrationConfig) (*store.IntegrationConfig, error) {
	settings, err := settingsFromDB(r.Settings)
	if err != nil {
		return nil, fmt.Errorf("postgres integration config: unmarshal settings: %w", err)
	}
	return &store.IntegrationConfig{
		ID:                   r.ID,
		IntegrationType:      r.IntegrationType,
		EncryptedCredentials: r.EncryptedCredentials,
		Settings:             settings,
		CreatedAt:            r.CreatedAt.Time,
		UpdatedAt:            r.UpdatedAt.Time,
	}, nil
}

// settingsToDB/settingsFromDB are tagsToDB/tagsFromDB's (sev.go)
// map[string]any equivalent — IntegrationConfig.Settings is typed
// map[string]any (ConfigServer only ever populates it with string values,
// but the store layer doesn't assume that), so the JSON round trip can't
// reuse the map[string]string-specific helpers as-is.
func settingsToDB(m map[string]any) ([]byte, error) {
	if m == nil {
		return json.Marshal(map[string]any{})
	}
	return json.Marshal(m)
}

func settingsFromDB(b []byte) (map[string]any, error) {
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	m := make(map[string]any)
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
