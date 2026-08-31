package grpc_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
	"github.com/g8rswimmer/sevitout/internal/auth"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

type testConfigServer struct {
	server       *grpchandler.ConfigServer
	services     *memory.ServiceStore
	users        *memory.UserStore
	oncall       *memory.OnCallStore
	integrations *memory.IntegrationConfigStore
	retention    *memory.RetentionConfigStore
	aiPlugins    *memory.AIPluginStore
}

func newTestConfigServer(enc grpchandler.Encryptor) *testConfigServer {
	services := memory.NewServiceStore()
	users := memory.NewUserStore()
	oncall := memory.NewOnCallStore()
	integrations := memory.NewIntegrationConfigStore()
	retention := memory.NewRetentionConfigStore()
	aiPlugins := memory.NewAIPluginStore()
	return &testConfigServer{
		server: grpchandler.NewConfigServer(grpchandler.ConfigServerParams{
			Services: services, Users: users, OnCall: oncall, Integrations: integrations,
			Retention: retention, AIPlugins: aiPlugins, Crypto: enc,
		}),
		services:     services,
		users:        users,
		oncall:       oncall,
		integrations: integrations,
		retention:    retention,
		aiPlugins:    aiPlugins,
	}
}

func testEncryptor(t *testing.T) grpchandler.Encryptor {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return crypto.NewKeyEncryptor(key)
}

// ── Service registry ─────────────────────────────────────────────────────────

func TestCreateService_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	resp, err := ts.server.CreateService(context.Background(), &pb.CreateServiceRequest{
		Id:   "checkout",
		Name: "Checkout",
		Tags: map[string]string{"team": "commerce"},
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if !resp.GetActive() {
		t.Error("a newly created service should be active")
	}
	if resp.GetTags()["team"] != "commerce" {
		t.Errorf("tags = %v", resp.GetTags())
	}
}

func TestCreateService_MissingID(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.CreateService(context.Background(), &pb.CreateServiceRequest{Name: "Checkout"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestCreateService_DuplicateID(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	req := &pb.CreateServiceRequest{Id: "checkout", Name: "Checkout"}
	if _, err := ts.server.CreateService(ctx, req); err != nil {
		t.Fatalf("first CreateService: %v", err)
	}
	_, err := ts.server.CreateService(ctx, req)
	if grpcCode(err) != codes.AlreadyExists {
		t.Errorf("want AlreadyExists, got %v", grpcCode(err))
	}
}

func TestGetService_NotFound(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.GetService(context.Background(), &pb.GetServiceRequest{Id: "missing"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestUpdateService_DeactivatePreservesRecord(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	if _, err := ts.server.CreateService(ctx, &pb.CreateServiceRequest{Id: "checkout", Name: "Checkout"}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	resp, err := ts.server.UpdateService(ctx, &pb.UpdateServiceRequest{
		Id:     "checkout",
		Active: wrapperspb.Bool(false),
	})
	if err != nil {
		t.Fatalf("UpdateService: %v", err)
	}
	if resp.GetActive() {
		t.Error("service should be inactive after deactivation")
	}
	if resp.GetName() != "Checkout" {
		t.Error("deactivation should not alter other fields")
	}

	// The record itself still exists (deactivating preserves history — see
	// docs/requirements.md §18.1) — it's just excluded by active_only listing.
	if _, err := ts.server.GetService(ctx, &pb.GetServiceRequest{Id: "checkout"}); err != nil {
		t.Errorf("GetService after deactivate: %v", err)
	}
}

func TestListServices_ActiveOnlyFilter(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	mustCreateService(t, ts, "checkout", "Checkout")
	mustCreateService(t, ts, "billing", "Billing")
	if _, err := ts.server.UpdateService(ctx, &pb.UpdateServiceRequest{Id: "billing", Active: wrapperspb.Bool(false)}); err != nil {
		t.Fatalf("UpdateService: %v", err)
	}

	all, err := ts.server.ListServices(ctx, &pb.ListServicesRequest{})
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(all.GetServices()) != 2 {
		t.Fatalf("want 2 services total, got %d", len(all.GetServices()))
	}

	active, err := ts.server.ListServices(ctx, &pb.ListServicesRequest{ActiveOnly: true})
	if err != nil {
		t.Fatalf("ListServices active_only: %v", err)
	}
	if len(active.GetServices()) != 1 || active.GetServices()[0].GetId() != "checkout" {
		t.Errorf("active_only should return just checkout, got %+v", active.GetServices())
	}
}

func TestDeleteService_NotFound(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.DeleteService(context.Background(), &pb.DeleteServiceRequest{Id: "missing"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestDeleteService_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	mustCreateService(t, ts, "checkout", "Checkout")
	if _, err := ts.server.DeleteService(ctx, &pb.DeleteServiceRequest{Id: "checkout"}); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}
	if _, err := ts.server.GetService(ctx, &pb.GetServiceRequest{Id: "checkout"}); grpcCode(err) != codes.NotFound {
		t.Errorf("service should be gone after delete, got %v", grpcCode(err))
	}
}

func mustCreateService(t *testing.T, ts *testConfigServer, id, name string) {
	t.Helper()
	if _, err := ts.server.CreateService(context.Background(), &pb.CreateServiceRequest{Id: id, Name: name}); err != nil {
		t.Fatalf("CreateService(%s): %v", id, err)
	}
}

// ── User management ──────────────────────────────────────────────────────────

func seedUser(t *testing.T, ts *testConfigServer, id, email, name string, role store.OrgRole) {
	t.Helper()
	now := time.Now()
	u := &store.User{ID: id, Email: email, Name: name, OrgRole: role, Active: true, CreatedAt: now, UpdatedAt: now}
	if err := ts.users.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

func TestListUsers_QueryFiltersByNameOrEmail(t *testing.T) {
	ts := newTestConfigServer(nil)
	seedUser(t, ts, "u1", "alice@example.com", "Alice", store.OrgRoleViewer)
	seedUser(t, ts, "u2", "bob@example.com", "Bob", store.OrgRoleResponder)

	resp, err := ts.server.ListUsers(context.Background(), &pb.ListUsersRequest{Query: "alice"})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(resp.GetUsers()) != 1 || resp.GetUsers()[0].GetId() != "u1" {
		t.Errorf("query \"alice\" should match only u1, got %+v", resp.GetUsers())
	}

	resp, err = ts.server.ListUsers(context.Background(), &pb.ListUsersRequest{Query: "example.com"})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(resp.GetUsers()) != 2 {
		t.Errorf("query \"example.com\" should match both users, got %d", len(resp.GetUsers()))
	}
}

func TestUpdateUserRole_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	seedUser(t, ts, "u1", "alice@example.com", "Alice", store.OrgRoleViewer)

	resp, err := ts.server.UpdateUserRole(context.Background(), &pb.UpdateUserRoleRequest{Id: "u1", OrgRole: "admin"})
	if err != nil {
		t.Fatalf("UpdateUserRole: %v", err)
	}
	if resp.GetOrgRole() != "admin" {
		t.Errorf("org_role = %q, want admin", resp.GetOrgRole())
	}
}

func TestUpdateUserRole_InvalidRole(t *testing.T) {
	ts := newTestConfigServer(nil)
	seedUser(t, ts, "u1", "alice@example.com", "Alice", store.OrgRoleViewer)

	_, err := ts.server.UpdateUserRole(context.Background(), &pb.UpdateUserRoleRequest{Id: "u1", OrgRole: "superadmin"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestUpdateUserRole_NotFound(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpdateUserRole(context.Background(), &pb.UpdateUserRoleRequest{Id: "missing", OrgRole: "admin"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestDeactivateAndReactivateUser(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	seedUser(t, ts, "u1", "alice@example.com", "Alice", store.OrgRoleViewer)

	deactivated, err := ts.server.DeactivateUser(ctx, &pb.DeactivateUserRequest{Id: "u1"})
	if err != nil {
		t.Fatalf("DeactivateUser: %v", err)
	}
	if deactivated.GetActive() {
		t.Error("user should be inactive after DeactivateUser")
	}

	reactivated, err := ts.server.ReactivateUser(ctx, &pb.ReactivateUserRequest{Id: "u1"})
	if err != nil {
		t.Fatalf("ReactivateUser: %v", err)
	}
	if !reactivated.GetActive() {
		t.Error("user should be active after ReactivateUser")
	}
}

// ── On-call configuration ────────────────────────────────────────────────────

func TestCreateOnCallRotation_ManualEntry(t *testing.T) {
	ts := newTestConfigServer(nil)
	start := time.Now().Add(1 * time.Hour)
	end := time.Now().Add(2 * time.Hour)

	resp, err := ts.server.CreateOnCallRotation(context.Background(), &pb.CreateOnCallRotationRequest{
		Name:              "Holiday override",
		ServiceId:         "checkout",
		ManualUserId:      "user-1",
		ManualDisplayName: "Alice",
		OverrideStart:     timestamppb.New(start),
		OverrideEnd:       timestamppb.New(end),
	})
	if err != nil {
		t.Fatalf("CreateOnCallRotation: %v", err)
	}
	if resp.GetManualDisplayName() != "Alice" {
		t.Errorf("manual_display_name = %q, want Alice", resp.GetManualDisplayName())
	}
	if resp.GetOverrideStart() == nil || resp.GetOverrideEnd() == nil {
		t.Error("override window should be set")
	}
}

func TestCreateOnCallRotation_InvalidOverrideWindow(t *testing.T) {
	ts := newTestConfigServer(nil)
	start := time.Now().Add(2 * time.Hour)
	end := time.Now().Add(1 * time.Hour) // end before start

	_, err := ts.server.CreateOnCallRotation(context.Background(), &pb.CreateOnCallRotationRequest{
		Name:          "Bad window",
		OverrideStart: timestamppb.New(start),
		OverrideEnd:   timestamppb.New(end),
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for override_end before override_start, got %v", grpcCode(err))
	}
}

// A partial update supplying only override_start must still be validated
// against the existing stored override_end — not just against the (possibly
// nil) fields present on the request in isolation.
func TestUpdateOnCallRotation_PartialUpdateCannotProduceInvertedWindow(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()

	start := time.Now().Add(1 * time.Hour)
	end := time.Now().Add(2 * time.Hour)
	created, err := ts.server.CreateOnCallRotation(ctx, &pb.CreateOnCallRotationRequest{
		Name:          "Holiday override",
		OverrideStart: timestamppb.New(start),
		OverrideEnd:   timestamppb.New(end),
	})
	if err != nil {
		t.Fatalf("CreateOnCallRotation: %v", err)
	}

	// Supply only a new override_start, after the existing override_end.
	newStart := end.Add(1 * time.Hour)
	_, err = ts.server.UpdateOnCallRotation(ctx, &pb.UpdateOnCallRotationRequest{
		Id:            created.GetId(),
		OverrideStart: timestamppb.New(newStart),
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument when the merged window has start after the existing end, got %v", grpcCode(err))
	}

	// The invalid update must not have been persisted.
	got, gerr := ts.server.GetOnCallRotation(ctx, &pb.GetOnCallRotationRequest{Id: created.GetId()})
	if gerr != nil {
		t.Fatalf("GetOnCallRotation: %v", gerr)
	}
	if !got.GetOverrideStart().AsTime().Equal(start) {
		t.Errorf("override_start should be unchanged after a rejected update, got %v", got.GetOverrideStart().AsTime())
	}
}

func TestCreateOnCallRotation_MissingName(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.CreateOnCallRotation(context.Background(), &pb.CreateOnCallRotationRequest{})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestOnCallRotation_UpdateAndDelete(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()

	created, err := ts.server.CreateOnCallRotation(ctx, &pb.CreateOnCallRotationRequest{Name: "Primary", ServiceId: "checkout"})
	if err != nil {
		t.Fatalf("CreateOnCallRotation: %v", err)
	}

	updated, err := ts.server.UpdateOnCallRotation(ctx, &pb.UpdateOnCallRotationRequest{
		Id:                  created.GetId(),
		PagerdutyScheduleId: "PSCHED123",
	})
	if err != nil {
		t.Fatalf("UpdateOnCallRotation: %v", err)
	}
	if updated.GetPagerdutyScheduleId() != "PSCHED123" {
		t.Errorf("pagerduty_schedule_id = %q, want PSCHED123", updated.GetPagerdutyScheduleId())
	}
	if updated.GetName() != "Primary" {
		t.Error("update should not clear untouched fields")
	}

	if _, err := ts.server.DeleteOnCallRotation(ctx, &pb.DeleteOnCallRotationRequest{Id: created.GetId()}); err != nil {
		t.Fatalf("DeleteOnCallRotation: %v", err)
	}
	if _, err := ts.server.GetOnCallRotation(ctx, &pb.GetOnCallRotationRequest{Id: created.GetId()}); grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound after delete, got %v", grpcCode(err))
	}
}

func TestListOnCallRotations(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	if _, err := ts.server.CreateOnCallRotation(ctx, &pb.CreateOnCallRotationRequest{Name: "A"}); err != nil {
		t.Fatalf("CreateOnCallRotation: %v", err)
	}
	if _, err := ts.server.CreateOnCallRotation(ctx, &pb.CreateOnCallRotationRequest{Name: "B"}); err != nil {
		t.Fatalf("CreateOnCallRotation: %v", err)
	}
	resp, err := ts.server.ListOnCallRotations(ctx, &pb.ListOnCallRotationsRequest{})
	if err != nil {
		t.Fatalf("ListOnCallRotations: %v", err)
	}
	if len(resp.GetRotations()) != 2 {
		t.Errorf("want 2 rotations, got %d", len(resp.GetRotations()))
	}
}

// ── Integration configuration ────────────────────────────────────────────────

func TestUpsertIntegrationConfig_EncryptsCredentials(t *testing.T) {
	enc := testEncryptor(t)
	ts := newTestConfigServer(enc)
	ctx := context.Background()

	resp, err := ts.server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "pagerduty",
		Credentials:     map[string]string{"api_key": "pd_super_secret"},
		Settings:        map[string]string{"default_escalation_policy": "P123"},
	})
	if err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}
	if !resp.GetCredentialsConfigured() {
		t.Error("credentials_configured should be true")
	}
	if resp.GetSettings()["default_escalation_policy"] != "P123" {
		t.Errorf("settings = %v", resp.GetSettings())
	}

	// The response must never carry the plaintext credential value anywhere.
	if bytes.Contains([]byte(resp.String()), []byte("pd_super_secret")) {
		t.Fatal("response leaked the plaintext credential")
	}

	// The store must hold ciphertext, not plaintext.
	stored, err := ts.integrations.Get(ctx, "pagerduty")
	if err != nil {
		t.Fatalf("Get from store: %v", err)
	}
	if bytes.Contains(stored.EncryptedCredentials, []byte("pd_super_secret")) {
		t.Fatal("stored credentials are not encrypted")
	}

	// Round trip: decrypting what was stored recovers the original credential.
	creds, err := grpchandler.DecryptIntegrationCredentials(enc, stored)
	if err != nil {
		t.Fatalf("DecryptIntegrationCredentials: %v", err)
	}
	if creds["api_key"] != "pd_super_secret" {
		t.Errorf("decrypted api_key = %q, want pd_super_secret", creds["api_key"])
	}
}

// fakeCredentialsRefresher records every RefreshIntegrationCredentials call
// it receives — including the plaintext credentials/settings handed to it —
// letting tests assert ConfigServer notifies its configured Refreshers (see
// cmd/server's OnCaller/IssueClient/JiraIssueClient *Resolver types, the
// real implementations) with the correct data, and optionally returns a
// canned error to exercise UpsertIntegrationConfig's
// reject-before-persisting path.
type fakeCredentialsRefresher struct {
	err   error
	calls []refresherCall
}

type refresherCall struct {
	integrationType string
	credentials     map[string]string
	settings        map[string]any
}

func (f *fakeCredentialsRefresher) RefreshIntegrationCredentials(_ context.Context, integrationType string, credentials map[string]string, settings map[string]any) error {
	f.calls = append(f.calls, refresherCall{integrationType, credentials, settings})
	return f.err
}

func TestUpsertIntegrationConfig_NotifiesRefreshersWithPlaintextCredentials(t *testing.T) {
	enc := testEncryptor(t)
	integrations := memory.NewIntegrationConfigStore()
	refresherA := &fakeCredentialsRefresher{}
	refresherB := &fakeCredentialsRefresher{}
	server := grpchandler.NewConfigServer(grpchandler.ConfigServerParams{
		Integrations: integrations,
		Crypto:       enc,
		Refreshers:   []grpchandler.IntegrationCredentialsRefresher{refresherA, refresherB},
	})

	if _, err := server.UpsertIntegrationConfig(context.Background(), &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "pagerduty",
		Credentials:     map[string]string{"api_key": "pd_super_secret"},
	}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}

	for name, r := range map[string]*fakeCredentialsRefresher{"A": refresherA, "B": refresherB} {
		if len(r.calls) != 1 {
			t.Fatalf("refresher %s calls = %v, want exactly one call", name, r.calls)
		}
		call := r.calls[0]
		if call.integrationType != "pagerduty" || call.credentials["api_key"] != "pd_super_secret" {
			t.Errorf("refresher %s received %+v, want integration_type=pagerduty, api_key=pd_super_secret", name, call)
		}
	}
}

func TestUpsertIntegrationConfig_RefreshRejects_NewIntegrationType_RollsBackToNoCredentials(t *testing.T) {
	enc := testEncryptor(t)
	integrations := memory.NewIntegrationConfigStore()
	refresher := &fakeCredentialsRefresher{err: errors.New("missing required field")}
	server := grpchandler.NewConfigServer(grpchandler.ConfigServerParams{
		Integrations: integrations,
		Crypto:       enc,
		Refreshers:   []grpchandler.IntegrationCredentialsRefresher{refresher},
	})

	_, err := server.UpsertIntegrationConfig(context.Background(), &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "pagerduty",
		Credentials:     map[string]string{"api_key": "pd_super_secret"},
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("UpsertIntegrationConfig err = %v, want InvalidArgument when a refresher rejects the config", err)
	}

	// Refreshers only run after the write is durable (so a resolver never
	// applies credentials that could still fail to persist) — a rejection
	// therefore rolls the row back to "no credentials" rather than leaving
	// no trace at all, since there was no previous row to restore.
	stored, getErr := integrations.Get(context.Background(), "pagerduty")
	if getErr != nil {
		t.Fatalf("Get after a rejected write: %v", getErr)
	}
	if len(stored.EncryptedCredentials) != 0 {
		t.Error("a rejected write for a brand-new integration_type must roll back to no credentials, not retain the rejected ones")
	}
}

func TestUpsertIntegrationConfig_RefreshRejects_ExistingIntegrationType_PreviousConfigUnchanged(t *testing.T) {
	enc := testEncryptor(t)
	integrations := memory.NewIntegrationConfigStore()
	refresher := &fakeCredentialsRefresher{}
	server := grpchandler.NewConfigServer(grpchandler.ConfigServerParams{
		Integrations: integrations,
		Crypto:       enc,
		Refreshers:   []grpchandler.IntegrationCredentialsRefresher{refresher},
	})
	ctx := context.Background()

	// A valid config is saved first (refresher accepts it).
	if _, err := server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "github",
		Credentials:     map[string]string{"token": "ghp_original"},
	}); err != nil {
		t.Fatalf("initial UpsertIntegrationConfig: %v", err)
	}
	original, err := integrations.Get(ctx, "github")
	if err != nil {
		t.Fatalf("Get original: %v", err)
	}

	// A second write with a credential the refresher rejects must be rolled
	// back after the fact — the original row ends up exactly as it was.
	refresher.err = errors.New("missing required field")
	_, err = server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "github",
		Credentials:     map[string]string{"token": "ghp_bad"},
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Fatalf("UpsertIntegrationConfig err = %v, want InvalidArgument when a refresher rejects the config", err)
	}

	stored, err := integrations.Get(ctx, "github")
	if err != nil {
		t.Fatalf("Get after rejected write: %v", err)
	}
	if !bytes.Equal(stored.EncryptedCredentials, original.EncryptedCredentials) {
		t.Error("a rejected write must leave the previously stored credentials untouched")
	}
}

// TestUpsertIntegrationConfig_SettingsOnlyUpdate_RefresherSeesExistingCredentials
// covers the one place UpsertIntegrationConfig still decrypts anything after
// startup: a request that only changes settings (no credentials.
// submitted) must still hand refreshers the previously stored credentials,
// decrypted once, rather than an empty map that would look like "no
// credentials configured" and wrongly flip a resolver back to its fallback.
func TestUpsertIntegrationConfig_SettingsOnlyUpdate_RefresherSeesExistingCredentials(t *testing.T) {
	enc := testEncryptor(t)
	integrations := memory.NewIntegrationConfigStore()
	refresher := &fakeCredentialsRefresher{}
	server := grpchandler.NewConfigServer(grpchandler.ConfigServerParams{
		Integrations: integrations,
		Crypto:       enc,
		Refreshers:   []grpchandler.IntegrationCredentialsRefresher{refresher},
	})
	ctx := context.Background()

	if _, err := server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "jira",
		Credentials:     map[string]string{"api_token": "jira_live_token"},
		Settings:        map[string]string{"cloud_id": "cloud-123"},
	}); err != nil {
		t.Fatalf("initial UpsertIntegrationConfig: %v", err)
	}
	refresher.calls = nil

	if _, err := server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "jira",
		Settings:        map[string]string{"cloud_id": "cloud-456"},
	}); err != nil {
		t.Fatalf("settings-only UpsertIntegrationConfig: %v", err)
	}

	if len(refresher.calls) != 1 {
		t.Fatalf("refresher calls = %v, want exactly one call", refresher.calls)
	}
	call := refresher.calls[0]
	if call.credentials["api_token"] != "jira_live_token" {
		t.Errorf("settings-only update passed credentials = %v, want the previously stored api_token preserved", call.credentials)
	}
	if call.settings["cloud_id"] != "cloud-456" {
		t.Errorf("settings-only update passed settings = %v, want the new cloud_id", call.settings)
	}
}

// TestUpsertIntegrationConfig_SettingsOnlyUpdate_ExistingCredentialsUndecryptable_Rejected
// covers the "encrypt/decrypt fails in any way" requirement for the one
// decrypt UpsertIntegrationConfig still performs after startup: existing
// credentials that can no longer be decrypted (e.g. ENCRYPTION_KEY rotated
// since they were written) must fail the request outright — before
// anything is written or any refresher is consulted — rather than silently
// treating the integration as unconfigured.
func TestUpsertIntegrationConfig_SettingsOnlyUpdate_ExistingCredentialsUndecryptable_Rejected(t *testing.T) {
	writeKeyEnc := testEncryptor(t)
	readKeyEnc := testEncryptor(t) // different key: decryption will fail
	integrations := memory.NewIntegrationConfigStore()

	raw, err := json.Marshal(map[string]string{"api_key": "pd_live_key"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sealed, err := writeKeyEnc.Encrypt(raw)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := integrations.Upsert(context.Background(), &store.IntegrationConfig{
		IntegrationType:      "pagerduty",
		EncryptedCredentials: sealed,
	}); err != nil {
		t.Fatalf("seed Upsert: %v", err)
	}

	refresher := &fakeCredentialsRefresher{}
	server := grpchandler.NewConfigServer(grpchandler.ConfigServerParams{
		Integrations: integrations,
		Crypto:       readKeyEnc,
		Refreshers:   []grpchandler.IntegrationCredentialsRefresher{refresher},
	})

	_, err = server.UpsertIntegrationConfig(context.Background(), &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "pagerduty",
		Settings:        map[string]string{"default_escalation_policy": "P999"},
	})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Fatalf("UpsertIntegrationConfig err = %v, want FailedPrecondition when existing credentials can't be decrypted", err)
	}
	if len(refresher.calls) != 0 {
		t.Error("a refresher must not be consulted when existing credentials can't be decrypted")
	}
}

func TestUpsertIntegrationConfig_NoRefreshersConfigured_DoesNotPanic(t *testing.T) {
	// ConfigServerParams.Refreshers is nil here, matching every other
	// newTestConfigServer-based test in this file — Upsert must tolerate
	// that rather than nil-panicking when it ranges over the list.
	ts := newTestConfigServer(testEncryptor(t))
	if _, err := ts.server.UpsertIntegrationConfig(context.Background(), &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "pagerduty",
		Credentials:     map[string]string{"api_key": "secret"},
	}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}
}

func TestUpsertIntegrationConfig_NoEncryptorConfigured(t *testing.T) {
	ts := newTestConfigServer(nil) // no ENCRYPTION_KEY
	_, err := ts.server.UpsertIntegrationConfig(context.Background(), &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "pagerduty",
		Credentials:     map[string]string{"api_key": "secret"},
	})
	if grpcCode(err) != codes.FailedPrecondition {
		t.Errorf("want FailedPrecondition, got %v", grpcCode(err))
	}
}

func TestUpsertIntegrationConfig_SettingsOnlyWithoutEncryptorIsAllowed(t *testing.T) {
	ts := newTestConfigServer(nil) // no ENCRYPTION_KEY
	_, err := ts.server.UpsertIntegrationConfig(context.Background(), &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "datadog",
		Settings:        map[string]string{"base_url": "https://app.datadoghq.com"},
	})
	if err != nil {
		t.Fatalf("UpsertIntegrationConfig without credentials should succeed with no encryptor: %v", err)
	}
}

func TestUpsertIntegrationConfig_OmittingCredentialsPreservesExisting(t *testing.T) {
	enc := testEncryptor(t)
	ts := newTestConfigServer(enc)
	ctx := context.Background()

	if _, err := ts.server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "github",
		Credentials:     map[string]string{"token": "ghp_original"},
	}); err != nil {
		t.Fatalf("first UpsertIntegrationConfig: %v", err)
	}

	// Second call updates only settings, omitting credentials entirely.
	resp, err := ts.server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "github",
		Settings:        map[string]string{"default_repo": "acme/infra"},
	})
	if err != nil {
		t.Fatalf("second UpsertIntegrationConfig: %v", err)
	}
	if !resp.GetCredentialsConfigured() {
		t.Error("credentials should still be configured after a settings-only update")
	}

	stored, _ := ts.integrations.Get(ctx, "github")
	creds, err := grpchandler.DecryptIntegrationCredentials(enc, stored)
	if err != nil {
		t.Fatalf("DecryptIntegrationCredentials: %v", err)
	}
	if creds["token"] != "ghp_original" {
		t.Errorf("original credentials should survive a settings-only update, got %q", creds["token"])
	}
}

func TestUpsertIntegrationConfig_OmittingSettingsPreservesExisting(t *testing.T) {
	enc := testEncryptor(t)
	ts := newTestConfigServer(enc)
	ctx := context.Background()

	if _, err := ts.server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "pagerduty",
		Settings:        map[string]string{"default_escalation_policy": "P123"},
	}); err != nil {
		t.Fatalf("first UpsertIntegrationConfig: %v", err)
	}

	// Second call rotates the credential only, omitting settings entirely.
	resp, err := ts.server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "pagerduty",
		Credentials:     map[string]string{"api_key": "new_key"},
	})
	if err != nil {
		t.Fatalf("second UpsertIntegrationConfig: %v", err)
	}
	if resp.GetSettings()["default_escalation_policy"] != "P123" {
		t.Errorf("settings should survive a credentials-only update, got %v", resp.GetSettings())
	}
}

func TestGetIntegrationConfig_NotFound(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.GetIntegrationConfig(context.Background(), &pb.GetIntegrationConfigRequest{IntegrationType: "slack"})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

// newTestConfigServerWithSlackbotEmail is like newTestConfigServer but also
// sets SlackbotServiceEmail, for GetSlackBotCredential's caller-identity gate.
func newTestConfigServerWithSlackbotEmail(enc grpchandler.Encryptor, slackbotEmail string) *testConfigServer {
	services := memory.NewServiceStore()
	users := memory.NewUserStore()
	oncall := memory.NewOnCallStore()
	integrations := memory.NewIntegrationConfigStore()
	retention := memory.NewRetentionConfigStore()
	aiPlugins := memory.NewAIPluginStore()
	return &testConfigServer{
		server: grpchandler.NewConfigServer(grpchandler.ConfigServerParams{
			Services: services, Users: users, OnCall: oncall, Integrations: integrations,
			Retention: retention, AIPlugins: aiPlugins, Crypto: enc,
			SlackbotServiceEmail: slackbotEmail,
		}),
		services:     services,
		users:        users,
		oncall:       oncall,
		integrations: integrations,
		retention:    retention,
		aiPlugins:    aiPlugins,
	}
}

func TestGetSlackBotCredential_ReturnsDecryptedPairForServiceAccount(t *testing.T) {
	enc := testEncryptor(t)
	ts := newTestConfigServerWithSlackbotEmail(enc, "slackbot@example.com")
	ctx := context.Background()
	if _, err := ts.server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "slack",
		Credentials:     map[string]string{"bot_token": "xoxb-1", "app_token": "xapp-1"},
	}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}

	callerCtx := auth.WithUser(ctx, &auth.UserContext{UserID: "slackbot-user", Email: "slackbot@example.com", OrgRole: store.OrgRoleAdmin})
	resp, err := ts.server.GetSlackBotCredential(callerCtx, &pb.GetSlackBotCredentialRequest{})
	if err != nil {
		t.Fatalf("GetSlackBotCredential: %v", err)
	}
	if resp.GetBotToken() != "xoxb-1" || resp.GetAppToken() != "xapp-1" {
		t.Errorf("got (%q, %q), want (xoxb-1, xapp-1)", resp.GetBotToken(), resp.GetAppToken())
	}
}

func TestGetSlackBotCredential_NotConfigured_ReturnsEmptyNotError(t *testing.T) {
	ts := newTestConfigServerWithSlackbotEmail(nil, "slackbot@example.com")
	callerCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "slackbot-user", Email: "slackbot@example.com", OrgRole: store.OrgRoleAdmin})
	resp, err := ts.server.GetSlackBotCredential(callerCtx, &pb.GetSlackBotCredentialRequest{})
	if err != nil {
		t.Fatalf("GetSlackBotCredential: %v", err)
	}
	if resp.GetBotToken() != "" || resp.GetAppToken() != "" {
		t.Errorf("got (%q, %q), want empty strings when slack isn't configured", resp.GetBotToken(), resp.GetAppToken())
	}
}

func TestGetSlackBotCredential_WrongCallerEmail_PermissionDenied(t *testing.T) {
	enc := testEncryptor(t)
	ts := newTestConfigServerWithSlackbotEmail(enc, "slackbot@example.com")
	ctx := context.Background()
	if _, err := ts.server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{
		IntegrationType: "slack",
		Credentials:     map[string]string{"bot_token": "xoxb-1"},
	}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}

	adminCtx := auth.WithUser(ctx, &auth.UserContext{UserID: "user-admin", Email: "admin@example.com", OrgRole: store.OrgRoleAdmin})
	_, err := ts.server.GetSlackBotCredential(adminCtx, &pb.GetSlackBotCredentialRequest{})
	if grpcCode(err) != codes.PermissionDenied {
		t.Errorf("an unrelated Admin caller: want PermissionDenied, got %v", grpcCode(err))
	}
}

func TestGetSlackBotCredential_NoServiceEmailConfigured_RejectsEveryCaller(t *testing.T) {
	enc := testEncryptor(t)
	ts := newTestConfigServerWithSlackbotEmail(enc, "")
	adminCtx := auth.WithUser(context.Background(), &auth.UserContext{UserID: "user-admin", Email: "admin@example.com", OrgRole: store.OrgRoleAdmin})
	_, err := ts.server.GetSlackBotCredential(adminCtx, &pb.GetSlackBotCredentialRequest{})
	if grpcCode(err) != codes.PermissionDenied {
		t.Errorf("no SlackbotServiceEmail configured: want PermissionDenied, got %v", grpcCode(err))
	}
}

func TestListIntegrationConfigs(t *testing.T) {
	enc := testEncryptor(t)
	ts := newTestConfigServer(enc)
	ctx := context.Background()
	if _, err := ts.server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{IntegrationType: "slack", Credentials: map[string]string{"bot_token": "xoxb-1"}}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}
	if _, err := ts.server.UpsertIntegrationConfig(ctx, &pb.UpsertIntegrationConfigRequest{IntegrationType: "github", Settings: map[string]string{"default_repo": "acme/infra"}}); err != nil {
		t.Fatalf("UpsertIntegrationConfig: %v", err)
	}

	resp, err := ts.server.ListIntegrationConfigs(ctx, &pb.ListIntegrationConfigsRequest{})
	if err != nil {
		t.Fatalf("ListIntegrationConfigs: %v", err)
	}
	if len(resp.GetConfigs()) != 2 {
		t.Fatalf("want 2 configs, got %d", len(resp.GetConfigs()))
	}
	for _, cfg := range resp.GetConfigs() {
		if cfg.GetIntegrationType() == "slack" && !cfg.GetCredentialsConfigured() {
			t.Error("slack should report credentials_configured=true")
		}
		if cfg.GetIntegrationType() == "github" && cfg.GetCredentialsConfigured() {
			t.Error("github should report credentials_configured=false (settings-only)")
		}
	}
}

// ── Data retention ────────────────────────────────────────────────────────────

func TestGetRetentionConfig_DefaultsToRetainForever(t *testing.T) {
	ts := newTestConfigServer(nil)
	resp, err := ts.server.GetRetentionConfig(context.Background(), &pb.GetRetentionConfigRequest{SeverityLevel: 1})
	if err != nil {
		t.Fatalf("GetRetentionConfig: %v", err)
	}
	if resp.GetRetentionDays() != 0 || resp.GetHardDelete() {
		t.Errorf("default retention = %+v, want retain-forever", resp)
	}
}

func TestGetRetentionConfig_InvalidSeverityLevel(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.GetRetentionConfig(context.Background(), &pb.GetRetentionConfigRequest{SeverityLevel: 9})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestUpdateRetentionConfig_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()

	resp, err := ts.server.UpdateRetentionConfig(ctx, &pb.UpdateRetentionConfigRequest{
		SeverityLevel: 4,
		RetentionDays: 180,
		HardDelete:    false,
	})
	if err != nil {
		t.Fatalf("UpdateRetentionConfig: %v", err)
	}
	if resp.GetRetentionDays() != 180 {
		t.Errorf("retention_days = %d, want 180", resp.GetRetentionDays())
	}
	if resp.GetUpdatedAt() == nil || !resp.GetUpdatedAt().AsTime().After(time.Now().Add(-time.Minute)) {
		t.Errorf("updated_at should be set to (approximately) now, got %v", resp.GetUpdatedAt())
	}

	got, err := ts.server.GetRetentionConfig(ctx, &pb.GetRetentionConfigRequest{SeverityLevel: 4})
	if err != nil {
		t.Fatalf("GetRetentionConfig: %v", err)
	}
	if got.GetRetentionDays() != 180 {
		t.Errorf("persisted retention_days = %d, want 180", got.GetRetentionDays())
	}
}

func TestUpdateRetentionConfig_NegativeDaysRejected(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpdateRetentionConfig(context.Background(), &pb.UpdateRetentionConfigRequest{
		SeverityLevel: 1,
		RetentionDays: -1,
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestListRetentionConfig(t *testing.T) {
	ts := newTestConfigServer(nil)
	resp, err := ts.server.ListRetentionConfig(context.Background(), &pb.ListRetentionConfigRequest{})
	if err != nil {
		t.Fatalf("ListRetentionConfig: %v", err)
	}
	if len(resp.GetConfigs()) != 4 {
		t.Errorf("want 4 severity levels, got %d", len(resp.GetConfigs()))
	}
}
