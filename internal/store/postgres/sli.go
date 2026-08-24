package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var _ store.SLIStore = (*SLIStore)(nil)

// SLIStore implements store.SLIStore against PostgreSQL.
type SLIStore struct {
	pool *pgxpool.Pool
}

// NewSLIStore returns an SLIStore backed by pool.
func NewSLIStore(pool *pgxpool.Pool) *SLIStore {
	return &SLIStore{pool: pool}
}

func (s *SLIStore) Create(ctx context.Context, sli *store.SLI) error {
	q := queries.New(s.pool)

	id, err := q.InsertSLI(ctx, queries.InsertSLIParams{
		SevID:          sli.SEVID,
		ServiceID:      sli.ServiceID,
		SliName:        sli.SLIName,
		SloThreshold:   sli.SLOThreshold,
		MeasuredValue:  sli.MeasuredValue,
		ViolationStart: timeToDB(sli.ViolationStart),
		ViolationEnd:   timeToDB(sli.ViolationEnd),
		DashboardUrl:   sli.DashboardURL,
		CreatedAt:      pgtype.Timestamptz{Time: sli.CreatedAt.UTC(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("postgres sli: insert: %w", err)
	}
	sli.ID = id
	return nil
}

func (s *SLIStore) Delete(ctx context.Context, id int64) error {
	// sev_slis has no other identifying key, so — like SEVLinkStore.Delete —
	// this checks the affected-row count directly rather than pre-checking
	// existence with a separate SELECT.
	ct, err := s.pool.Exec(ctx, `DELETE FROM sev_slis WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("postgres sli: delete: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *SLIStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.SLI, error) {
	q := queries.New(s.pool)

	rows, err := q.ListSLIsBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres sli: list: %w", err)
	}

	out := make([]*store.SLI, 0, len(rows))
	for _, r := range rows {
		out = append(out, &store.SLI{
			ID:             r.ID,
			SEVID:          r.SevID,
			ServiceID:      r.ServiceID,
			SLIName:        r.SliName,
			SLOThreshold:   r.SloThreshold,
			MeasuredValue:  r.MeasuredValue,
			ViolationStart: timeFromDB(r.ViolationStart),
			ViolationEnd:   timeFromDB(r.ViolationEnd),
			DashboardURL:   r.DashboardUrl,
			CreatedAt:      r.CreatedAt.Time,
		})
	}
	return out, nil
}
