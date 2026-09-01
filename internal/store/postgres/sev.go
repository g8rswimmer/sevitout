package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
		AlertUrl:              sv.AlertURL,
		DashboardUrl:          sv.DashboardURL,
		Query:                 sv.Query,
		SnapshotUrl:           sv.SnapshotURL,
		GithubRepo:            sv.GitHubRepo,
		RootCauseReferenceUrl: sv.RootCauseReferenceURL,
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
		AiDisabled:            sv.AIDisabled,
		CreatedAt:             pgtype.Timestamptz{Time: sv.CreatedAt.UTC(), Valid: true},
		UpdatedAt:             pgtype.Timestamptz{Time: sv.UpdatedAt.UTC(), Valid: true},
		CreatedBy:             sv.CreatedBy,
		SlackChannelID:        sv.SlackChannelID,
		RtpcSeconds:           sv.RTPCSeconds,
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
		AlertUrl:              sv.AlertURL,
		DashboardUrl:          sv.DashboardURL,
		Query:                 sv.Query,
		SnapshotUrl:           sv.SnapshotURL,
		GithubRepo:            sv.GitHubRepo,
		RootCauseReferenceUrl: sv.RootCauseReferenceURL,
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
		AiDisabled:            sv.AIDisabled,
		UpdatedAt:             pgtype.Timestamptz{Time: sv.UpdatedAt.UTC(), Valid: true},
		SlackChannelID:        sv.SlackChannelID,
		RtpcSeconds:           sv.RTPCSeconds,
	}); err != nil {
		return fmt.Errorf("postgres sev: update: %w", err)
	}
	return nil
}

func (s *SEVStore) List(ctx context.Context, filter store.SEVFilter) ([]*store.SEV, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	where, args := buildSEVFilterWhere(filter)
	n := len(args) + 1
	q := fmt.Sprintf("%s %s %s LIMIT $%d OFFSET $%d", sevSelectCols, where, sevOrderByClause(filter), n, n+1)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres sev: list: %w", err)
	}
	defer rows.Close()

	var result []*store.SEV
	for rows.Next() {
		sv, err := scanSEVRow(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres sev: scan row: %w", err)
		}
		result = append(result, sv)
	}
	return result, rows.Err()
}

func (s *SEVStore) Count(ctx context.Context, filter store.SEVFilter) (int, error) {
	where, args := buildSEVFilterWhere(filter)
	q := fmt.Sprintf("SELECT COUNT(*) FROM sevs %s", where)
	var n int
	if err := s.pool.QueryRow(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("postgres sev: count: %w", err)
	}
	return n, nil
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

const sevSelectCols = `SELECT id, title, description, severity_level, status,
       root_cause_category, root_cause_description, mitigation, prevention,
       business_impact, affected_services, detection_method, alert_name,
       monitoring_tool, alert_url, dashboard_url, query, snapshot_url, github_repo,
       root_cause_reference_url,
       right_people_present, right_people_notes, tags,
       started_at, detected_at, mitigated_at, resolved_at, postmortem_completed_at,
       mttd_seconds, mttm_seconds, mttr_seconds, dttm_seconds,
       locked, sensitive, ai_disabled, created_at, updated_at, created_by,
       slack_channel_id, rtpc_seconds
FROM sevs`

// buildSEVFilterWhere builds a parameterized WHERE clause from the filter,
// returning the clause string (empty if no conditions) and the bound args.
// Limit and Offset are handled by the caller. OnCallUser is also not handled
// here — it predates this function and is dead on the legacy SEVService.ListSEVs
// endpoint; the new SearchService resolves on_call_user separately via
// RoleStore into filter.IDs instead (see internal/api/grpc/search.go).
func buildSEVFilterWhere(filter store.SEVFilter) (string, []any) {
	var conds []string
	var args []any
	n := 1

	if len(filter.SeverityLevels) > 0 {
		conds = append(conds, fmt.Sprintf("severity_level = ANY($%d::smallint[])", n))
		args = append(args, filter.SeverityLevels)
		n++
	}
	if len(filter.Statuses) > 0 {
		strs := make([]string, len(filter.Statuses))
		for i, s := range filter.Statuses {
			strs[i] = string(s)
		}
		conds = append(conds, fmt.Sprintf("status = ANY($%d)", n))
		args = append(args, strs)
		n++
	}
	if filter.ExcludeSensitive {
		conds = append(conds, "NOT sensitive")
	}
	if filter.IDs != nil {
		conds = append(conds, fmt.Sprintf("id = ANY($%d::text[])", n))
		args = append(args, filter.IDs)
		n++
	}
	if len(filter.ServiceIDs) > 0 {
		conds = append(conds, fmt.Sprintf("affected_services && $%d::text[]", n))
		args = append(args, filter.ServiceIDs)
		n++
	}
	if len(filter.Tags) > 0 {
		// tagsToDB can't actually fail for a map[string]string (no cycles,
		// all-string keys/values), so discarding the error matches Create/
		// Update's own encoding while reusing their exact behavior.
		tagsJSON, _ := tagsToDB(filter.Tags)
		conds = append(conds, fmt.Sprintf("tags @> $%d::jsonb", n))
		args = append(args, tagsJSON)
		n++
	}
	if filter.RootCauseCategory != "" {
		conds = append(conds, fmt.Sprintf("root_cause_category = $%d", n))
		args = append(args, filter.RootCauseCategory)
		n++
	}
	if filter.StartedAfter != nil {
		conds = append(conds, fmt.Sprintf("started_at >= $%d", n))
		args = append(args, filter.StartedAfter.UTC())
		n++
	}
	if filter.StartedBefore != nil {
		conds = append(conds, fmt.Sprintf("started_at <= $%d", n))
		args = append(args, filter.StartedBefore.UTC())
		n++
	}
	if filter.Search != "" {
		// plainto_tsquery tokenizes free-form user input (implicit AND
		// between words) instead of requiring to_tsquery's operator syntax,
		// so arbitrary search text can't produce a malformed query.
		conds = append(conds, fmt.Sprintf("search_vector @@ plainto_tsquery('english', $%d)", n))
		args = append(args, filter.Search)
		n++ //nolint:ineffassign,staticcheck // final increment's value is never read; kept for symmetry with the append pattern above
	}

	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// sevOrderByClause builds the ORDER BY clause for filter.Sort/SortDesc. The
// zero value of Sort preserves the pre-M08 default ordering exactly. For an
// explicit sort field, "<col> IS NULL" is ordered first so that missing
// values (e.g. StartedAt/MTTRSeconds on an open SEV) always sort last
// regardless of direction, and id is a final deterministic tie-breaker.
func sevOrderByClause(filter store.SEVFilter) string {
	var col string
	switch filter.Sort {
	case store.SEVSortStartedAt:
		col = "started_at"
	case store.SEVSortSeverity:
		col = "severity_level"
	case store.SEVSortMTTR:
		col = "mttr_seconds"
	case store.SEVSortUpdatedAt:
		col = "updated_at"
	default:
		return "ORDER BY created_at DESC"
	}
	dir := "ASC"
	if filter.SortDesc {
		dir = "DESC"
	}
	return fmt.Sprintf("ORDER BY %s IS NULL, %s %s, id", col, col, dir)
}

// scanSEVRow scans a single SEV from an open pgx.Rows cursor.
func scanSEVRow(rows pgx.Rows) (*store.SEV, error) {
	var (
		id, title, desc, status, createdBy    string
		rootCateg, rootDesc, mitigation       *string
		prevention, bizImpact                 *string
		detMethod, alertName, monTool         *string
		alertURL, dashboardURL, query         *string
		snapshotURL, githubRepo               *string
		rootCauseReferenceURL                 *string
		rightPeopleNotes                      *string
		rightPeoplePresent                    *bool
		severityLevel                         int16
		locked, sensitive, aiDisabled         bool
		affectedServices                      []string
		tags                                  []byte
		startedAt, detectedAt                 pgtype.Timestamptz
		mitigatedAt, resolvedAt               pgtype.Timestamptz
		postmortemCompletedAt                 pgtype.Timestamptz
		mttdSeconds, mttmSeconds, mttrSeconds *int64
		dttmSeconds                           *int64
		createdAt, updatedAt                  pgtype.Timestamptz
		slackChannelID                        *string
		rtpcSeconds                           *int64
	)
	if err := rows.Scan(
		&id, &title, &desc, &severityLevel, &status,
		&rootCateg, &rootDesc, &mitigation, &prevention,
		&bizImpact, &affectedServices, &detMethod, &alertName,
		&monTool, &alertURL, &dashboardURL, &query, &snapshotURL, &githubRepo,
		&rootCauseReferenceURL,
		&rightPeoplePresent, &rightPeopleNotes, &tags,
		&startedAt, &detectedAt, &mitigatedAt, &resolvedAt, &postmortemCompletedAt,
		&mttdSeconds, &mttmSeconds, &mttrSeconds, &dttmSeconds,
		&locked, &sensitive, &aiDisabled, &createdAt, &updatedAt, &createdBy,
		&slackChannelID, &rtpcSeconds,
	); err != nil {
		return nil, err
	}
	tagMap, err := tagsFromDB(tags)
	if err != nil {
		return nil, fmt.Errorf("unmarshal tags: %w", err)
	}
	return &store.SEV{
		ID:                    id,
		Title:                 title,
		Description:           desc,
		SeverityLevel:         severityLevel,
		Status:                store.SEVStatus(status),
		RootCauseCategory:     rootCateg,
		RootCauseDescription:  rootDesc,
		Mitigation:            mitigation,
		Prevention:            prevention,
		BusinessImpact:        bizImpact,
		AffectedServices:      affectedServices,
		DetectionMethod:       detMethod,
		AlertName:             alertName,
		MonitoringTool:        monTool,
		AlertURL:              alertURL,
		DashboardURL:          dashboardURL,
		Query:                 query,
		SnapshotURL:           snapshotURL,
		GitHubRepo:            githubRepo,
		RootCauseReferenceURL: rootCauseReferenceURL,
		RightPeoplePresent:    rightPeoplePresent,
		RightPeopleNotes:      rightPeopleNotes,
		Tags:                  tagMap,
		StartedAt:             timeFromDB(startedAt),
		DetectedAt:            timeFromDB(detectedAt),
		MitigatedAt:           timeFromDB(mitigatedAt),
		ResolvedAt:            timeFromDB(resolvedAt),
		PostmortemCompletedAt: timeFromDB(postmortemCompletedAt),
		MTTDSeconds:           mttdSeconds,
		MTTMSeconds:           mttmSeconds,
		MTTRSeconds:           mttrSeconds,
		DTTMSeconds:           dttmSeconds,
		Locked:                locked,
		Sensitive:             sensitive,
		AIDisabled:            aiDisabled,
		CreatedAt:             createdAt.Time,
		UpdatedAt:             updatedAt.Time,
		CreatedBy:             createdBy,
		SlackChannelID:        slackChannelID,
		RTPCSeconds:           rtpcSeconds,
	}, nil
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
		AlertURL:              r.AlertUrl,
		DashboardURL:          r.DashboardUrl,
		Query:                 r.Query,
		SnapshotURL:           r.SnapshotUrl,
		GitHubRepo:            r.GithubRepo,
		RootCauseReferenceURL: r.RootCauseReferenceUrl,
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
		AIDisabled:            r.AiDisabled,
		CreatedAt:             r.CreatedAt.Time,
		UpdatedAt:             r.UpdatedAt.Time,
		CreatedBy:             r.CreatedBy,
		SlackChannelID:        r.SlackChannelID,
		RTPCSeconds:           r.RtpcSeconds,
	}, nil
}
