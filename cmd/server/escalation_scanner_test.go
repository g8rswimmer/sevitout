package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/crypto"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// recordingSlackSender is a grpchandler.SlackSender that records every call,
// for asserting scanEscalations actually notifies (rather than just not
// panicking).
type recordingSlackSender struct {
	channel string
	calls   int
}

func (r *recordingSlackSender) PostMessage(_ context.Context, channelID, _ string) error {
	r.channel = channelID
	r.calls++
	return nil
}

// newTestNotifier builds a real *grpchandler.Notifier backed by in-memory
// stores, with sender recorded via slack. Seeds a "slack" integration config
// and one NotificationConfig routing rule for "sev.escalation_no_ic".
func newTestEscalationNotifier(t *testing.T, slack *recordingSlackSender) *grpchandler.Notifier {
	t.Helper()
	key := make([]byte, crypto.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	enc := crypto.NewKeyEncryptor(key)

	integrations := memory.NewIntegrationConfigStore()
	raw, err := json.Marshal(map[string]string{"bot_token": "xoxb-fake"})
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	encrypted, err := enc.Encrypt(raw)
	if err != nil {
		t.Fatalf("encrypt credentials: %v", err)
	}
	now := time.Now()
	if err := integrations.Upsert(context.Background(), &store.IntegrationConfig{
		IntegrationType: "slack", EncryptedCredentials: encrypted, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed slack config: %v", err)
	}

	configs := memory.NewNotificationConfigStore()
	if err := configs.Upsert(context.Background(), &store.NotificationConfig{
		Role: store.OrgRoleAdmin, Event: "sev.escalation_no_ic", ChannelType: store.NotificationChannelSlack, ChannelTarget: "#alerts",
	}); err != nil {
		t.Fatalf("seed notification config: %v", err)
	}

	return grpchandler.NewNotifier(grpchandler.NotifierParams{
		Configs:      configs,
		Integrations: integrations,
		Crypto:       enc,
		SlackFactory: func(string) grpchandler.SlackSender { return slack },
	})
}

func TestScanEscalations_FiresAndMarksEscalated(t *testing.T) {
	sevs := memory.NewSEVStore()
	roles := memory.NewRoleStore()
	escalations := memory.NewEscalationConfigStore()
	slack := &recordingSlackSender{}
	notifier := newTestEscalationNotifier(t, slack)

	started := time.Now().Add(-45 * time.Minute)
	sv := &store.SEV{
		Title: "checkout down", Status: store.SEVStatusOpen, SeverityLevel: 1, StartedAt: &started,
		CreatedBy: "user-1", CreatedAt: started, UpdatedAt: started,
	}
	if err := sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("create SEV: %v", err)
	}
	if err := escalations.Upsert(context.Background(), &store.EscalationConfig{SeverityLevel: 1, ThresholdMinutes: 30, Enabled: true}); err != nil {
		t.Fatalf("upsert escalation config: %v", err)
	}

	scanEscalations(context.Background(), discardLogger(), sevs, roles, escalations, notifier)

	if slack.calls != 1 {
		t.Fatalf("want 1 escalation notification, got %d", slack.calls)
	}
	if slack.channel != "#alerts" {
		t.Errorf("channel = %q, want %q", slack.channel, "#alerts")
	}

	got, err := sevs.Get(context.Background(), sv.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EscalatedAt == nil {
		t.Fatal("want EscalatedAt set after escalation fires")
	}

	// A second scan must not re-fire for the same incident.
	scanEscalations(context.Background(), discardLogger(), sevs, roles, escalations, notifier)
	if slack.calls != 1 {
		t.Fatalf("want no re-fire on a second scan, got %d total calls", slack.calls)
	}
}

func TestScanEscalations_SkipsWhenICAssigned(t *testing.T) {
	sevs := memory.NewSEVStore()
	roles := memory.NewRoleStore()
	escalations := memory.NewEscalationConfigStore()
	slack := &recordingSlackSender{}
	notifier := newTestEscalationNotifier(t, slack)

	started := time.Now().Add(-45 * time.Minute)
	sv := &store.SEV{
		Title: "checkout down", Status: store.SEVStatusOpen, SeverityLevel: 1, StartedAt: &started,
		CreatedBy: "user-1", CreatedAt: started, UpdatedAt: started,
	}
	if err := sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("create SEV: %v", err)
	}
	if err := roles.Assign(context.Background(), &store.SEVRole{
		SEVID: sv.ID, RoleType: store.SEVRoleIncidentCommander, DisplayName: "Alice", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("assign IC role: %v", err)
	}
	if err := escalations.Upsert(context.Background(), &store.EscalationConfig{SeverityLevel: 1, ThresholdMinutes: 30, Enabled: true}); err != nil {
		t.Fatalf("upsert escalation config: %v", err)
	}

	scanEscalations(context.Background(), discardLogger(), sevs, roles, escalations, notifier)

	if slack.calls != 0 {
		t.Fatalf("want no escalation when an IC is already assigned, got %d", slack.calls)
	}
}

func TestScanEscalations_NoOpenSEVs_NoPanic(t *testing.T) {
	sevs := memory.NewSEVStore()
	roles := memory.NewRoleStore()
	escalations := memory.NewEscalationConfigStore()
	slack := &recordingSlackSender{}
	notifier := newTestEscalationNotifier(t, slack)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("scanEscalations panicked with no open SEVs: %v", r)
		}
	}()
	scanEscalations(context.Background(), discardLogger(), sevs, roles, escalations, notifier)
	if slack.calls != 0 {
		t.Errorf("want no deliveries with no open SEVs, got %d", slack.calls)
	}
}

func TestStartEscalationScanner_StopsWhenContextCanceled(t *testing.T) {
	sevs := memory.NewSEVStore()
	roles := memory.NewRoleStore()
	escalations := memory.NewEscalationConfigStore()
	notifier := newTestEscalationNotifier(t, &recordingSlackSender{})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		startEscalationScanner(ctx, discardLogger(), sevs, roles, escalations, notifier)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startEscalationScanner did not return after its context was canceled")
	}
}
