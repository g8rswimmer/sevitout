package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/g8rswimmer/sevitout/internal/sev"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/queries"
)

var (
	_ store.SEVStore           = (*SEVStore)(nil)
	_ store.StatusHistoryStore = (*StatusHistoryStore)(nil)
)

// SEVStore implements store.SEVStore against PostgreSQL.
type SEVStore struct {
	pool *pgxpool.Pool
}

// NewSEVStore returns a SEVStore backed by pool.
func NewSEVStore(pool *pgxpool.Pool) *SEVStore {
	return &SEVStore{pool: pool}
}

func (s *SEVStore) Create(ctx context.Context, sv *store.SEV) error {
	q := queries.New(s.pool)

	seq, err := q.NextSEVNumber(ctx)
	if err != nil {
		return fmt.Errorf("postgres sev: next sequence: %w", err)
	}

	// Year is derived from now so the ID reflects when the SEV was opened.
	sv.ID = sev.FormatID(time.Now().UTC().Year(), seq)

	tags, err := tagsToDB(sv.Tags)
	if err != nil {
		return fmt.Errorf("postgres sev: marshal tags: %w", err)
	}

	err = q.InsertSEV(ctx, queries.InsertSEVParams{
		ID:                    sv.ID,
		Title:                 sv.Title,
		Description:           sv.Description,
		SeverityLevel:         sv.SeverityLevel,
		Status:                string(sv.Status),
		RootCauseCategory:     sv.RootCauseCategory,
		RootCauseDescription:  sv.RootCauseDescription,
		Mitigation:            sv.Mitigation,
		Prevention:            sv.Prevention,
		BusinessImpact:        sv.BusinessImpact,
		AffectedServices:      sv.AffectedServices,
		DetectionMethod:       sv.DetectionMethod,
		AlertName:             sv.AlertName,
		MonitoringTool:        sv.MonitoringTool,
		RightPeoplePresent:    sv.RightPeoplePresent,
		RightPeopleNotes:      sv.RightPeopleNotes,
		Tags:                  tags,
		StartedAt:             timeToDB(sv.StartedAt),
		DetectedAt:            timeToDB(sv.DetectedAt),
		MitigatedAt:           timeToDB(sv.MitigatedAt),
		ResolvedAt:            timeToDB(sv.ResolvedAt),
		PostmortemCompletedAt: timeToDB(sv.PostmortemCompletedAt),
		MttdSeconds:           sv.MTTDSeconds,
		MttmSeconds:           sv.MTTMSeconds,
		MttrSeconds:           sv.MTTRSeconds,
		DttmSeconds:           sv.DTTMSeconds,
		Locked:                sv.Locked,
		Sensitive:             sv.Sensitive,
		CreatedAt:             pgtype.Timestamptz{Time: sv.CreatedAt.UTC(), Valid: true},
		UpdatedAt:             pgtype.Timestamptz{Time: sv.UpdatedAt.UTC(), Valid: true},
		CreatedBy:             sv.CreatedBy,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return fmt.Errorf("postgres sev: insert: %w", err)
	}
	return nil
}

func (s *SEVStore) Get(ctx context.Context, id string) (*store.SEV, error) {
	q := queries.New(s.pool)

	row, err := q.GetSEV(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("postgres sev: get: %w", err)
	}
	return mapGetSEVRow(row)
}

func (s *SEVStore) Update(ctx context.Context, sv *store.SEV) error {
	q := queries.New(s.pool)

	// Pre-check so callers get a clean ErrNotFound rather than a silent no-op.
	if _, err := q.GetSEV(ctx, sv.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres sev: pre-update get: %w", err)
	}

	tags, err := tagsToDB(sv.Tags)
	if err != nil {
		return fmt.Errorf("postgres sev: marshal tags: %w", err)
	}

	if err := q.UpdateSEV(ctx, queries.UpdateSEVParams{
		ID:                    sv.ID,
		Title:                 sv.Title,
		Description:           sv.Description,
		SeverityLevel:         sv.SeverityLevel,
		Status:                string(sv.Status),
		RootCauseCategory:     sv.RootCauseCategory,
		RootCauseDescription:  sv.RootCauseDescription,
		Mitigation:            sv.Mitigation,
		Prevention:            sv.Prevention,
		BusinessImpact:        sv.BusinessImpact,
		AffectedServices:      sv.AffectedServices,
		DetectionMethod:       sv.DetectionMethod,
		AlertName:             sv.AlertName,
		MonitoringTool:        sv.MonitoringTool,
		RightPeoplePresent:    sv.RightPeoplePresent,
		RightPeopleNotes:      sv.RightPeopleNotes,
		Tags:                  tags,
		StartedAt:             timeToDB(sv.StartedAt),
		DetectedAt:            timeToDB(sv.DetectedAt),
		MitigatedAt:           timeToDB(sv.MitigatedAt),
		ResolvedAt:            timeToDB(sv.ResolvedAt),
		PostmortemCompletedAt: timeToDB(sv.PostmortemCompletedAt),
		MttdSeconds:           sv.MTTDSeconds,
		MttmSeconds:           sv.MTTMSeconds,
		MttrSeconds:           sv.MTTRSeconds,
		DttmSeconds:           sv.DTTMSeconds,
		Locked:                sv.Locked,
		Sensitive:             sv.Sensitive,
		UpdatedAt:             pgtype.Timestamptz{Time: sv.UpdatedAt.UTC(), Valid: true},
	}); err != nil {
		return fmt.Errorf("postgres sev: update: %w", err)
	}
	return nil
}

// List returns SEVs ordered by created_at DESC using the provided pagination.
// The current sqlc query does not support filtering by severity, status, or
// search text; those filter fields are ignored until a filtered query is added.
func (s *SEVStore) List(ctx context.Context, filter store.SEVFilter) ([]*store.SEV, error) {
	q := queries.New(s.pool)

	limit := int32(filter.Limit)
	if limit <= 0 {
		limit = 100
	}
	offset := int32(filter.Offset)
	if offset < 0 {
		offset = 0
	}

	rows, err := q.ListSEVs(ctx, queries.ListSEVsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres sev: list: %w", err)
	}

	result := make([]*store.SEV, 0, len(rows))
	for _, r := range rows {
		sv, err := mapListSEVRow(r)
		if err != nil {
			return nil, fmt.Errorf("postgres sev: map row %s: %w", r.ID, err)
		}
		result = append(result, sv)
	}
	return result, nil
}

func (s *SEVStore) UpdateLocked(ctx context.Context, id string, locked bool) error {
	q := queries.New(s.pool)

	if _, err := q.GetSEV(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return fmt.Errorf("postgres sev: pre-lock get: %w", err)
	}

	if err := q.UpdateSEVLocked(ctx, queries.UpdateSEVLockedParams{
		ID:     id,
		Locked: locked,
	}); err != nil {
		return fmt.Errorf("postgres sev: update locked: %w", err)
	}
	return nil
}

// ---------- StatusHistoryStore ----------

// StatusHistoryStore implements store.StatusHistoryStore against PostgreSQL.
type StatusHistoryStore struct {
	pool *pgxpool.Pool
}

// NewStatusHistoryStore returns a StatusHistoryStore backed by pool.
func NewStatusHistoryStore(pool *pgxpool.Pool) *StatusHistoryStore {
	return &StatusHistoryStore{pool: pool}
}

func (s *StatusHistoryStore) Create(ctx context.Context, h *store.SEVStatusHistory) error {
	q := queries.New(s.pool)

	var fromStatus *string
	if h.FromStatus != nil {
		fs := string(*h.FromStatus)
		fromStatus = &fs
	}

	id, err := q.InsertStatusHistory(ctx, queries.InsertStatusHistoryParams{
		SevID:          h.SEVID,
		FromStatus:     fromStatus,
		ToStatus:       string(h.ToStatus),
		UserID:         h.UserID,
		TransitionedAt: pgtype.Timestamptz{Time: h.TransitionedAt.UTC(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("postgres status history: insert: %w", err)
	}
	h.ID = id
	return nil
}

func (s *StatusHistoryStore) ListBySEVID(ctx context.Context, sevID string) ([]*store.SEVStatusHistory, error) {
	q := queries.New(s.pool)

	rows, err := q.ListStatusHistoryBySEVID(ctx, sevID)
	if err != nil {
		return nil, fmt.Errorf("postgres status history: list: %w", err)
	}

	result := make([]*store.SEVStatusHistory, 0, len(rows))
	for _, r := range rows {
		h := &store.SEVStatusHistory{
			ID:             r.ID,
			SEVID:          r.SevID,
			ToStatus:       store.SEVStatus(r.ToStatus),
			UserID:         r.UserID,
			TransitionedAt: r.TransitionedAt.Time,
		}
		if r.FromStatus != nil {
			fs := store.SEVStatus(*r.FromStatus)
			h.FromStatus = &fs
		}
		result = append(result, h)
	}
	return result, nil
}

// ---------- type conversion helpers ----------

func timeToDB(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{Valid: false}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func timeFromDB(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func tagsToDB(tags map[string]string) ([]byte, error) {
	if tags == nil {
		return json.Marshal(map[string]string{})
	}
	return json.Marshal(tags)
}

func tagsFromDB(b []byte) (map[string]string, error) {
	if len(b) == 0 {
		return map[string]string{}, nil
	}
	m := make(map[string]string)
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func mapGetSEVRow(r queries.GetSEVRow) (*store.SEV, error) {
	tags, err := tagsFromDB(r.Tags)
	if err != nil {
		return nil, fmt.Errorf("unmarshal tags: %w", err)
	}
	return &store.SEV{
		ID:                    r.ID,
		Title:                 r.Title,
		Description:           r.Description,
		SeverityLevel:         r.SeverityLevel,
		Status:                store.SEVStatus(r.Status),
		RootCauseCategory:     r.RootCauseCategory,
		RootCauseDescription:  r.RootCauseDescription,
		Mitigation:            r.Mitigation,
		Prevention:            r.Prevention,
		BusinessImpact:        r.BusinessImpact,
		AffectedServices:      r.AffectedServices,
		DetectionMethod:       r.DetectionMethod,
		AlertName:             r.AlertName,
		MonitoringTool:        r.MonitoringTool,
		RightPeoplePresent:    r.RightPeoplePresent,
		RightPeopleNotes:      r.RightPeopleNotes,
		Tags:                  tags,
		StartedAt:             timeFromDB(r.StartedAt),
		DetectedAt:            timeFromDB(r.DetectedAt),
		MitigatedAt:           timeFromDB(r.MitigatedAt),
		ResolvedAt:            timeFromDB(r.ResolvedAt),
		PostmortemCompletedAt: timeFromDB(r.PostmortemCompletedAt),
		MTTDSeconds:           r.MttdSeconds,
		MTTMSeconds:           r.MttmSeconds,
		MTTRSeconds:           r.MttrSeconds,
		DTTMSeconds:           r.DttmSeconds,
		Locked:                r.Locked,
		Sensitive:             r.Sensitive,
		CreatedAt:             r.CreatedAt.Time,
		UpdatedAt:             r.UpdatedAt.Time,
		CreatedBy:             r.CreatedBy,
	}, nil
}

func mapListSEVRow(r queries.ListSEVsRow) (*store.SEV, error) {
	tags, err := tagsFromDB(r.Tags)
	if err != nil {
		return nil, fmt.Errorf("unmarshal tags: %w", err)
	}
	return &store.SEV{
		ID:                    r.ID,
		Title:                 r.Title,
		Description:           r.Description,
		SeverityLevel:         r.SeverityLevel,
		Status:                store.SEVStatus(r.Status),
		RootCauseCategory:     r.RootCauseCategory,
		RootCauseDescription:  r.RootCauseDescription,
		Mitigation:            r.Mitigation,
		Prevention:            r.Prevention,
		BusinessImpact:        r.BusinessImpact,
		AffectedServices:      r.AffectedServices,
		DetectionMethod:       r.DetectionMethod,
		AlertName:             r.AlertName,
		MonitoringTool:        r.MonitoringTool,
		RightPeoplePresent:    r.RightPeoplePresent,
		RightPeopleNotes:      r.RightPeopleNotes,
		Tags:                  tags,
		StartedAt:             timeFromDB(r.StartedAt),
		DetectedAt:            timeFromDB(r.DetectedAt),
		MitigatedAt:           timeFromDB(r.MitigatedAt),
		ResolvedAt:            timeFromDB(r.ResolvedAt),
		PostmortemCompletedAt: timeFromDB(r.PostmortemCompletedAt),
		MTTDSeconds:           r.MttdSeconds,
		MTTMSeconds:           r.MttmSeconds,
		MTTRSeconds:           r.MttrSeconds,
		DTTMSeconds:           r.DttmSeconds,
		Locked:                r.Locked,
		Sensitive:             r.Sensitive,
		CreatedAt:             r.CreatedAt.Time,
		UpdatedAt:             r.UpdatedAt.Time,
		CreatedBy:             r.CreatedBy,
	}, nil
}
