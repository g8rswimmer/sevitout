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

var _ store.ShareStore = (*ShareStore)(nil)

// ShareStore implements store.ShareStore against PostgreSQL.
type ShareStore struct {
	pool *pgxpool.Pool
}

// NewShareStore returns a ShareStore backed by pool.
func NewShareStore(pool *pgxpool.Pool) *ShareStore {
	return &ShareStore{pool: pool}
}

func (s *ShareStore) Create(ctx context.Context, link *store.ShareableLink) error {
	q := queries.New(s.pool)

	id, err := q.InsertShareableLink(ctx, queries.InsertShareableLinkParams{
		SevID:     link.SEVID,
		Token:     link.Token,
		CreatedBy: link.CreatedBy,
		ExpiresAt: timeToDB(link.ExpiresAt),
		CreatedAt: pgtype.Timestamptz{Time: link.CreatedAt.UTC(), Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres share: insert: %w", err)
	}
	link.ID = id
	return nil
}

func (s *ShareStore) GetByToken(ctx context.Context, token string) (*store.ShareableLink, error) {
	q := queries.New(s.pool)

	r, err := q.GetShareableLinkByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres share: get: %w", err)
	}
	return mapShareableLinkRow(r), nil
}

func (s *ShareStore) Revoke(ctx context.Context, token string, revokedBy string) error {
	q := queries.New(s.pool)

	link, err := q.GetShareableLinkByToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres share: pre-revoke get: %w", err)
	}
	if link.Revoked {
		// Matches the in-memory store: revoking an already-revoked link is a
		// no-op, not an error.
		return nil
	}

	if err := q.RevokeShareableLink(ctx, queries.RevokeShareableLinkParams{
		Token:     token,
		RevokedBy: &revokedBy,
	}); err != nil {
		return fmt.Errorf("postgres share: revoke: %w", err)
	}
	return nil
}

func (s *ShareStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.ShareableLink, error) {
	q := queries.New(s.pool)

	rows, err := q.ListShareableLinksBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres share: list: %w", err)
	}

	out := make([]*store.ShareableLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapShareableLinkRow(r))
	}
	return out, nil
}

func mapShareableLinkRow(r queries.ShareableLink) *store.ShareableLink {
	link := &store.ShareableLink{
		ID:        r.ID,
		SEVID:     r.SevID,
		Token:     r.Token,
		CreatedBy: r.CreatedBy,
		ExpiresAt: timeFromDB(r.ExpiresAt),
		Revoked:   r.Revoked,
		RevokedBy: r.RevokedBy,
		CreatedAt: r.CreatedAt.Time,
	}
	if v := timeFromDB(r.RevokedAt); v != nil {
		link.RevokedAt = v
	}
	return link
}
