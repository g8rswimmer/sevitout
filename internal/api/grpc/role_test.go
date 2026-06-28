package grpc_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// testRoleServer bundles a RoleServer with its backing in-memory stores.
type testRoleServer struct {
	server *grpchandler.RoleServer
	roles  *memory.RoleStore
	sevs   *memory.SEVStore
	audit  *memory.AuditStore
}

func newTestRoleServer() *testRoleServer {
	roles := memory.NewRoleStore()
	sevs := memory.NewSEVStore()
	audit := memory.NewAuditStore()
	return &testRoleServer{
		server: grpchandler.NewRoleServer(roles, sevs, audit),
		roles:  roles,
		sevs:   sevs,
		audit:  audit,
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
	server := grpchandler.NewSEVServer(sevs, audit, history, roles, services, oc)

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

	// onCaller is nil — integration not configured
	server := grpchandler.NewSEVServer(sevs, audit, history, roles, services, nil)

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
