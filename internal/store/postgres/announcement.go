package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.AnnouncementStore = (*AnnouncementStore)(nil)

// AnnouncementStore implements store.AnnouncementStore against PostgreSQL.
type AnnouncementStore struct {
	pool *pgxpool.Pool
}

// NewAnnouncementStore returns an AnnouncementStore backed by pool.
func NewAnnouncementStore(pool *pgxpool.Pool) *AnnouncementStore {
	return &AnnouncementStore{pool: pool}
}

func (s *AnnouncementStore) Create(ctx context.Context, a *store.Announcement) error {
	q := queries.New(s.pool)

	id, err := q.InsertAnnouncement(ctx, queries.InsertAnnouncementParams{
		SevID:       a.SEVID,
		AuthorID:    a.AuthorID,
		Message:     a.Message,
		Audience:    string(a.Audience),
		IsMilestone: a.IsMilestone,
		CreatedAt:   pgtype.Timestamptz{Time: a.CreatedAt.UTC(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("postgres announcement: insert: %w", err)
	}
	a.ID = id
	return nil
}

func (s *AnnouncementStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.Announcement, error) {
	q := queries.New(s.pool)

	rows, err := q.ListAnnouncementsBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres announcement: list: %w", err)
	}

	out := make([]*store.Announcement, 0, len(rows))
	for _, r := range rows {
		out = append(out, &store.Announcement{
			ID:          r.ID,
			SEVID:       r.SevID,
			AuthorID:    r.AuthorID,
			Message:     r.Message,
			Audience:    store.AudienceType(r.Audience),
			IsMilestone: r.IsMilestone,
			CreatedAt:   r.CreatedAt.Time,
		})
	}
	return out, nil
}

// SearchSEVIDs returns the distinct SEV IDs with at least one announcement
// whose message matches query. Mirrors the in-memory store's contract: an
// empty query returns nil (distinguishing "no query" from "queried, zero
// matches"), even though callers currently always guard against an empty
// query before calling (internal/api/grpc/search.go).
func (s *AnnouncementStore) SearchSEVIDs(ctx context.Context, query string) ([]string, error) {
	if query == "" {
		return nil, nil
	}
	q := queries.New(s.pool)

	ids, err := q.SearchAnnouncementSEVIDs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres announcement: search: %w", err)
	}
	// A non-nil (possibly empty) slice distinguishes "queried, zero matches"
	// from the query=="" case above.
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}
