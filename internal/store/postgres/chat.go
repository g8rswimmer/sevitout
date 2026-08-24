package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.ChatStore = (*ChatStore)(nil)

// ChatStore implements store.ChatStore against PostgreSQL.
type ChatStore struct {
	pool *pgxpool.Pool
}

// NewChatStore returns a ChatStore backed by pool.
func NewChatStore(pool *pgxpool.Pool) *ChatStore {
	return &ChatStore{pool: pool}
}

func (s *ChatStore) Create(ctx context.Context, entry *store.ChatEntry) error {
	q := queries.New(s.pool)

	id, err := q.InsertChatEntry(ctx, queries.InsertChatEntryParams{
		SevID:      entry.SEVID,
		OccurredAt: pgtype.Timestamptz{Time: entry.OccurredAt.UTC(), Valid: true},
		Source:     entry.Source,
		Author:     entry.Author,
		Content:    entry.Content,
		AddedAt:    pgtype.Timestamptz{Time: entry.AddedAt.UTC(), Valid: true},
		AddedBy:    entry.AddedBy,
	})
	if err != nil {
		return fmt.Errorf("postgres chat: insert: %w", err)
	}
	entry.ID = id
	return nil
}

func (s *ChatStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.ChatEntry, error) {
	q := queries.New(s.pool)

	rows, err := q.ListChatEntriesBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres chat: list: %w", err)
	}

	out := make([]*store.ChatEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, &store.ChatEntry{
			ID:         r.ID,
			SEVID:      r.SevID,
			OccurredAt: r.OccurredAt.Time,
			Source:     r.Source,
			Author:     r.Author,
			Content:    r.Content,
			AddedAt:    r.AddedAt.Time,
			AddedBy:    r.AddedBy,
		})
	}
	return out, nil
}
