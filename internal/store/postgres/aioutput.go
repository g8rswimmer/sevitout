package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.AIOutputStore = (*AIOutputStore)(nil)

// AIOutputStore implements store.AIOutputStore against PostgreSQL.
type AIOutputStore struct {
	pool *pgxpool.Pool
}

// NewAIOutputStore returns an AIOutputStore backed by pool.
func NewAIOutputStore(pool *pgxpool.Pool) *AIOutputStore {
	return &AIOutputStore{pool: pool}
}

func (s *AIOutputStore) Create(ctx context.Context, output *store.AIOutput) error {
	q := queries.New(s.pool)

	id, err := q.InsertAIOutput(ctx, queries.InsertAIOutputParams{
		SevID:        output.SEVID,
		PluginID:     output.PluginID,
		TriggerEvent: output.TriggerEvent,
		Action:       output.Action,
		Content:      output.Content,
		CreatedAt:    pgtype.Timestamptz{Time: output.CreatedAt.UTC(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("postgres aioutput: insert: %w", err)
	}
	output.ID = id
	return nil
}

func (s *AIOutputStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.AIOutput, error) {
	q := queries.New(s.pool)

	rows, err := q.ListAIOutputsBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres aioutput: list: %w", err)
	}

	out := make([]*store.AIOutput, 0, len(rows))
	for _, r := range rows {
		out = append(out, &store.AIOutput{
			ID:           r.ID,
			SEVID:        r.SevID,
			PluginID:     r.PluginID,
			TriggerEvent: r.TriggerEvent,
			Action:       r.Action,
			Content:      r.Content,
			CreatedAt:    r.CreatedAt.Time,
		})
	}
	return out, nil
}
