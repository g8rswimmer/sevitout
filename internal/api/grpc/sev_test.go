package grpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/g8rswimmer/sevitout/internal/ai"
	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// adminCtx returns a context carrying an Admin auth.UserContext — Admin
// bypasses sensitiveSEVVisible (§14) unconditionally, so tests exercising
// unrelated behavior against a Sensitive SEV (e.g. publish suppression,
// auto-link exclusion) can use this instead of context.Background(), which
// would now be denied visibility into a Sensitive SEV as an unauthenticated
// caller.
func adminCtx() context.Context {
	return auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-admin", OrgRole: store.OrgRoleAdmin})
}

// testSEVServer groups a SEVServer with its backing in-memory stores.
type testSEVServer struct {
	server      *grpchandler.SEVServer
	sevs        *memory.SEVStore
	audit       *memory.AuditStore
	history     *memory.StatusHistoryStore
	links       *memory.SEVLinkStore
	access      *memory.SEVAccessStore
	services    *memory.ServiceStore
	serviceSLAs *memory.ServiceSLAStore
	pub         *fakePublisher
	ai          *fakeAIDispatcher
}

// newTestSEVServer returns a fresh SEVServer backed by empty in-memory stores.
func newTestSEVServer() *testSEVServer {
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	history := memory.NewStatusHistoryStore()
	links := memory.NewSEVLinkStore()
	access := memory.NewSEVAccessStore()
	services := memory.NewServiceStore()
	serviceSLAs := memory.NewServiceSLAStore()
	pub := &fakePublisher{}
	aiDispatch := &fakeAIDispatcher{}
	return &testSEVServer{
		server: grpchandler.NewSEVServer(grpchandler.SEVServerParams{
			SEVs: sevs, Audit: audit, History: history, Roles: memory.NewRoleStore(),
			Services: services, ServiceSLAs: serviceSLAs, Postmortems: memory.NewPostmortemStore(),
			Links: links, Access: access, Publisher: pub, AIDispatch: aiDispatch,
		}),
		sevs:        sevs,
		audit:       audit,
		history:     history,
		links:       links,
		access:      access,
		services:    services,
		serviceSLAs: serviceSLAs,
		pub:         pub,
		ai:          aiDispatch,
	}
}

// newTestSEVServerWithNotifier is like newTestSEVServer but also wires a
// real *grpchandler.Notifier (docs/roadmap.md Phase 15), backed by tn's fake
// Slack/email senders — so tests can assert a handler actually calls
// Notify with the right event type, not just that it doesn't panic.
func newTestSEVServerWithNotifier(t *testing.T, tn *testNotifier) *testSEVServer {
	t.Helper()
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	history := memory.NewStatusHistoryStore()
	links := memory.NewSEVLinkStore()
	access := memory.NewSEVAccessStore()
	services := memory.NewServiceStore()
	serviceSLAs := memory.NewServiceSLAStore()
	pub := &fakePublisher{}
	aiDispatch := &fakeAIDispatcher{}
	return &testSEVServer{
		server: grpchandler.NewSEVServer(grpchandler.SEVServerParams{
			SEVs: sevs, Audit: audit, History: history, Roles: memory.NewRoleStore(),
			Services: services, ServiceSLAs: serviceSLAs, Postmortems: memory.NewPostmortemStore(),
			Links: links, Access: access, Publisher: pub, AIDispatch: aiDispatch,
			Notifier: tn.notifier,
		}),
		sevs:        sevs,
		audit:       audit,
		history:     history,
		links:       links,
		access:      access,
		services:    services,
		serviceSLAs: serviceSLAs,
		pub:         pub,
		ai:          aiDispatch,
	}
}

// seedSEV inserts a SEV directly into the backing store and returns the
// auto-assigned ID. Use this for tests that need a pre-existing SEV to call
// handlers such as TransitionStatus or UpdateSEV.
func seedSEV(t *testing.T, ts *testSEVServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Seeded SEV",
		SeverityLevel: 1,
		Status:        store.SEVStatusOpen,
		CreatedBy:     "user-seed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSEV: %v", err)
	}
	return sv.ID
}

// grpcCode extracts the gRPC status code from an error returned by a handler.
func grpcCode(err error) codes.Code {
	if st, ok := status.FromError(err); ok {
		return st.Code()
	}
	return codes.Unknown
}

// ── CreateSEV ─────────────────────────────────────────────────────────────────

func TestCreateSEV_ValidRequest(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Database failure",
		SeverityLevel: 2,
		Description:   "Primary DB is unresponsive",
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}
	if resp.GetStatus() != string(store.SEVStatusOpen) {
		t.Errorf("Status = %q, want %q", resp.GetStatus(), store.SEVStatusOpen)
	}
	if resp.GetTitle() != "Database failure" {
		t.Errorf("Title = %q, want %q", resp.GetTitle(), "Database failure")
	}
	if resp.GetSeverityLevel() != 2 {
		t.Errorf("SeverityLevel = %d, want 2", resp.GetSeverityLevel())
	}
	if resp.GetDescription() != "Primary DB is unresponsive" {
		t.Errorf("Description = %q, want %q", resp.GetDescription(), "Primary DB is unresponsive")
	}
}

// TestCreateSEV_StartedAtNotDefaulted guards against reintroducing the
// previous "default started_at to now when omitted" behavior — the caller
// must set it explicitly (docs/requirements.md §2.1: "may be estimated", not
// assumed to be the moment the record was opened).
func TestCreateSEV_StartedAtNotDefaulted(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Test SEV",
		SeverityLevel: 1,
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}
	if resp.GetStartedAt() != nil {
		t.Errorf("StartedAt = %v, want nil (unset) when omitted from the request", resp.GetStartedAt())
	}
}

func TestCreateSEV_PublishesSEVCreated(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Database failure",
		SeverityLevel: 1,
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].eventType != "sev.created" || events[0].sevID != resp.GetId() {
		t.Errorf("got %+v, want type=sev.created sev_id=%s", events[0], resp.GetId())
	}
}

func TestCreateSEV_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	if _, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Sensitive incident",
		SeverityLevel: 1,
		Sensitive:     true,
	}); err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

func TestCreateSEV_NotifiesOnSevCreated(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	tn.addRule(t, store.OrgRoleIncidentCommander, "sev.created", store.NotificationChannelSlack, "#incidents", nil)
	ts := newTestSEVServerWithNotifier(t, tn)

	if _, err := ts.server.CreateSEV(context.Background(), &pb.CreateSEVRequest{
		Title: "Database failure", SeverityLevel: 1,
	}); err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	if tn.slack.calls != 1 {
		t.Fatalf("want 1 notification delivery on sev.created, got %d", tn.slack.calls)
	}
	if tn.slack.channel != "#incidents" {
		t.Errorf("channel = %q, want %q", tn.slack.channel, "#incidents")
	}
}

func TestCreateSEV_SensitiveSEVDoesNotNotify(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	tn.addRule(t, store.OrgRoleIncidentCommander, "sev.created", store.NotificationChannelSlack, "#incidents", nil)
	ts := newTestSEVServerWithNotifier(t, tn)

	if _, err := ts.server.CreateSEV(context.Background(), &pb.CreateSEVRequest{
		Title: "Sensitive incident", SeverityLevel: 1, Sensitive: true,
	}); err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	if tn.slack.calls != 0 {
		t.Errorf("want no notification delivery for a sensitive SEV, got %d", tn.slack.calls)
	}
}

func TestTransitionStatus_NotifiesStatusChangedAndPostmortemDue(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	tn.addRule(t, store.OrgRoleIncidentCommander, "sev.status_changed", store.NotificationChannelSlack, "#incidents", nil)
	tn.addRule(t, store.OrgRoleIncidentCommander, "postmortem.due", store.NotificationChannelSlack, "#incidents", nil)
	ts := newTestSEVServerWithNotifier(t, tn)
	sevID := seedSEV(t, ts)

	if _, err := ts.server.TransitionStatus(context.Background(), &pb.TransitionStatusRequest{
		Id: sevID, ToStatus: string(store.SEVStatusInvestigating),
	}); err != nil {
		t.Fatalf("TransitionStatus to Investigating: %v", err)
	}
	if tn.slack.calls != 1 {
		t.Fatalf("want 1 delivery (sev.status_changed only) after Investigating, got %d", tn.slack.calls)
	}

	if _, err := ts.server.TransitionStatus(context.Background(), &pb.TransitionStatusRequest{
		Id: sevID, ToStatus: string(store.SEVStatusMitigated),
	}); err != nil {
		t.Fatalf("TransitionStatus to Mitigated: %v", err)
	}
	if _, err := ts.server.TransitionStatus(context.Background(), &pb.TransitionStatusRequest{
		Id: sevID, ToStatus: string(store.SEVStatusResolved),
	}); err != nil {
		t.Fatalf("TransitionStatus to Resolved: %v", err)
	}

	// Investigating (1) + Mitigated (1) + Resolved (status_changed + postmortem.due = 2) = 4.
	if tn.slack.calls != 4 {
		t.Fatalf("want 4 total deliveries after reaching Resolved (status_changed x3 + postmortem.due x1), got %d", tn.slack.calls)
	}
}

func TestCreateSEV_AutoGrantsCreatorOnSensitiveCreate(t *testing.T) {
	ts := newTestSEVServer()
	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-reporter", OrgRole: store.OrgRoleResponder})

	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Sensitive incident", SeverityLevel: 1, Sensitive: true,
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	ok, err := ts.access.HasAccess(context.Background(), created.GetId(), "user-reporter")
	if err != nil {
		t.Fatalf("HasAccess: %v", err)
	}
	if !ok {
		t.Fatal("expected the creator to be auto-granted access to their own Sensitive SEV")
	}

	// Confirm the grant is actually load-bearing: the reporter can still
	// GetSEV their own SEV afterward without needing Admin/IC.
	if _, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: created.GetId()}); err != nil {
		t.Fatalf("GetSEV as reporter: %v", err)
	}
}

func TestCreateSEV_NonSensitiveDoesNotAutoGrant(t *testing.T) {
	ts := newTestSEVServer()
	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-reporter", OrgRole: store.OrgRoleResponder})

	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{Title: "Ordinary incident", SeverityLevel: 3})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	grants, err := ts.access.ListBySEVID(context.Background(), created.GetId())
	if err != nil {
		t.Fatalf("ListBySEVID: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("want no access grants for a non-sensitive SEV, got %d", len(grants))
	}
}

func TestCreateSEV_MissingTitle(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		SeverityLevel: 1,
		CreatedBy:     "user-1",
		// Title intentionally omitted
	})
	if err == nil {
		t.Fatal("CreateSEV: want error for missing title, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestCreateSEV_SeverityLevelZero(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Test SEV",
		SeverityLevel: 0, // proto default — invalid; must be 1-4
		CreatedBy:     "user-1",
	})
	if err == nil {
		t.Fatal("CreateSEV: want error for severity_level 0, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestCreateSEV_DetectionMetadataAndLinks(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:           "Checkout errors",
		SeverityLevel:   2,
		DetectionMethod: string(store.DetectionMethodMonitoringDashboard),
		MonitoringTool:  "datadog",
		AlertUrl:        "https://pagerduty.example.com/incidents/1",
		DashboardUrl:    "https://app.datadoghq.com/dashboard/abc",
		Query:           "sum:trace.express.request.errors{service:checkout}",
		SnapshotUrl:     "https://img.example.com/snapshot.png",
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}
	if got, want := resp.GetDetectionMethod(), string(store.DetectionMethodMonitoringDashboard); got != want {
		t.Errorf("DetectionMethod = %q, want %q", got, want)
	}
	if got, want := resp.GetAlertUrl(), "https://pagerduty.example.com/incidents/1"; got != want {
		t.Errorf("AlertUrl = %q, want %q", got, want)
	}
	if got, want := resp.GetDashboardUrl(), "https://app.datadoghq.com/dashboard/abc"; got != want {
		t.Errorf("DashboardUrl = %q, want %q", got, want)
	}
	if got, want := resp.GetQuery(), "sum:trace.express.request.errors{service:checkout}"; got != want {
		t.Errorf("Query = %q, want %q", got, want)
	}
	if got, want := resp.GetSnapshotUrl(), "https://img.example.com/snapshot.png"; got != want {
		t.Errorf("SnapshotUrl = %q, want %q", got, want)
	}
}

func TestCreateSEV_UnknownMonitoringTool_Rejected(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:          "Checkout errors",
		SeverityLevel:  2,
		MonitoringTool: "new-relic",
	})
	if err == nil {
		t.Fatal("CreateSEV: want error for unknown monitoring_tool, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestCreateSEV_UnknownDetectionMethod(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:           "Test SEV",
		SeverityLevel:   1,
		DetectionMethod: "carrier-pigeon",
	})
	if err == nil {
		t.Fatal("CreateSEV: want error for unknown detection_method, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

// ── GetSEV ────────────────────────────────────────────────────────────────────

func TestGetSEV_AfterCreate(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Network outage",
		SeverityLevel: 1,
		CreatedBy:     "user-1",
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	// Use the ID returned by Create (the in-memory store uses whatever ID is
	// on the record; with the current handler this will be the empty string).
	got, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: created.GetId()})
	if err != nil {
		t.Fatalf("GetSEV: %v", err)
	}
	if got.GetTitle() != "Network outage" {
		t.Errorf("Title = %q, want %q", got.GetTitle(), "Network outage")
	}
	if got.GetStatus() != string(store.SEVStatusOpen) {
		t.Errorf("Status = %q, want %q", got.GetStatus(), store.SEVStatusOpen)
	}
}

func TestGetSEV_UnknownID(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: "does-not-exist"})
	if err == nil {
		t.Fatal("GetSEV: want error for unknown ID, got nil")
	}
	if code := grpcCode(err); code != codes.NotFound {
		t.Errorf("error code = %v, want NotFound", code)
	}
}

// TestGetSEV_StoreError_LogsUnderlyingError is representative of the
// internalError conversion applied across internal/api/grpc/*.go (roadmap
// Phase 3): before, a store failure here surfaced only as a generic
// "failed to get SEV" — the underlying err was discarded entirely, so
// LoggingUnaryInterceptor's own log line never saw more than code=Internal
// either. Uses the erroringSEVStore fault injector already defined in
// share_view_test.go (same package) — the in-memory store's own Get only
// ever fails with store.ErrNotFound, which GetSEV handles as an expected
// 404, so a wrapper is needed to exercise the 500 path at all.
func TestGetSEV_StoreError_LogsUnderlyingError(t *testing.T) {
	buf := withCapturedDefaultLog(t)
	boom := errors.New("db exploded")
	server := grpchandler.NewSEVServer(grpchandler.SEVServerParams{
		SEVs: &erroringSEVStore{SEVStore: memory.NewSEVStore(), err: boom},
	})

	_, err := server.GetSEV(context.Background(), &pb.GetSEVRequest{Id: "sev-1"})
	if code := grpcCode(err); code != codes.Internal {
		t.Fatalf("error code = %v, want Internal", code)
	}
	if err.Error() != status.Error(codes.Internal, "failed to get SEV").Error() {
		t.Errorf("returned error = %v, want the generic message only (no leaked detail)", err)
	}

	fields := lastLogLine(t, buf)
	if fields["level"] != "ERROR" {
		t.Errorf("level = %v, want ERROR", fields["level"])
	}
	if fields["msg"] != "failed to get SEV" {
		t.Errorf("msg = %v, want %q", fields["msg"], "failed to get SEV")
	}
	if fields["err"] != "db exploded" {
		t.Errorf("err = %v, want the underlying store error to be logged", fields["err"])
	}
}

func TestGetSEV_SensitiveSEVHiddenFromCallerWithoutAccess(t *testing.T) {
	ts := newTestSEVServer()
	sevID := seedSensitiveSEV(t, ts)

	viewerCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-outsider", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.GetSEV(viewerCtx, &pb.GetSEVRequest{Id: sevID})
	if code := grpcCode(err); code != codes.NotFound {
		t.Errorf("error code = %v, want NotFound (masking existence, not PermissionDenied)", code)
	}
}

func TestGetSEV_SensitiveSEVVisibleToGrantedUser(t *testing.T) {
	ts := newTestSEVServer()
	sevID := seedSensitiveSEV(t, ts)

	if err := ts.access.Grant(context.Background(), &store.SEVAccess{SEVID: sevID, UserID: "user-granted", CreatedBy: "user-admin"}); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	grantedCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-granted", OrgRole: store.OrgRoleViewer})
	if _, err := ts.server.GetSEV(grantedCtx, &pb.GetSEVRequest{Id: sevID}); err != nil {
		t.Fatalf("GetSEV: %v", err)
	}
}

func TestGetSEV_SensitiveSEVVisibleToAdmin(t *testing.T) {
	ts := newTestSEVServer()
	sevID := seedSensitiveSEV(t, ts)

	if _, err := ts.server.GetSEV(adminCtx(), &pb.GetSEVRequest{Id: sevID}); err != nil {
		t.Fatalf("GetSEV as Admin: %v", err)
	}
}

func TestGetSEV_SensitiveSEVVisibleToIncidentCommander(t *testing.T) {
	ts := newTestSEVServer()
	sevID := seedSensitiveSEV(t, ts)

	icCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-ic", OrgRole: store.OrgRoleIncidentCommander})
	if _, err := ts.server.GetSEV(icCtx, &pb.GetSEVRequest{Id: sevID}); err != nil {
		t.Fatalf("GetSEV as Incident Commander: %v", err)
	}
}

// ── UpdateSEV ─────────────────────────────────────────────────────────────────

func TestUpdateSEV_Title(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	sevID := seedSEV(t, ts)

	resp, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{
		Id:    sevID,
		Title: "Updated title",
	})
	if err != nil {
		t.Fatalf("UpdateSEV: %v", err)
	}
	if resp.GetTitle() != "Updated title" {
		t.Errorf("response Title = %q, want %q", resp.GetTitle(), "Updated title")
	}

	// Verify the change is persisted — read it back through the handler.
	got, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: sevID})
	if err != nil {
		t.Fatalf("GetSEV after update: %v", err)
	}
	if got.GetTitle() != "Updated title" {
		t.Errorf("persisted Title = %q, want %q", got.GetTitle(), "Updated title")
	}
}

func TestUpdateSEV_DetectionMetadataAndLinks(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	resp, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{
		Id:              sevID,
		DetectionMethod: string(store.DetectionMethodSlackEscalation),
		MonitoringTool:  "other",
		AlertUrl:        "https://alerts.example.com/1",
		DashboardUrl:    "https://metrics.example.com/q/1",
		Query:           "up{job=\"checkout\"} == 0",
		SnapshotUrl:     "https://img.example.com/2.png",
		GithubRepo:      "acme-corp/checkout-service",
	})
	if err != nil {
		t.Fatalf("UpdateSEV: %v", err)
	}
	if got, want := resp.GetDetectionMethod(), string(store.DetectionMethodSlackEscalation); got != want {
		t.Errorf("DetectionMethod = %q, want %q", got, want)
	}
	if got, want := resp.GetMonitoringTool(), "other"; got != want {
		t.Errorf("MonitoringTool = %q, want %q", got, want)
	}
	if got, want := resp.GetAlertUrl(), "https://alerts.example.com/1"; got != want {
		t.Errorf("AlertUrl = %q, want %q", got, want)
	}
	if got, want := resp.GetDashboardUrl(), "https://metrics.example.com/q/1"; got != want {
		t.Errorf("DashboardUrl = %q, want %q", got, want)
	}
	if got, want := resp.GetQuery(), "up{job=\"checkout\"} == 0"; got != want {
		t.Errorf("Query = %q, want %q", got, want)
	}
	if got, want := resp.GetSnapshotUrl(), "https://img.example.com/2.png"; got != want {
		t.Errorf("SnapshotUrl = %q, want %q", got, want)
	}
	if got, want := resp.GetGithubRepo(), "acme-corp/checkout-service"; got != want {
		t.Errorf("GithubRepo = %q, want %q", got, want)
	}
}

func TestUpdateSEV_RootCauseReferenceURL(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	resp, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{
		Id:                    sevID,
		RootCauseCategory:     "deployment",
		RootCauseDescription:  "A bad rollout introduced a nil pointer.",
		RootCauseReferenceUrl: "https://github.com/acme-corp/checkout-service/pull/123",
	})
	if err != nil {
		t.Fatalf("UpdateSEV: %v", err)
	}
	if got, want := resp.GetRootCauseReferenceUrl(), "https://github.com/acme-corp/checkout-service/pull/123"; got != want {
		t.Errorf("RootCauseReferenceUrl = %q, want %q", got, want)
	}

	// Verify the change is persisted — read it back through the handler.
	got, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: sevID})
	if err != nil {
		t.Fatalf("GetSEV after update: %v", err)
	}
	if want := "https://github.com/acme-corp/checkout-service/pull/123"; got.GetRootCauseReferenceUrl() != want {
		t.Errorf("persisted RootCauseReferenceUrl = %q, want %q", got.GetRootCauseReferenceUrl(), want)
	}
}

func TestUpdateSEV_UnknownDetectionMethod(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	_, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{
		Id:              sevID,
		DetectionMethod: "smoke-signal",
	})
	if err == nil {
		t.Fatal("UpdateSEV: want error for unknown detection_method, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestUpdateSEV_UnknownMonitoringTool_Rejected(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	_, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{
		Id:             sevID,
		MonitoringTool: "new-relic",
	})
	if err == nil {
		t.Fatal("UpdateSEV: want error for unknown monitoring_tool, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestUpdateSEV_SensitiveSEVHiddenFromCallerWithoutAccess(t *testing.T) {
	ts := newTestSEVServer()
	sevID := seedSensitiveSEV(t, ts)

	viewerCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-outsider", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.UpdateSEV(viewerCtx, &pb.UpdateSEVRequest{Id: sevID, Title: "should not apply"})
	if code := grpcCode(err); code != codes.NotFound {
		t.Errorf("error code = %v, want NotFound (masking existence, not PermissionDenied)", code)
	}
}

// ── ListSEVs ──────────────────────────────────────────────────────────────────

func TestListSEVs_Empty(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.ListSEVs(ctx, &pb.ListSEVsRequest{})
	if err != nil {
		t.Fatalf("ListSEVs: %v", err)
	}
	if len(resp.GetSevs()) != 0 {
		t.Errorf("len(SEVs) = %d, want 0", len(resp.GetSevs()))
	}
}

func TestListSEVs_AfterCreate(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title:         "Memory leak",
		SeverityLevel: 3,
		CreatedBy:     "user-1",
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	resp, err := ts.server.ListSEVs(ctx, &pb.ListSEVsRequest{})
	if err != nil {
		t.Fatalf("ListSEVs: %v", err)
	}
	if len(resp.GetSevs()) != 1 {
		t.Errorf("len(SEVs) = %d, want 1", len(resp.GetSevs()))
	}
	if resp.GetTotal() != 1 {
		t.Errorf("Total = %d, want 1", resp.GetTotal())
	}
	if len(resp.GetSevs()) > 0 && resp.GetSevs()[0].GetTitle() != "Memory leak" {
		t.Errorf("SEV[0].Title = %q, want %q", resp.GetSevs()[0].GetTitle(), "Memory leak")
	}
}

func TestListSEVs_ExcludesSensitiveSEVsWithoutAccess(t *testing.T) {
	ts := newTestSEVServer()
	seedSensitiveSEV(t, ts)

	viewerCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-outsider", OrgRole: store.OrgRoleViewer})
	resp, err := ts.server.ListSEVs(viewerCtx, &pb.ListSEVsRequest{})
	if err != nil {
		t.Fatalf("ListSEVs: %v", err)
	}
	if len(resp.GetSevs()) != 0 {
		t.Errorf("len(SEVs) = %d, want 0 (sensitive SEV should be excluded)", len(resp.GetSevs()))
	}
	if resp.GetTotal() != 0 {
		t.Errorf("Total = %d, want 0", resp.GetTotal())
	}
}

func TestListSEVs_IncludesGrantedSensitiveSEVs(t *testing.T) {
	ts := newTestSEVServer()
	sevID := seedSensitiveSEV(t, ts)
	if err := ts.access.Grant(context.Background(), &store.SEVAccess{SEVID: sevID, UserID: "user-granted", CreatedBy: "user-admin"}); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	grantedCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-granted", OrgRole: store.OrgRoleViewer})
	resp, err := ts.server.ListSEVs(grantedCtx, &pb.ListSEVsRequest{})
	if err != nil {
		t.Fatalf("ListSEVs: %v", err)
	}
	if len(resp.GetSevs()) != 1 {
		t.Errorf("len(SEVs) = %d, want 1 (granted SEV should be included)", len(resp.GetSevs()))
	}
}

func TestListSEVs_AdminOrICSeesAllViaFastPath(t *testing.T) {
	ts := newTestSEVServer()
	seedSensitiveSEV(t, ts)

	resp, err := ts.server.ListSEVs(adminCtx(), &pb.ListSEVsRequest{})
	if err != nil {
		t.Fatalf("ListSEVs as Admin: %v", err)
	}
	if len(resp.GetSevs()) != 1 {
		t.Errorf("len(SEVs) = %d, want 1 (Admin sees all)", len(resp.GetSevs()))
	}

	icCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-ic", OrgRole: store.OrgRoleIncidentCommander})
	resp, err = ts.server.ListSEVs(icCtx, &pb.ListSEVsRequest{})
	if err != nil {
		t.Fatalf("ListSEVs as Incident Commander: %v", err)
	}
	if len(resp.GetSevs()) != 1 {
		t.Errorf("len(SEVs) = %d, want 1 (Incident Commander sees all)", len(resp.GetSevs()))
	}
}

func TestListSEVs_PaginationCorrectAfterFiltering(t *testing.T) {
	ts := newTestSEVServer()
	viewerCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-outsider", OrgRole: store.OrgRoleViewer})

	// Two visible SEVs and one sensitive SEV the caller can't see, seeded in
	// between so filtering can't accidentally line up with the page boundary
	// by coincidence.
	seedSEV(t, ts)
	seedSensitiveSEV(t, ts)
	seedSEV(t, ts)

	resp, err := ts.server.ListSEVs(viewerCtx, &pb.ListSEVsRequest{Limit: 10})
	if err != nil {
		t.Fatalf("ListSEVs: %v", err)
	}
	if resp.GetTotal() != 2 {
		t.Errorf("Total = %d, want 2 (post-filter count, not the raw 3 seeded)", resp.GetTotal())
	}
	if len(resp.GetSevs()) != 2 {
		t.Errorf("len(SEVs) = %d, want 2", len(resp.GetSevs()))
	}
	for _, sv := range resp.GetSevs() {
		if sv.GetSensitive() {
			t.Errorf("sensitive SEV %s leaked into a non-privileged caller's page", sv.GetId())
		}
	}
}

// ── TransitionStatus ──────────────────────────────────────────────────────────

func TestTransitionStatus_OpenToInvestigating(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	sevID := seedSEV(t, ts)

	resp, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:       sevID,
		ToStatus: string(store.SEVStatusInvestigating),
		UserId:   "user-1",
	})
	if err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}
	if resp.GetStatus() != string(store.SEVStatusInvestigating) {
		t.Errorf("Status = %q, want %q", resp.GetStatus(), store.SEVStatusInvestigating)
	}
}

func TestTransitionStatus_InvalidTransition(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	sevID := seedSEV(t, ts) // starts at Open

	// Open → Resolved is not an allowed transition.
	_, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:       sevID,
		ToStatus: string(store.SEVStatusResolved),
		UserId:   "user-1",
	})
	if err == nil {
		t.Fatal("TransitionStatus: want error for invalid transition Open→Resolved, got nil")
	}
	if code := grpcCode(err); code != codes.InvalidArgument {
		t.Errorf("error code = %v, want InvalidArgument", code)
	}
}

func TestTransitionStatus_UnknownSEV(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	_, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:       "SEV-9999-0001", // does not exist
		ToStatus: string(store.SEVStatusInvestigating),
		UserId:   "user-1",
	})
	if err == nil {
		t.Fatal("TransitionStatus: want error for unknown SEV, got nil")
	}
	if code := grpcCode(err); code != codes.NotFound {
		t.Errorf("error code = %v, want NotFound", code)
	}
}

func TestTransitionStatus_SensitiveSEVHiddenFromCallerWithoutAccess(t *testing.T) {
	ts := newTestSEVServer()
	sevID := seedSensitiveSEV(t, ts)

	viewerCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-outsider", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.TransitionStatus(viewerCtx, &pb.TransitionStatusRequest{
		Id: sevID, ToStatus: string(store.SEVStatusInvestigating),
	})
	if code := grpcCode(err); code != codes.NotFound {
		t.Errorf("error code = %v, want NotFound (masking existence, not PermissionDenied)", code)
	}
}

func TestTransitionStatus_AuditEntryCreated(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	sevID := seedSEV(t, ts)

	_, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id:       sevID,
		ToStatus: string(store.SEVStatusInvestigating),
		UserId:   "user-1",
	})
	if err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}

	entries, err := ts.audit.ListBySEVID(ctx, sevID)
	if err != nil {
		t.Fatalf("ListBySEVID: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("audit log is empty after TransitionStatus, want at least one entry")
	}

	found := false
	for _, e := range entries {
		if e.Action == "sev.status_transitioned" {
			found = true
			if e.SEVID != sevID {
				t.Errorf("audit entry SEVID = %q, want %q", e.SEVID, sevID)
			}
			break
		}
	}
	if !found {
		t.Errorf("no audit entry with action %q found in %d entries", "sev.status_transitioned", len(entries))
	}
}

// ── WebSocket event publishing ────────────────────────────────────────────────

func TestUpdateSEV_PublishesEvent(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: sevID, Title: "New title"}); err != nil {
		t.Fatalf("UpdateSEV: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "sev.updated" {
		t.Errorf("event = %+v, want sev_id=%q type=sev.updated", events[0], sevID)
	}
}

func TestTransitionStatus_PublishesEvent(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusInvestigating)}); err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "sev.status_changed" {
		t.Errorf("event = %+v, want sev_id=%q type=sev.status_changed", events[0], sevID)
	}
}

func seedSensitiveSEV(t *testing.T, ts *testSEVServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Sensitive SEV",
		SeverityLevel: 1,
		Status:        store.SEVStatusOpen,
		Sensitive:     true,
		CreatedBy:     "user-seed",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSensitiveSEV: %v", err)
	}
	return sv.ID
}

func TestUpdateSEV_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestSEVServer()
	ctx := adminCtx()
	sevID := seedSensitiveSEV(t, ts)

	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: sevID, Title: "New title"}); err != nil {
		t.Fatalf("UpdateSEV: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

func TestUpdateSEV_AutoGrantsCreatorOnSensitiveFlip(t *testing.T) {
	ts := newTestSEVServer()
	sevID := seedSEV(t, ts) // seedSEV's CreatedBy is "user-seed", not the flipper below

	if _, err := ts.server.UpdateSEV(adminCtx(), &pb.UpdateSEVRequest{
		Id: sevID, Sensitive: wrapperspb.Bool(true),
	}); err != nil {
		t.Fatalf("UpdateSEV: %v", err)
	}

	ok, err := ts.access.HasAccess(context.Background(), sevID, "user-seed")
	if err != nil {
		t.Fatalf("HasAccess: %v", err)
	}
	if !ok {
		t.Fatal("expected the original reporter (CreatedBy) to be auto-granted access on the sensitive flip")
	}

	// The flipper (admin) is not auto-granted — they already bypass the
	// check via their org role and don't need an explicit grant.
	flipperOk, err := ts.access.HasAccess(context.Background(), sevID, "user-admin")
	if err != nil {
		t.Fatalf("HasAccess: %v", err)
	}
	if flipperOk {
		t.Error("did not expect the flipper to be auto-granted a redundant explicit grant")
	}
}

func TestUpdateSEV_NoDuplicateGrantOnRepeatedFlip(t *testing.T) {
	ts := newTestSEVServer()
	sevID := seedSEV(t, ts)

	if _, err := ts.server.UpdateSEV(adminCtx(), &pb.UpdateSEVRequest{
		Id: sevID, Sensitive: wrapperspb.Bool(true),
	}); err != nil {
		t.Fatalf("UpdateSEV(first flip): %v", err)
	}
	// Flip back to false, then true again — re-flipping to true a second
	// time re-grants the same (sev_id, user_id) pair, which the store maps
	// to ErrConflict; the handler must swallow that, not fail the request.
	if _, err := ts.server.UpdateSEV(adminCtx(), &pb.UpdateSEVRequest{
		Id: sevID, Sensitive: wrapperspb.Bool(false),
	}); err != nil {
		t.Fatalf("UpdateSEV(un-flip): %v", err)
	}
	if _, err := ts.server.UpdateSEV(adminCtx(), &pb.UpdateSEVRequest{
		Id: sevID, Sensitive: wrapperspb.Bool(true),
	}); err != nil {
		t.Fatalf("UpdateSEV(re-flip): %v", err)
	}

	grants, err := ts.access.ListBySEVID(context.Background(), sevID)
	if err != nil {
		t.Fatalf("ListBySEVID: %v", err)
	}
	if len(grants) != 1 {
		t.Errorf("want exactly 1 grant for the reporter after repeated flips, got %d", len(grants))
	}
}

func TestTransitionStatus_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestSEVServer()
	ctx := adminCtx()
	sevID := seedSensitiveSEV(t, ts)

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusInvestigating)}); err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

// ── AI dispatch (§11.1, M12) ────────────────────────────────────────────────

func TestCreateSEV_DispatchesAIOnOpenForSEV1(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{Title: "db down", SeverityLevel: 1})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 1 || triggers[0].event != ai.TriggerSEVOpened || triggers[0].sevID != resp.GetId() {
		t.Fatalf("got triggers %+v, want one sev.opened for %s", triggers, resp.GetId())
	}
}

// TestCreateSEV_SensitiveSEVStillEnqueuesTrigger and
// TestCreateSEV_AIDisabledSEVStillEnqueuesTrigger: SEVServer.dispatchAI
// enqueues unconditionally — it deliberately does not re-implement the
// Sensitive/AIDisabled gate itself (see its doc comment). That gate is
// enforced once, centrally, by ai.Dispatcher against a freshly-fetched
// record (internal/ai/dispatcher_test.go's
// TestDispatch_SensitiveSEVSkipsProactiveTrigger /
// TestDispatch_AIDisabledSEVSkipsProactiveTrigger /
// TestDispatch_SensitiveAtExecutionTimeSkipsTrigger cover the actual
// skip-dispatch behavior); fakeAIDispatcher here is a stand-in for the gRPC
// layer's Dispatch call, not for that gate.
func TestCreateSEV_SensitiveSEVStillEnqueuesTrigger(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{Title: "sensitive", SeverityLevel: 1, Sensitive: true})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 1 || triggers[0].event != ai.TriggerSEVOpened || triggers[0].sevID != resp.GetId() {
		t.Errorf("got triggers %+v, want one sev.opened for %s", triggers, resp.GetId())
	}
}

func TestCreateSEV_AIDisabledSEVStillEnqueuesTrigger(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	resp, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{Title: "x", SeverityLevel: 1, AiDisabled: true})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 1 || triggers[0].event != ai.TriggerSEVOpened || triggers[0].sevID != resp.GetId() {
		t.Errorf("got triggers %+v, want one sev.opened for %s", triggers, resp.GetId())
	}
}

func TestTransitionStatus_DispatchesAIOnMitigatedAndResolved(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts) // starts Open, SEV-1

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusMitigated)}); err != nil {
		t.Fatalf("TransitionStatus to mitigated: %v", err)
	}
	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusResolved)}); err != nil {
		t.Fatalf("TransitionStatus to resolved: %v", err)
	}

	triggers := ts.ai.All()
	if len(triggers) != 2 || triggers[0].event != ai.TriggerSEVMitigated || triggers[1].event != ai.TriggerSEVResolved {
		t.Fatalf("got triggers %+v, want [sev.mitigated, sev.resolved]", triggers)
	}
}

func TestTransitionStatus_InvestigatingDoesNotDispatchAI(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()
	sevID := seedSEV(t, ts)

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{Id: sevID, ToStatus: string(store.SEVStatusInvestigating)}); err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}

	if triggers := ts.ai.All(); len(triggers) != 0 {
		t.Errorf("triggers = %+v, want none for a transition to investigating", triggers)
	}
}

// ── Recurrence auto-link (§17) ───────────────────────────────────────────────

func TestUpdateSEV_AutoLinksRecurrence_SameServiceAndCategory(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "First outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Second outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}

	links, err := ts.links.ListBySEVID(ctx, second.GetId())
	if err != nil {
		t.Fatalf("ListBySEVID: %v", err)
	}
	found := false
	for _, l := range links {
		if l.SourceSEVID == second.GetId() && l.TargetSEVID == first.GetId() && l.RelationshipType == store.SEVRelationshipRecurrenceOf {
			found = true
		}
	}
	if !found {
		t.Errorf("want a recurrence-of link from %s to %s, got %+v", second.GetId(), first.GetId(), links)
	}
}

func TestUpdateSEV_NoAutoLinkToSensitiveSEV(t *testing.T) {
	ts := newTestSEVServer()
	ctx := adminCtx()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Sensitive outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"}, Sensitive: true,
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Second outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}

	// A non-sensitive SEV must never get auto-linked to a Sensitive one —
	// that would surface the sensitive SEV's ID to anyone who can view the
	// new, non-sensitive record via ListLinkedSEVs.
	links, _ := ts.links.ListBySEVID(ctx, second.GetId())
	if len(links) != 0 {
		t.Errorf("want no auto-link to a sensitive SEV, got %+v", links)
	}
}

func TestUpdateSEV_NoAutoLinkForDifferentService(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "API outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "DB outage", SeverityLevel: 2, AffectedServices: []string{"svc-db"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}

	links, _ := ts.links.ListBySEVID(ctx, second.GetId())
	if len(links) != 0 {
		t.Errorf("want no auto-link for a different affected service, got %+v", links)
	}
}

func TestUpdateSEV_NoAutoLinkForDifferentCategory(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "First outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Second outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "hardware"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}

	links, _ := ts.links.ListBySEVID(ctx, second.GetId())
	if len(links) != 0 {
		t.Errorf("want no auto-link for a different root cause category, got %+v", links)
	}
}

func TestUpdateSEV_UnrelatedUpdateDoesNotReLink(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	first, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "First outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(first): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: first.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(first): %v", err)
	}

	second, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Second outage", SeverityLevel: 2, AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV(second): %v", err)
	}
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), RootCauseCategory: "deployment"}); err != nil {
		t.Fatalf("UpdateSEV(second): %v", err)
	}
	// An unrelated follow-up update (root cause category unchanged) must not
	// attempt to re-link (which would otherwise surface as a duplicate-link
	// error being silently swallowed — this asserts the guard that prevents
	// the attempt in the first place).
	if _, err := ts.server.UpdateSEV(ctx, &pb.UpdateSEVRequest{Id: second.GetId(), Mitigation: "rolled back the bad deploy"}); err != nil {
		t.Fatalf("UpdateSEV(second, unrelated): %v", err)
	}

	links, _ := ts.links.ListBySEVID(ctx, second.GetId())
	if len(links) != 1 {
		t.Errorf("want exactly 1 link after an unrelated update, got %+v", links)
	}
}

// ── SLA status (docs/roadmap.md Phase 12) ────────────────────────────────────

func TestGetSEV_SLAStatus_MostStrictAcrossMultipleServices(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	// checkout's MTTD target (300s) is stricter than payments' (600s); only
	// payments configures an MTTR target, so it applies unopposed.
	if err := ts.serviceSLAs.Upsert(ctx, &store.ServiceSLA{
		ServiceID: "checkout", SeverityLevel: 1, MTTDTargetSeconds: int64Ptr(300),
	}); err != nil {
		t.Fatalf("seed checkout SLA: %v", err)
	}
	if err := ts.serviceSLAs.Upsert(ctx, &store.ServiceSLA{
		ServiceID: "payments", SeverityLevel: 1, MTTDTargetSeconds: int64Ptr(600), MTTRTargetSeconds: int64Ptr(3600),
	}); err != nil {
		t.Fatalf("seed payments SLA: %v", err)
	}

	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Checkout errors", SeverityLevel: 1, AffectedServices: []string{"checkout", "payments"},
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	got, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: created.GetId()})
	if err != nil {
		t.Fatalf("GetSEV: %v", err)
	}
	sla := got.GetSlaStatus()
	if sla == nil {
		t.Fatal("SlaStatus is nil, want it populated")
	}
	if sla.GetMttdTargetSeconds() != 300 {
		t.Errorf("MttdTargetSeconds = %d, want 300 (strictest of checkout's 300 and payments' 600)", sla.GetMttdTargetSeconds())
	}
	if sla.GetMttrTargetSeconds() != 3600 {
		t.Errorf("MttrTargetSeconds = %d, want 3600 (only payments configures it)", sla.GetMttrTargetSeconds())
	}
	// started_at isn't set on this newly-created SEV, so there's no baseline
	// to measure elapsed time from yet — every metric is not_applicable
	// rather than falsely "at risk".
	if sla.GetMttd() != "not_applicable" {
		t.Errorf("Mttd = %q, want not_applicable (no started_at yet)", sla.GetMttd())
	}
}

func TestGetSEV_SLAStatus_AtRiskWhileStillOpen(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	if err := ts.serviceSLAs.Upsert(ctx, &store.ServiceSLA{
		ServiceID: "checkout", SeverityLevel: 1, MTTDTargetSeconds: int64Ptr(60),
	}); err != nil {
		t.Fatalf("seed SLA: %v", err)
	}

	started := time.Now().Add(-10 * time.Minute) // 600s elapsed, well past a 60s target
	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Slow checkout", SeverityLevel: 1, AffectedServices: []string{"checkout"},
		StartedAt: timestamppb.New(started),
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	got, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: created.GetId()})
	if err != nil {
		t.Fatalf("GetSEV: %v", err)
	}
	if got.GetSlaStatus().GetMttd() != "at_risk" {
		t.Errorf("Mttd = %q, want at_risk (still open, elapsed 600s > 60s target)", got.GetSlaStatus().GetMttd())
	}
	if got.GetSlaStatus().GetOverall() != "at_risk" {
		t.Errorf("Overall = %q, want at_risk", got.GetSlaStatus().GetOverall())
	}
}

func TestGetSEV_SLAStatus_BreachedOnceDetectedLate(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	if err := ts.serviceSLAs.Upsert(ctx, &store.ServiceSLA{
		ServiceID: "checkout", SeverityLevel: 1, MTTDTargetSeconds: int64Ptr(60),
	}); err != nil {
		t.Fatalf("seed SLA: %v", err)
	}

	started := time.Now().Add(-20 * time.Minute)
	detected := started.Add(10 * time.Minute) // 600s to detect, over the 60s target
	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Slow checkout", SeverityLevel: 1, AffectedServices: []string{"checkout"},
		StartedAt: timestamppb.New(started), DetectedAt: timestamppb.New(detected),
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}
	if created.GetSlaStatus().GetMttd() != "breached" {
		t.Errorf("Mttd = %q, want breached (final MTTD 600s > 60s target)", created.GetSlaStatus().GetMttd())
	}
}

func TestGetSEV_SLAStatus_NoAttachedServiceIsNotApplicable(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "No service attached", SeverityLevel: 1, StartedAt: timestamppb.New(time.Now().Add(-time.Hour)),
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}
	if created.GetSlaStatus() != nil {
		t.Errorf("SlaStatus = %+v, want nil with no affected_services", created.GetSlaStatus())
	}
}

func TestListSEVs_SLAStatusPopulated(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	if err := ts.serviceSLAs.Upsert(ctx, &store.ServiceSLA{
		ServiceID: "checkout", SeverityLevel: 1, MTTDTargetSeconds: int64Ptr(300),
	}); err != nil {
		t.Fatalf("seed SLA: %v", err)
	}
	if _, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Checkout errors", SeverityLevel: 1, AffectedServices: []string{"checkout"},
	}); err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	resp, err := ts.server.ListSEVs(ctx, &pb.ListSEVsRequest{})
	if err != nil {
		t.Fatalf("ListSEVs: %v", err)
	}
	if len(resp.GetSevs()) != 1 {
		t.Fatalf("want 1 SEV, got %d", len(resp.GetSevs()))
	}
	if resp.GetSevs()[0].GetSlaStatus().GetMttdTargetSeconds() != 300 {
		t.Errorf("MttdTargetSeconds = %d, want 300", resp.GetSevs()[0].GetSlaStatus().GetMttdTargetSeconds())
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestTransitionStatus_RTPCSecondsComputedOnPostmortemComplete(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{Title: "Outage", SeverityLevel: 2})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id: created.GetId(), ToStatus: string(store.SEVStatusMitigated),
	}); err != nil {
		t.Fatalf("TransitionStatus(Mitigated): %v", err)
	}
	resolvedAt := time.Now().Add(-2 * time.Hour)
	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id: created.GetId(), ToStatus: string(store.SEVStatusResolved), ResolvedAt: timestamppb.New(resolvedAt),
	}); err != nil {
		t.Fatalf("TransitionStatus(Resolved): %v", err)
	}
	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id: created.GetId(), ToStatus: string(store.SEVStatusPostmortemInProgress),
	}); err != nil {
		t.Fatalf("TransitionStatus(PostmortemInProgress): %v", err)
	}
	// PostmortemCompletedAt omitted — defaults to now, per
	// applyTransitionTimestamps' doc comment.
	resp, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id: created.GetId(), ToStatus: string(store.SEVStatusPostmortemComplete),
	})
	if err != nil {
		t.Fatalf("TransitionStatus(PostmortemComplete): %v", err)
	}

	if resp.GetRtpcSeconds() == 0 {
		t.Fatal("RtpcSeconds = 0, want computed value (postmortem_completed_at - resolved_at)")
	}
	// ~2 hours (7200s), allowing slack for test execution time.
	if resp.GetRtpcSeconds() < 7195 || resp.GetRtpcSeconds() > 7210 {
		t.Errorf("RtpcSeconds = %d, want ~7200 (2h since resolved_at)", resp.GetRtpcSeconds())
	}
}

func TestGetSEV_SLAStatus_RTPCUsesResolvedAtAsBaseline(t *testing.T) {
	ts := newTestSEVServer()
	ctx := context.Background()

	if err := ts.serviceSLAs.Upsert(ctx, &store.ServiceSLA{
		ServiceID: "checkout", SeverityLevel: 1, RTPCTargetSeconds: int64Ptr(86400), // 24h
	}); err != nil {
		t.Fatalf("seed SLA: %v", err)
	}

	created, err := ts.server.CreateSEV(ctx, &pb.CreateSEVRequest{
		Title: "Checkout errors", SeverityLevel: 1, AffectedServices: []string{"checkout"},
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id: created.GetId(), ToStatus: string(store.SEVStatusMitigated),
	}); err != nil {
		t.Fatalf("TransitionStatus(Mitigated): %v", err)
	}
	// Resolved only 10 minutes ago — well within the 24h RTPC target, even
	// though the SEV started 2 days ago. If RTPC were (incorrectly) measured
	// from started_at like MTTD/MTTM/MTTR, this would show breached.
	resolvedAt := time.Now().Add(-10 * time.Minute)
	if _, err := ts.server.TransitionStatus(ctx, &pb.TransitionStatusRequest{
		Id: created.GetId(), ToStatus: string(store.SEVStatusResolved), ResolvedAt: timestamppb.New(resolvedAt),
	}); err != nil {
		t.Fatalf("TransitionStatus(Resolved): %v", err)
	}

	got, err := ts.server.GetSEV(ctx, &pb.GetSEVRequest{Id: created.GetId()})
	if err != nil {
		t.Fatalf("GetSEV: %v", err)
	}
	if got.GetSlaStatus().GetRtpc() != "ok" {
		t.Errorf("Rtpc = %q, want ok (10m elapsed since resolved_at < 24h target)", got.GetSlaStatus().GetRtpc())
	}
}
