package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.SEVLinkStore = (*SEVLinkStore)(nil)

// SEVLinkStore implements store.SEVLinkStore against PostgreSQL.
type SEVLinkStore struct {
	pool *pgxpool.Pool
}

// NewSEVLinkStore returns a SEVLinkStore backed by pool.
func NewSEVLinkStore(pool *pgxpool.Pool) *SEVLinkStore {
	return &SEVLinkStore{pool: pool}
}

func (s *SEVLinkStore) Create(ctx context.Context, link *store.SEVLink) error {
	q := queries.New(s.pool)

	id, err := q.InsertSEVLink(ctx, queries.InsertSEVLinkParams{
		SourceSevID:      link.SourceSEVID,
		TargetSevID:      link.TargetSEVID,
		RelationshipType: string(link.RelationshipType),
		CreatedAt:        pgtype.Timestamptz{Time: link.CreatedAt.UTC(), Valid: true},
		CreatedBy:        link.CreatedBy,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres sevlink: insert: %w", err)
	}
	link.ID = id
	return nil
}

func (s *SEVLinkStore) Delete(ctx context.Context, sourceSEVID, targetSEVID string, relType store.SEVRelationshipType) error {
	// sev_links has no primary key that identifies a single link other than
	// the (source, target, type) triple, so there's no cheap pre-check like
	// the id-keyed stores use; a DELETE that matches nothing is detected
	// below via the affected-row count instead. Queried directly against the
	// pool (rather than sqlc's generated DeleteSEVLink) since sqlc emits
	// :exec as a bare error, discarding the command tag this needs.
	ct, err := s.pool.Exec(ctx, `DELETE FROM sev_links WHERE source_sev_id = $1 AND target_sev_id = $2 AND relationship_type = $3`,
		sourceSEVID, targetSEVID, string(relType))
	if err != nil {
		return fmt.Errorf("postgres sevlink: delete: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SEVLinkStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.SEVLink, error) {
	q := queries.New(s.pool)

	rows, err := q.ListSEVLinksBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres sevlink: list: %w", err)
	}

	out := make([]*store.SEVLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, &store.SEVLink{
			ID:               r.ID,
			SourceSEVID:      r.SourceSevID,
			TargetSEVID:      r.TargetSevID,
			RelationshipType: store.SEVRelationshipType(r.RelationshipType),
			CreatedAt:        r.CreatedAt.Time,
			CreatedBy:        r.CreatedBy,
		})
	}
	return out, nil
}
