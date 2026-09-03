package grpc_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"

	"github.com/g8rswimmer/sevitout/internal/api/pb"
)

func TestUpsertNotificationConfig_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()

	resp, err := ts.server.UpsertNotificationConfig(ctx, &pb.UpsertNotificationConfigRequest{
		Role: "incident-commander", Event: "sev.created", ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if err != nil {
		t.Fatalf("UpsertNotificationConfig: %v", err)
	}
	if resp.GetChannelTarget() != "#incidents" {
		t.Errorf("ChannelTarget = %q, want %q", resp.GetChannelTarget(), "#incidents")
	}
	if resp.GetMaxSeverityLevel() != 0 {
		t.Errorf("MaxSeverityLevel = %d, want 0 (unset)", resp.GetMaxSeverityLevel())
	}
}

func TestUpsertNotificationConfig_WithMaxSeverityLevel(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()

	resp, err := ts.server.UpsertNotificationConfig(ctx, &pb.UpsertNotificationConfigRequest{
		Role: "admin", Event: "sev.created", ChannelType: "email", ChannelTarget: "management@example.com", MaxSeverityLevel: 2,
	})
	if err != nil {
		t.Fatalf("UpsertNotificationConfig: %v", err)
	}
	if resp.GetMaxSeverityLevel() != 2 {
		t.Errorf("MaxSeverityLevel = %d, want 2", resp.GetMaxSeverityLevel())
	}
}

func TestUpsertNotificationConfig_UnknownRole(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpsertNotificationConfig(context.Background(), &pb.UpsertNotificationConfigRequest{
		Role: "manager", Event: "sev.created", ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for unknown role, got %v", grpcCode(err))
	}
}

func TestUpsertNotificationConfig_UnknownEvent(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpsertNotificationConfig(context.Background(), &pb.UpsertNotificationConfigRequest{
		Role: "admin", Event: "sev.deleted", ChannelType: "slack", ChannelTarget: "#incidents",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for unknown event, got %v", grpcCode(err))
	}
}

func TestUpsertNotificationConfig_UnknownChannelType(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpsertNotificationConfig(context.Background(), &pb.UpsertNotificationConfigRequest{
		Role: "admin", Event: "sev.created", ChannelType: "sms", ChannelTarget: "555-1234",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for unknown channel_type, got %v", grpcCode(err))
	}
}

func TestUpsertNotificationConfig_EmptyChannelTargetRejected(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpsertNotificationConfig(context.Background(), &pb.UpsertNotificationConfigRequest{
		Role: "admin", Event: "sev.created", ChannelType: "slack", ChannelTarget: "   ",
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for blank channel_target, got %v", grpcCode(err))
	}
}

func TestUpsertNotificationConfig_InvalidMaxSeverityLevel(t *testing.T) {
	ts := newTestConfigServer(nil)
	_, err := ts.server.UpsertNotificationConfig(context.Background(), &pb.UpsertNotificationConfigRequest{
		Role: "admin", Event: "sev.created", ChannelType: "slack", ChannelTarget: "#incidents", MaxSeverityLevel: 9,
	})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("want InvalidArgument for out-of-range max_severity_level, got %v", grpcCode(err))
	}
}

func TestDeleteNotificationConfig_Valid(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	if _, err := ts.server.UpsertNotificationConfig(ctx, &pb.UpsertNotificationConfigRequest{
		Role: "admin", Event: "sev.created", ChannelType: "slack", ChannelTarget: "#incidents",
	}); err != nil {
		t.Fatalf("UpsertNotificationConfig: %v", err)
	}

	if _, err := ts.server.DeleteNotificationConfig(ctx, &pb.DeleteNotificationConfigRequest{
		Role: "admin", Event: "sev.created", ChannelType: "slack",
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
		Role: "admin", Event: "sev.created", ChannelType: "slack",
	})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("want NotFound, got %v", grpcCode(err))
	}
}

func TestListNotificationConfigs_ReturnsAllRules(t *testing.T) {
	ts := newTestConfigServer(nil)
	ctx := context.Background()
	if _, err := ts.server.UpsertNotificationConfig(ctx, &pb.UpsertNotificationConfigRequest{
		Role: "incident-commander", Event: "sev.created", ChannelType: "slack", ChannelTarget: "#incidents",
	}); err != nil {
		t.Fatalf("UpsertNotificationConfig: %v", err)
	}
	if _, err := ts.server.UpsertNotificationConfig(ctx, &pb.UpsertNotificationConfigRequest{
		Role: "admin", Event: "sev.created", ChannelType: "email", ChannelTarget: "mgmt@example.com", MaxSeverityLevel: 2,
	}); err != nil {
		t.Fatalf("UpsertNotificationConfig: %v", err)
	}

	resp, err := ts.server.ListNotificationConfigs(ctx, nil)
	if err != nil {
		t.Fatalf("ListNotificationConfigs: %v", err)
	}
	if len(resp.GetConfigs()) != 2 {
		t.Fatalf("want 2 configs, got %d", len(resp.GetConfigs()))
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
