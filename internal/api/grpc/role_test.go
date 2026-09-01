package grpc_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// fakeSlackInviteClient is a grpchandler.SlackInviteClient that records
// InviteUsers calls and resolves emails from a fixed map, for
// InviteRoleToSlack tests.
type fakeSlackInviteClient struct {
	emailToUserID map[string]string
	invitedTo     string
	invitedUsers  []string
	inviteErr     error
}

func (f *fakeSlackInviteClient) InviteUsers(_ context.Context, channelID string, userIDs []string) error {
	f.invitedTo = channelID
	f.invitedUsers = append(f.invitedUsers, userIDs...)
	return f.inviteErr
}

func (f *fakeSlackInviteClient) LookupUserIDByEmail(_ context.Context, email string) (string, error) {
	return f.emailToUserID[email], nil
}

// testRoleServer bundles a RoleServer with its backing in-memory stores.
type testRoleServer struct {
	server       *grpchandler.RoleServer
	roles        *memory.RoleStore
	sevs         *memory.SEVStore
	access       *memory.SEVAccessStore
	audit        *memory.AuditStore
	pub          *fakePublisher
	users        *memory.UserStore
	integrations *memory.IntegrationConfigStore
	enc          grpchandler.Encryptor
}

// seedSlackConfig upserts a "slack" integration config with the given
// bot_token, encrypted with ts.enc — the credential InviteRoleToSlack reads.
func (ts *testRoleServer) seedSlackConfig(t *testing.T, botToken string) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"bot_token": botToken})
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	encrypted, err := ts.enc.Encrypt(raw)
	if err != nil {
		t.Fatalf("encrypt credentials: %v", err)
	}
	now := time.Now()
	if err := ts.integrations.Upsert(context.Background(), &store.IntegrationConfig{
		IntegrationType: "slack", EncryptedCredentials: encrypted, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed slack config: %v", err)
	}
}

func newTestRoleServer() *testRoleServer {
	roles := memory.NewRoleStore()
	sevs := memory.NewSEVStore()
	access := memory.NewSEVAccessStore()
	audit := memory.NewAuditStore()
	pub := &fakePublisher{}
	return &testRoleServer{
		server: grpchandler.NewRoleServer(grpchandler.RoleServerParams{Roles: roles, SEVs: sevs, Access: access, Audit: audit, Publisher: pub}),
		roles:  roles,
		sevs:   sevs,
		access: access,
		audit:  audit,
		pub:    pub,
	}
}

// newTestRoleServerWithSlack is like newTestRoleServer but also wires
// Users/Integrations/Crypto/SlackFactory, for InviteRoleToSlack tests. slack
// is the fake every SlackFactory call returns, regardless of botToken.
func newTestRoleServerWithSlack(t *testing.T, slack *fakeSlackInviteClient) *testRoleServer {
	t.Helper()
	roles := memory.NewRoleStore()
	sevs := memory.NewSEVStore()
	access := memory.NewSEVAccessStore()
	audit := memory.NewAuditStore()
	pub := &fakePublisher{}
	users := memory.NewUserStore()
	integrations := memory.NewIntegrationConfigStore()
	enc := testEncryptor(t)
	return &testRoleServer{
		server: grpchandler.NewRoleServer(grpchandler.RoleServerParams{
			Roles: roles, SEVs: sevs, Access: access, Audit: audit, Publisher: pub,
			Users: users, Integrations: integrations, Crypto: enc,
			SlackFactory: func(string) grpchandler.SlackInviteClient { return slack },
		}),
		roles:        roles,
		sevs:         sevs,
		access:       access,
		audit:        audit,
		pub:          pub,
		users:        users,
		integrations: integrations,
		enc:          enc,
	}
}

// seedSEVForRole inserts a SEV directly into the backing store and returns its ID.
func seedSEVForRole(t *testing.T, ts *testRoleServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title:         "Role test SEV",
		SeverityLevel: 1,
		Status:        store.SEVStatusOpen,
		CreatedBy:     "user-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSEVForRole: %v", err)
	}
	return sv.ID
}

// ── AssignRole ────────────────────────────────────────────────────────────────

func TestAssignRole_ValidRequest(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	resp, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId:       sevID,
		RoleType:    string(store.SEVRoleIncidentCommander),
		DisplayName: "Alice",
		UserId:      "usr-alice",
		CreatedBy:   "user-1",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if resp.GetId() == 0 {
		t.Error("ID should be set after assign")
	}
	if resp.GetSevId() != sevID {
		t.Errorf("SevId = %q, want %q", resp.GetSevId(), sevID)
	}
	if resp.GetRoleType() != string(store.SEVRoleIncidentCommander) {
		t.Errorf("RoleType = %q, want incident-commander", resp.GetRoleType())
	}
	if resp.GetDisplayName() != "Alice" {
		t.Errorf("DisplayName = %q, want Alice", resp.GetDisplayName())
	}
	if resp.GetUserId() != "usr-alice" {
		t.Errorf("UserId = %q, want usr-alice", resp.GetUserId())
	}
}

func TestAssignRole_MissingSEVID(t *testing.T) {
	ts := newTestRoleServer()
	_, err := ts.server.AssignRole(context.Background(), &pb.AssignRoleRequest{
		RoleType:    string(store.SEVRoleResponder),
		DisplayName: "Bob",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestAssignRole_MissingRoleType(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	_, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId:       sevID,
		DisplayName: "Bob",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestAssignRole_UnknownRoleType(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	_, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId:       sevID,
		RoleType:    "not-a-role",
		DisplayName: "Bob",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestAssignRole_SEVNotFound(t *testing.T) {
	ts := newTestRoleServer()
	_, err := ts.server.AssignRole(context.Background(), &pb.AssignRoleRequest{
		SevId:       "SEV-9999-0001",
		RoleType:    string(store.SEVRoleResponder),
		DisplayName: "Bob",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestAssignRole_MultipleRoles(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	roleTypes := []store.SEVRoleType{
		store.SEVRoleOnCall,
		store.SEVRoleIncidentCommander,
		store.SEVRoleResponder,
		store.SEVRoleResponder, // multiple responders is valid
	}
	for _, rt := range roleTypes {
		_, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
			SevId:       sevID,
			RoleType:    string(rt),
			DisplayName: "Responder",
		})
		if err != nil {
			t.Fatalf("AssignRole %s: %v", rt, err)
		}
	}

	listResp, err := ts.server.ListRoles(ctx, &pb.ListRolesRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(listResp.GetRoles()) != len(roleTypes) {
		t.Errorf("len(roles) = %d, want %d", len(listResp.GetRoles()), len(roleTypes))
	}
}

// ── RemoveRole ────────────────────────────────────────────────────────────────

func TestRemoveRole_ExistingRole(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	assigned, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId:       sevID,
		RoleType:    string(store.SEVRoleResponder),
		DisplayName: "Carol",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	_, err = ts.server.RemoveRole(ctx, &pb.RemoveRoleRequest{
		SevId: sevID,
		Id:    assigned.GetId(),
	})
	if err != nil {
		t.Fatalf("RemoveRole: %v", err)
	}

	listResp, _ := ts.server.ListRoles(ctx, &pb.ListRolesRequest{SevId: sevID})
	if len(listResp.GetRoles()) != 0 {
		t.Errorf("expected 0 roles after removal, got %d", len(listResp.GetRoles()))
	}
}

func TestRemoveRole_NotFound(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	_, err := ts.server.RemoveRole(ctx, &pb.RemoveRoleRequest{
		SevId: sevID,
		Id:    9999,
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestRemoveRole_MissingSEVID(t *testing.T) {
	ts := newTestRoleServer()
	_, err := ts.server.RemoveRole(context.Background(), &pb.RemoveRoleRequest{Id: 1})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestRemoveRole_AuditEntryCreated(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	assigned, _ := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId:       sevID,
		RoleType:    string(store.SEVRoleCommsLead),
		DisplayName: "Dave",
	})

	_, _ = ts.server.RemoveRole(ctx, &pb.RemoveRoleRequest{
		SevId: sevID,
		Id:    assigned.GetId(),
	})

	entries, _ := ts.audit.ListBySEVID(ctx, sevID)
	found := false
	for _, e := range entries {
		if e.Action == "role.removed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("no audit entry with action role.removed after RemoveRole")
	}
}

// ── ListRoles ─────────────────────────────────────────────────────────────────

func TestListRoles_Empty(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	resp, err := ts.server.ListRoles(ctx, &pb.ListRolesRequest{SevId: sevID})
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(resp.GetRoles()) != 0 {
		t.Errorf("want 0 roles, got %d", len(resp.GetRoles()))
	}
}

func TestListRoles_MissingSEVID(t *testing.T) {
	ts := newTestRoleServer()
	_, err := ts.server.ListRoles(context.Background(), &pb.ListRolesRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestListRoles_SEVNotFound(t *testing.T) {
	ts := newTestRoleServer()
	_, err := ts.server.ListRoles(context.Background(), &pb.ListRolesRequest{SevId: "SEV-9999-0001"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound for unknown SEV, got %v", grpcCode(err))
	}
}

func TestRemoveRole_WrongSEV(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	assigned, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId:       sevID,
		RoleType:    string(store.SEVRoleResponder),
		DisplayName: "Eve",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	// Seed a second SEV and try to delete the first SEV's role using the second SEV's ID.
	sevID2 := seedSEVForRole(t, ts)
	_, err = ts.server.RemoveRole(ctx, &pb.RemoveRoleRequest{
		SevId: sevID2,
		Id:    assigned.GetId(),
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound when role id belongs to different SEV, got %v", grpcCode(err))
	}

	// Confirm the original role is untouched.
	listResp, _ := ts.server.ListRoles(ctx, &pb.ListRolesRequest{SevId: sevID})
	if len(listResp.GetRoles()) != 1 {
		t.Errorf("original role should still exist, got %d roles", len(listResp.GetRoles()))
	}
}

// ── On-call auto-population via SEVServer ─────────────────────────────────────

// staticOnCaller is a test double that always returns a fixed display name.
type staticOnCaller struct{ displayName string }

func (s *staticOnCaller) OnCallLookup(_ context.Context, _ string) (string, error) {
	return s.displayName, nil
}

func TestCreateSEV_AutoPopulatesOnCallRole(t *testing.T) {
	roles := memory.NewRoleStore()
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	history := memory.NewStatusHistoryStore()
	services := memory.NewServiceStore()

	pdSvcID := "PD-SVC-001"
	svc := &store.Service{
		ID:                 "svc-api",
		Name:               "API Service",
		PagerDutyServiceID: &pdSvcID,
		Active:             true,
	}
	if err := services.Create(context.Background(), svc); err != nil {
		t.Fatalf("seed service: %v", err)
	}

	oc := &staticOnCaller{displayName: "Alice <alice@example.com>"}
	server := grpchandler.NewSEVServer(grpchandler.SEVServerParams{
		SEVs: sevs, Audit: audit, History: history, Roles: roles, Services: services,
		Postmortems: memory.NewPostmortemStore(), Links: memory.NewSEVLinkStore(), OnCaller: oc,
	})

	resp, err := server.CreateSEV(context.Background(), &pb.CreateSEVRequest{
		Title:            "API outage",
		SeverityLevel:    1,
		CreatedBy:        "user-1",
		AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	assigned, err := roles.ListBySEVID(context.Background(), resp.GetId())
	if err != nil {
		t.Fatalf("ListBySEVID: %v", err)
	}
	if len(assigned) != 1 {
		t.Fatalf("want 1 role assignment (on-call), got %d", len(assigned))
	}
	if assigned[0].RoleType != store.SEVRoleOnCall {
		t.Errorf("role_type = %q, want on-call", assigned[0].RoleType)
	}
	if assigned[0].DisplayName != "Alice <alice@example.com>" {
		t.Errorf("display_name = %q, want %q", assigned[0].DisplayName, "Alice <alice@example.com>")
	}
}

func TestCreateSEV_NoOnCallWhenIntegrationDisabled(t *testing.T) {
	roles := memory.NewRoleStore()
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	history := memory.NewStatusHistoryStore()
	services := memory.NewServiceStore()

	// OnCaller is nil — integration not configured
	server := grpchandler.NewSEVServer(grpchandler.SEVServerParams{
		SEVs: sevs, Audit: audit, History: history, Roles: roles, Services: services,
		Postmortems: memory.NewPostmortemStore(), Links: memory.NewSEVLinkStore(),
	})

	resp, err := server.CreateSEV(context.Background(), &pb.CreateSEVRequest{
		Title:            "API outage",
		SeverityLevel:    2,
		CreatedBy:        "user-1",
		AffectedServices: []string{"svc-api"},
	})
	if err != nil {
		t.Fatalf("CreateSEV: %v", err)
	}

	assigned, _ := roles.ListBySEVID(context.Background(), resp.GetId())
	if len(assigned) != 0 {
		t.Errorf("want 0 role assignments when on-call disabled, got %d", len(assigned))
	}
}

// ── WebSocket event publishing ────────────────────────────────────────────────

func TestAssignRole_PublishesEvent(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	_, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId:       sevID,
		RoleType:    string(store.SEVRoleResponder),
		DisplayName: "Carol",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1: %+v", len(events), events)
	}
	if events[0].sevID != sevID || events[0].eventType != "role.changed" {
		t.Errorf("event = %+v, want sev_id=%q type=role.changed", events[0], sevID)
	}
}

func TestRemoveRole_PublishesEvent(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSEVForRole(t, ts)

	assigned, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId:       sevID,
		RoleType:    string(store.SEVRoleResponder),
		DisplayName: "Carol",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	if _, err := ts.server.RemoveRole(ctx, &pb.RemoveRoleRequest{SevId: sevID, Id: assigned.GetId()}); err != nil {
		t.Fatalf("RemoveRole: %v", err)
	}

	events := ts.pub.All()
	if len(events) != 2 {
		t.Fatalf("published events = %d, want 2 (assign + remove): %+v", len(events), events)
	}
	last := events[1]
	if last.sevID != sevID || last.eventType != "role.changed" {
		t.Errorf("event = %+v, want sev_id=%q type=role.changed", last, sevID)
	}
}

func seedSensitiveSEVForRole(t *testing.T, ts *testRoleServer) string {
	t.Helper()
	now := time.Now()
	sv := &store.SEV{
		Title: "Sensitive SEV", SeverityLevel: 1, Status: store.SEVStatusOpen,
		Sensitive: true, CreatedBy: "user-1", CreatedAt: now, UpdatedAt: now,
	}
	if err := ts.sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("seedSensitiveSEVForRole: %v", err)
	}
	return sv.ID
}

func TestAssignRole_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSensitiveSEVForRole(t, ts)

	if _, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId: sevID, RoleType: string(store.SEVRoleResponder), DisplayName: "Carol",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

func TestRemoveRole_SensitiveSEVDoesNotPublish(t *testing.T) {
	ts := newTestRoleServer()
	ctx := context.Background()
	sevID := seedSensitiveSEVForRole(t, ts)

	assigned, err := ts.server.AssignRole(ctx, &pb.AssignRoleRequest{
		SevId: sevID, RoleType: string(store.SEVRoleResponder), DisplayName: "Carol",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if _, err := ts.server.RemoveRole(ctx, &pb.RemoveRoleRequest{SevId: sevID, Id: assigned.GetId()}); err != nil {
		t.Fatalf("RemoveRole: %v", err)
	}

	if events := ts.pub.All(); len(events) != 0 {
		t.Errorf("published events = %d, want 0 for a sensitive SEV: %+v", len(events), events)
	}
}

// ── InviteRoleToSlack ─────────────────────────────────────────────────────────

// mustSetSlackChannel fetches sevID, sets SlackChannelID to channelID, and
// persists it directly — a stand-in for Phase 10e's UpdateSEV write-back
// without going through the full SEVServer.
func mustSetSlackChannel(t *testing.T, ts *testRoleServer, sevID, channelID string) {
	t.Helper()
	sv, err := ts.sevs.Get(context.Background(), sevID)
	if err != nil {
		t.Fatalf("get SEV: %v", err)
	}
	sv.SlackChannelID = &channelID
	if err := ts.sevs.Update(context.Background(), sv); err != nil {
		t.Fatalf("set slack channel: %v", err)
	}
}

func TestInviteRoleToSlack_NoChannel_FailedPrecondition(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSEVForRole(t, ts)
	role, err := ts.server.AssignRole(context.Background(), &pb.AssignRoleRequest{
		SevId: sevID, RoleType: string(store.SEVRoleResponder), DisplayName: "Carol <carol@example.com>",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	_, err = ts.server.InviteRoleToSlack(context.Background(), &pb.InviteRoleToSlackRequest{SevId: sevID, Id: role.GetId()})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.FailedPrecondition)
	}
}

func TestInviteRoleToSlack_NotConfigured_Unavailable(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	// No seedSlackConfig call — nothing configured.
	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")
	role, err := ts.server.AssignRole(context.Background(), &pb.AssignRoleRequest{
		SevId: sevID, RoleType: string(store.SEVRoleResponder), DisplayName: "Carol <carol@example.com>",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	_, err = ts.server.InviteRoleToSlack(context.Background(), &pb.InviteRoleToSlackRequest{SevId: sevID, Id: role.GetId()})
	if grpcCode(err) != codes.Unavailable {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.Unavailable)
	}
}

func TestInviteRoleToSlack_ResolvesViaStoredSlackUserID(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	now := time.Now()
	if err := ts.users.Create(context.Background(), &store.User{
		ID: "user-1", Email: "carol@example.com", Name: "Carol",
		OrgRole: store.OrgRoleResponder, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	slackID := "U-STORED"
	if _, err := ts.users.UpdateIntegrationIdentities(context.Background(), "user-1", &slackID, nil, nil); err != nil {
		t.Fatalf("set slack id: %v", err)
	}

	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")
	role, err := ts.server.AssignRole(context.Background(), &pb.AssignRoleRequest{
		SevId: sevID, RoleType: string(store.SEVRoleResponder), UserId: "user-1", DisplayName: "Carol",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	if _, err := ts.server.InviteRoleToSlack(context.Background(), &pb.InviteRoleToSlackRequest{SevId: sevID, Id: role.GetId()}); err != nil {
		t.Fatalf("InviteRoleToSlack: %v", err)
	}
	if slack.invitedTo != "C123" || len(slack.invitedUsers) != 1 || slack.invitedUsers[0] != "U-STORED" {
		t.Errorf("invited (%q, %v), want (C123, [U-STORED])", slack.invitedTo, slack.invitedUsers)
	}
}

func TestInviteRoleToSlack_FallsBackToDisplayNameRegex(t *testing.T) {
	slack := &fakeSlackInviteClient{emailToUserID: map[string]string{"carol@example.com": "U-EMAIL"}}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")
	role, err := ts.server.AssignRole(context.Background(), &pb.AssignRoleRequest{
		SevId: sevID, RoleType: string(store.SEVRoleResponder), DisplayName: "Carol <carol@example.com>",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	if _, err := ts.server.InviteRoleToSlack(context.Background(), &pb.InviteRoleToSlackRequest{SevId: sevID, Id: role.GetId()}); err != nil {
		t.Fatalf("InviteRoleToSlack: %v", err)
	}
	if len(slack.invitedUsers) != 1 || slack.invitedUsers[0] != "U-EMAIL" {
		t.Errorf("invited users = %v, want [U-EMAIL]", slack.invitedUsers)
	}
}

func TestInviteRoleToSlack_NoResolvableIdentity_FailedPrecondition(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")
	role, err := ts.server.AssignRole(context.Background(), &pb.AssignRoleRequest{
		SevId: sevID, RoleType: string(store.SEVRoleResponder), DisplayName: "No Email Here",
	})
	if err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	_, err = ts.server.InviteRoleToSlack(context.Background(), &pb.InviteRoleToSlackRequest{SevId: sevID, Id: role.GetId()})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.FailedPrecondition)
	}
}

func TestInviteRoleToSlack_RoleNotFound(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")

	_, err := ts.server.InviteRoleToSlack(context.Background(), &pb.InviteRoleToSlackRequest{SevId: sevID, Id: 9999})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.NotFound)
	}
}

// ── JoinSlackChannel ──────────────────────────────────────────────────────────

func TestJoinSlackChannel_ResolvesViaStoredSlackUserID(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	now := time.Now()
	if err := ts.users.Create(context.Background(), &store.User{
		ID: "user-1", Email: "carol@example.com", Name: "Carol",
		OrgRole: store.OrgRoleViewer, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	slackID := "U-STORED"
	if _, err := ts.users.UpdateIntegrationIdentities(context.Background(), "user-1", &slackID, nil, nil); err != nil {
		t.Fatalf("set slack id: %v", err)
	}

	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")

	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-1", Email: "carol@example.com", OrgRole: store.OrgRoleViewer})
	if _, err := ts.server.JoinSlackChannel(ctx, &pb.JoinSlackChannelRequest{SevId: sevID}); err != nil {
		t.Fatalf("JoinSlackChannel: %v", err)
	}
	if slack.invitedTo != "C123" || len(slack.invitedUsers) != 1 || slack.invitedUsers[0] != "U-STORED" {
		t.Errorf("invited (%q, %v), want (C123, [U-STORED])", slack.invitedTo, slack.invitedUsers)
	}
}

func TestJoinSlackChannel_FallsBackToLookupByEmail(t *testing.T) {
	slack := &fakeSlackInviteClient{emailToUserID: map[string]string{"dave@example.com": "U-EMAIL"}}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")

	// No stored user record at all — falls back to the caller's own
	// UserContext.Email straight away.
	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-2", Email: "dave@example.com", OrgRole: store.OrgRoleViewer})
	if _, err := ts.server.JoinSlackChannel(ctx, &pb.JoinSlackChannelRequest{SevId: sevID}); err != nil {
		t.Fatalf("JoinSlackChannel: %v", err)
	}
	if len(slack.invitedUsers) != 1 || slack.invitedUsers[0] != "U-EMAIL" {
		t.Errorf("invited users = %v, want [U-EMAIL]", slack.invitedUsers)
	}
}

func TestJoinSlackChannel_NoResolvableIdentity_FailedPrecondition(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")

	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-3", Email: "", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.JoinSlackChannel(ctx, &pb.JoinSlackChannelRequest{SevId: sevID})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.FailedPrecondition)
	}
}

func TestJoinSlackChannel_NoChannel_FailedPrecondition(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSEVForRole(t, ts)

	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-1", Email: "carol@example.com", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.JoinSlackChannel(ctx, &pb.JoinSlackChannelRequest{SevId: sevID})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.FailedPrecondition)
	}
}

func TestJoinSlackChannel_NotConfigured_Unavailable(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")

	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-1", Email: "carol@example.com", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.JoinSlackChannel(ctx, &pb.JoinSlackChannelRequest{SevId: sevID})
	if grpcCode(err) != codes.Unavailable {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.Unavailable)
	}
}

func TestJoinSlackChannel_SevNotFound(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")

	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-1", Email: "carol@example.com", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.JoinSlackChannel(ctx, &pb.JoinSlackChannelRequest{SevId: "SEV-9999-0001"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.NotFound)
	}
}

func TestJoinSlackChannel_Unauthenticated(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")

	_, err := ts.server.JoinSlackChannel(context.Background(), &pb.JoinSlackChannelRequest{SevId: sevID})
	if grpcCode(err) != codes.Unauthenticated {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.Unauthenticated)
	}
}

func TestJoinSlackChannel_SensitiveSEVWithoutAccess_PermissionDenied(t *testing.T) {
	slack := &fakeSlackInviteClient{}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSensitiveSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")

	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-outsider", Email: "outsider@example.com", OrgRole: store.OrgRoleViewer})
	_, err := ts.server.JoinSlackChannel(ctx, &pb.JoinSlackChannelRequest{SevId: sevID})
	if grpcCode(err) != codes.PermissionDenied {
		t.Errorf("code = %v, want %v", grpcCode(err), codes.PermissionDenied)
	}
}

func TestJoinSlackChannel_SensitiveSEVWithGrantedAccess_Succeeds(t *testing.T) {
	slack := &fakeSlackInviteClient{emailToUserID: map[string]string{"granted@example.com": "U-GRANTED"}}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSensitiveSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")

	if err := ts.access.Grant(context.Background(), &store.SEVAccess{
		SEVID: sevID, UserID: "user-granted", CreatedAt: time.Now(), CreatedBy: "user-1",
	}); err != nil {
		t.Fatalf("grant access: %v", err)
	}

	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-granted", Email: "granted@example.com", OrgRole: store.OrgRoleViewer})
	if _, err := ts.server.JoinSlackChannel(ctx, &pb.JoinSlackChannelRequest{SevId: sevID}); err != nil {
		t.Fatalf("JoinSlackChannel: %v", err)
	}
	if len(slack.invitedUsers) != 1 || slack.invitedUsers[0] != "U-GRANTED" {
		t.Errorf("invited users = %v, want [U-GRANTED]", slack.invitedUsers)
	}
}

func TestJoinSlackChannel_SensitiveSEVIncidentCommanderBypass(t *testing.T) {
	slack := &fakeSlackInviteClient{emailToUserID: map[string]string{"ic@example.com": "U-IC"}}
	ts := newTestRoleServerWithSlack(t, slack)
	ts.seedSlackConfig(t, "xoxb-1")
	sevID := seedSensitiveSEVForRole(t, ts)
	mustSetSlackChannel(t, ts, sevID, "C123")

	// No explicit grant — an Incident Commander can see any Sensitive SEV.
	ctx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-ic", Email: "ic@example.com", OrgRole: store.OrgRoleIncidentCommander})
	if _, err := ts.server.JoinSlackChannel(ctx, &pb.JoinSlackChannelRequest{SevId: sevID}); err != nil {
		t.Fatalf("JoinSlackChannel: %v", err)
	}
	if len(slack.invitedUsers) != 1 || slack.invitedUsers[0] != "U-IC" {
		t.Errorf("invited users = %v, want [U-IC]", slack.invitedUsers)
	}
}
