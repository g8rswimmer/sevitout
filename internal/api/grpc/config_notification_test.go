package grpc_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

// newTestConfigServerWithNotifier is newTestConfigServer, but with a real
// *grpchandler.Notifier wired in (backed by tn's own stores/fakes) — needed
// for TestNotificationConfig, which no-ops without one. tn lets a test seed
// Slack/email integration config and inspect the fake senders directly, the
// same way notify_test.go's Notifier tests do.
func newTestConfigServerWithNotifier(t *testing.T) (*testConfigServer, *testNotifier) {
	t.Helper()
	tn := newTestNotifier(t)
	ts := &testConfigServer{
		server: grpchandler.NewConfigServer(grpchandler.ConfigServerParams{
			NotificationConfigs: tn.configs,
			Integrations:        tn.integrations,
			Crypto:              tn.enc,
			Notifier:            tn.notifier,
		}),
		notificationConfigs: tn.configs,
		integrations:        tn.integrations,
	}
	return ts, tn
}

func TestCreateNotificationConfig_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()

	resp, err := ts.server.CreateNotificationConfig(ctx, &pb.CreateNotificationConfigRequest{
		Role: "incident-commander", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if err != nil {
		t.Fatalf("CreateNotificationConfig: %v", err)
	}
	if resp.GetId() == 0 {
		t.Error("want a nonzero id on create")
	}
	if resp.GetChannelTarget() != "#incidents" {
		t.Errorf("ChannelTarget = %q, want %q", resp.GetChannelTarget(), "#incidents")
	}
	if resp.GetMaxSeverityLevel() != 0 {
		t.Errorf("MaxSeverityLevel = %d, want 0 (unset)", resp.GetMaxSeverityLevel())
	}
}

func TestCreateNotificationConfig_TimestampsAreSet(t *testing.T) {
	// Regression: an earlier version of this handler left CreatedAt/UpdatedAt
	// zero-valued on the Go struct before calling Create, which the
	// in-memory store (unlike postgres's NOW()-driven SQL) persists as-is —
	// every rule rendered "0001-01-01T00:00:00Z" until this was fixed.
	ts := newTestConfigServer(nil)
	resp, err := ts.server.CreateNotificationConfig(context.Background(), &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if err != nil {
		t.Fatalf("CreateNotificationConfig: %v", err)
	}
	if resp.GetCreatedAt().AsTime().IsZero() {
		t.Error("CreatedAt should not be the zero time")
	}
	if resp.GetUpdatedAt().AsTime().IsZero() {
		t.Error("UpdatedAt should not be the zero time")
	}
}

func TestCreateNotificationConfig_MultipleEvents(t *testing.T) {
	ts := newTestConfigServer(nil)
	resp, err := ts.server.CreateNotificationConfig(context.Background(), &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.sla_at_risk", "sev.sla_breached"}, ChannelType: "slack", ChannelTarget: "#sla-alerts",
	})
	if err != nil {
		t.Fatalf("CreateNotificationConfig: %v", err)
	}
	if got := resp.GetEvents(); len(got) != 2 || got[0] != "sev.sla_at_risk" || got[1] != "sev.sla_breached" {
		t.Errorf("Events = %v, want [sev.sla_at_risk sev.sla_breached]", got)
	}
}

func TestCreateNotificationConfig_WithMaxSeverityLevel(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()

	resp, err := ts.server.CreateNotificationConfig(ctx, &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "email", ChannelTarget: "management@example.com", MaxSeverityLevel: 2,
	})
	if err != nil {
		t.Fatalf("CreateNotificationConfig: %v", err)
	}
	if resp.GetMaxSeverityLevel() != 2 {
		t.Errorf("MaxSeverityLevel = %d, want 2", resp.GetMaxSeverityLevel())
	}
}

func TestCreateNotificationConfig_UnknownRole(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.CreateNotificationConfig(context.Background(), &pb.CreateNotificationConfigRequest{
		Role: "manager", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for unknown role, got %v", grpcCode(err))
	}
}

func TestCreateNotificationConfig_NoEvents(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.CreateNotificationConfig(context.Background(), &pb.CreateNotificationConfigRequest{
		Role: "admin", ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for an empty events list, got %v", grpcCode(err))
	}
}

func TestCreateNotificationConfig_UnknownEvent(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.CreateNotificationConfig(context.Background(), &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.deleted"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for unknown event, got %v", grpcCode(err))
	}
}

func TestCreateNotificationConfig_DuplicateEvent(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.CreateNotificationConfig(context.Background(), &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created", "sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for a duplicate event in the list, got %v", grpcCode(err))
	}
}

func TestCreateNotificationConfig_UnknownChannelType(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.CreateNotificationConfig(context.Background(), &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "sms", ChannelTarget: "555-1234",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for unknown channel_type, got %v", grpcCode(err))
	}
}

func TestCreateNotificationConfig_EmptyChannelTargetRejected(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.CreateNotificationConfig(context.Background(), &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "   ",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for blank channel_target, got %v", grpcCode(err))
	}
}

func TestCreateNotificationConfig_InvalidMaxSeverityLevel(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.CreateNotificationConfig(context.Background(), &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents", MaxSeverityLevel: 9,
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for out-of-range max_severity_level, got %v", grpcCode(err))
	}
}

func TestUpdateNotificationConfig_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	created, err := ts.server.CreateNotificationConfig(ctx, &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if err != nil {
		t.Fatalf("CreateNotificationConfig: %v", err)
	}

	updated, err := ts.server.UpdateNotificationConfig(ctx, &pb.UpdateNotificationConfigRequest{
		Id: created.GetId(), Role: "admin", Events: []string{"sev.created", "sev.updated"}, ChannelType: "slack", ChannelTarget: "#incidents-v2",
	})
	if err != nil {
		t.Fatalf("UpdateNotificationConfig: %v", err)
	}
	if updated.GetId() != created.GetId() {
		t.Errorf("Id = %d, want %d (unchanged)", updated.GetId(), created.GetId())
	}
	if updated.GetChannelTarget() != "#incidents-v2" {
		t.Errorf("ChannelTarget = %q, want %q", updated.GetChannelTarget(), "#incidents-v2")
	}
	if len(updated.GetEvents()) != 2 {
		t.Errorf("Events = %v, want 2 events", updated.GetEvents())
	}
}

func TestUpdateNotificationConfig_NotFound(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpdateNotificationConfig(context.Background(), &pb.UpdateNotificationConfigRequest{
		Id: 999999, Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestUpdateNotificationConfig_MissingID(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpdateNotificationConfig(context.Background(), &pb.UpdateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for a missing id, got %v", grpcCode(err))
	}
}

func TestDeleteNotificationConfig_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	created, err := ts.server.CreateNotificationConfig(ctx, &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if err != nil {
		t.Fatalf("CreateNotificationConfig: %v", err)
	}

	if _, err := ts.server.DeleteNotificationConfig(ctx, &pb.DeleteNotificationConfigRequest{
		Id: created.GetId(),
	}); err != nil {
		t.Fatalf("DeleteNotificationConfig: %v", err)
	}

	resp, err := ts.server.ListNotificationConfigs(ctx, nil)
	if err != nil {
		t.Fatalf("ListNotificationConfigs: %v", err)
	}
	if len(resp.GetConfigs()) != 0 {
		t.Errorf("want no configs after delete, got %d", len(resp.GetConfigs()))
	}
}

func TestDeleteNotificationConfig_NotFound(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.DeleteNotificationConfig(context.Background(), &pb.DeleteNotificationConfigRequest{
		Id: 999999,
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestListNotificationConfigs_ReturnsAllRules(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	if _, err := ts.server.CreateNotificationConfig(ctx, &pb.CreateNotificationConfigRequest{
		Role: "incident-commander", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	}); err != nil {
		t.Fatalf("CreateNotificationConfig: %v", err)
	}
	if _, err := ts.server.CreateNotificationConfig(ctx, &pb.CreateNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "email", ChannelTarget: "mgmt@example.com", MaxSeverityLevel: 2,
	}); err != nil {
		t.Fatalf("CreateNotificationConfig: %v", err)
	}

	resp, err := ts.server.ListNotificationConfigs(ctx, nil)
	if err != nil {
		t.Fatalf("ListNotificationConfigs: %v", err)
	}
	if len(resp.GetConfigs()) != 2 {
		t.Fatalf("want 2 configs, got %d", len(resp.GetConfigs()))
	}
}

func TestTestNotificationConfig_NoNotifierWired_Unavailable(t *testing.T) {
	// newTestConfigServer(nil) doesn't wire a Notifier — matches how every
	// other test in this file constructs a ConfigServer, and confirms the
	// handler fails clearly rather than silently reporting 0 results.
	ts := newTestConfigServer(nil)
	_, err := ts.server.TestNotificationConfig(context.Background(), &pb.TestNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.Unavailable {
		t.Errorf("want Unavailable with no Notifier wired, got %v", grpcCode(err))
	}
}

func TestTestNotificationConfig_OneResultPerEvent(t *testing.T) {
	ts, tn := newTestConfigServerWithNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")

	resp, err := ts.server.TestNotificationConfig(context.Background(), &pb.TestNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.sla_at_risk", "sev.sla_breached"}, ChannelType: "slack", ChannelTarget: "#sla-alerts",
	})
	if err != nil {
		t.Fatalf("TestNotificationConfig: %v", err)
	}
	results := resp.GetResults()
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for i, want := range []string{"sev.sla_at_risk", "sev.sla_breached"} {
		if results[i].GetEvent() != want {
			t.Errorf("results[%d].Event = %q, want %q", i, results[i].GetEvent(), want)
		}
		if !results[i].GetSuccess() {
			t.Errorf("results[%d]: want success, got error %q", i, results[i].GetError())
		}
	}
	if tn.slack.calls != 2 {
		t.Errorf("want 2 slack deliveries, got %d", tn.slack.calls)
	}
}

func TestTestNotificationConfig_UnconfiguredIntegration_ReportsError(t *testing.T) {
	ts, _ := newTestConfigServerWithNotifier(t)
	// No "slack" integration seeded.
	resp, err := ts.server.TestNotificationConfig(context.Background(), &pb.TestNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if err != nil {
		t.Fatalf("TestNotificationConfig: %v", err)
	}
	results := resp.GetResults()
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].GetSuccess() {
		t.Error("want success=false when the slack integration isn't configured")
	}
	if results[0].GetError() == "" {
		t.Error("want a non-empty error message explaining the failure")
	}
}

func TestTestNotificationConfig_WorksForAnUnsavedDraft(t *testing.T) {
	// No rule is ever Create'd here — Test must work purely off the request
	// body, matching a not-yet-saved Add-rule form.
	ts, tn := newTestConfigServerWithNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")

	resp, err := ts.server.TestNotificationConfig(context.Background(), &pb.TestNotificationConfigRequest{
		Role: "incident-commander", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if err != nil {
		t.Fatalf("TestNotificationConfig: %v", err)
	}
	if len(resp.GetResults()) != 1 || !resp.GetResults()[0].GetSuccess() {
		t.Fatalf("want 1 successful result for a never-saved draft rule, got %+v", resp.GetResults())
	}
	configs, _ := tn.configs.List(context.Background())
	if len(configs) != 0 {
		t.Errorf("TestNotificationConfig must not persist anything, found %d saved rules", len(configs))
	}
}

func TestTestNotificationConfig_UnknownRole(t *testing.T) {
	ts, _ := newTestConfigServerWithNotifier(t)
	_, err := ts.server.TestNotificationConfig(context.Background(), &pb.TestNotificationConfigRequest{
		Role: "manager", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for unknown role, got %v", grpcCode(err))
	}
}

func TestTestNotificationConfig_NoEvents(t *testing.T) {
	ts, _ := newTestConfigServerWithNotifier(t)
	_, err := ts.server.TestNotificationConfig(context.Background(), &pb.TestNotificationConfigRequest{
		Role: "admin", ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for an empty events list, got %v", grpcCode(err))
	}
}

func TestTestNotificationConfig_EmptyChannelTargetRejected(t *testing.T) {
	ts, _ := newTestConfigServerWithNotifier(t)
	_, err := ts.server.TestNotificationConfig(context.Background(), &pb.TestNotificationConfigRequest{
		Role: "admin", Events: []string{"sev.created"}, ChannelType: "slack", ChannelTarget: "   ",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for blank channel_target, got %v", grpcCode(err))
	}
}

func TestUpsertEscalationConfig_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()

	resp, err := ts.server.UpsertEscalationConfig(ctx, &pb.UpsertEscalationConfigRequest{
		SeverityLevel: 1, ThresholdMinutes: 30, Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertEscalationConfig: %v", err)
	}
	if resp.GetThresholdMinutes() != 30 || !resp.GetEnabled() {
		t.Errorf("got %+v, want threshold=30 enabled=true", resp)
	}
}

func TestUpsertEscalationConfig_InvalidSeverityLevel(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpsertEscalationConfig(context.Background(), &pb.UpsertEscalationConfigRequest{
		SeverityLevel: 9, ThresholdMinutes: 30, Enabled: true,
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument, got %v", grpcCode(err))
	}
}

func TestUpsertEscalationConfig_NegativeThreshold(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpsertEscalationConfig(context.Background(), &pb.UpsertEscalationConfigRequest{
		SeverityLevel: 1, ThresholdMinutes: -5, Enabled: true,
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for negative threshold, got %v", grpcCode(err))
	}
}

func TestListEscalationConfigs_ReturnsPreSeededRows(t *testing.T) {
	ts := newTestConfigServer(nil)
	resp, err := ts.server.ListEscalationConfigs(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListEscalationConfigs: %v", err)
	}
	// The in-memory store pre-seeds all four severity levels, disabled by
	// default, matching RetentionConfigStore's precedent.
	if len(resp.GetConfigs()) != 4 {
		t.Fatalf("want 4 pre-seeded rows, got %d", len(resp.GetConfigs()))
	}
	for _, cfg := range resp.GetConfigs() {
		if cfg.GetEnabled() {
			t.Errorf("severity %d: want disabled by default, got enabled", cfg.GetSeverityLevel())
		}
	}
}
