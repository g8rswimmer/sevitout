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

func int64p(v int64) *int64 { return &v }

// newTestSLANotifier is like newTestEscalationNotifier but seeds routing
// rules for "sev.sla_at_risk" and "sev.sla_breached" instead.
func newTestSLANotifier(t *testing.T, slack *recordingSlackSender) *grpchandler.Notifier {
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
	// One rule covering both events — exercises the multi-event rule shape
	// rather than one row per event.
	if err := configs.Create(context.Background(), &store.NotificationConfig{
		Role: store.OrgRoleAdmin, Events: []string{"sev.sla_at_risk", "sev.sla_breached"},
		ChannelType: store.NotificationChannelSlack, ChannelTarget: "#alerts",
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

func TestScanSLARisk_FiresAtRiskAndMarksNotified(t *testing.T) {
	sevs := memory.NewSEVStore()
	serviceSLAs := memory.NewServiceSLAStore()
	slack := &recordingSlackSender{}
	notifier := newTestSLANotifier(t, slack)

	started := time.Now().Add(-45 * time.Minute)
	sv := &store.SEV{
		Title: "checkout down", Status: store.SEVStatusOpen, SeverityLevel: 1,
		AffectedServices: []string{"checkout"}, StartedAt: &started,
		CreatedBy: "user-1", CreatedAt: started, UpdatedAt: started,
	}
	if err := sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("create SEV: %v", err)
	}
	// 5-minute MTTD target, but 45 minutes have already elapsed with no
	// DetectedAt recorded — Overall should read at_risk.
	if err := serviceSLAs.Upsert(context.Background(), &store.ServiceSLA{
		ServiceID: "checkout", SeverityLevel: 1, MTTDTargetSeconds: int64p(300),
	}); err != nil {
		t.Fatalf("upsert service SLA: %v", err)
	}

	scanSLARisk(context.Background(), discardLogger(), sevs, serviceSLAs, notifier)

	if slack.calls != 1 {
		t.Fatalf("want 1 sla_at_risk notification, got %d", slack.calls)
	}
	if slack.channel != "#alerts" {
		t.Errorf("channel = %q, want %q", slack.channel, "#alerts")
	}

	got, err := sevs.Get(context.Background(), sv.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SLANotifiedStatus == nil || *got.SLANotifiedStatus != "at_risk" {
		t.Fatalf("SLANotifiedStatus = %v, want at_risk", got.SLANotifiedStatus)
	}

	// A second scan must not re-fire for the same incident.
	scanSLARisk(context.Background(), discardLogger(), sevs, serviceSLAs, notifier)
	if slack.calls != 1 {
		t.Fatalf("want no re-fire on a second scan, got %d total calls", slack.calls)
	}
}

func TestScanSLARisk_EscalatesFromAtRiskToBreached(t *testing.T) {
	sevs := memory.NewSEVStore()
	serviceSLAs := memory.NewServiceSLAStore()
	slack := &recordingSlackSender{}
	notifier := newTestSLANotifier(t, slack)

	started := time.Now().Add(-45 * time.Minute)
	sv := &store.SEV{
		Title: "checkout down", Status: store.SEVStatusOpen, SeverityLevel: 1,
		AffectedServices: []string{"checkout"}, StartedAt: &started,
		CreatedBy: "user-1", CreatedAt: started, UpdatedAt: started,
	}
	if err := sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("create SEV: %v", err)
	}
	if err := serviceSLAs.Upsert(context.Background(), &store.ServiceSLA{
		ServiceID: "checkout", SeverityLevel: 1, MTTDTargetSeconds: int64p(300),
	}); err != nil {
		t.Fatalf("upsert service SLA: %v", err)
	}

	scanSLARisk(context.Background(), discardLogger(), sevs, serviceSLAs, notifier)
	if slack.calls != 1 {
		t.Fatalf("want 1 sla_at_risk notification, got %d", slack.calls)
	}

	// DetectedAt finally lands, well past target — the SEV's final MTTD is
	// now a confirmed breach.
	got, err := sevs.Get(context.Background(), sv.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got.MTTDSeconds = int64p(2700)
	if err := sevs.Update(context.Background(), got); err != nil {
		t.Fatalf("Update: %v", err)
	}

	scanSLARisk(context.Background(), discardLogger(), sevs, serviceSLAs, notifier)
	if slack.calls != 2 {
		t.Fatalf("want a second notification (sla_breached) once confirmed, got %d total calls", slack.calls)
	}

	final, err := sevs.Get(context.Background(), sv.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.SLANotifiedStatus == nil || *final.SLANotifiedStatus != "breached" {
		t.Fatalf("SLANotifiedStatus = %v, want breached", final.SLANotifiedStatus)
	}

	// A third scan must not re-fire again.
	scanSLARisk(context.Background(), discardLogger(), sevs, serviceSLAs, notifier)
	if slack.calls != 2 {
		t.Fatalf("want no re-fire once already breached, got %d total calls", slack.calls)
	}
}

func TestScanSLARisk_DirectBreachSkipsAtRiskNotification(t *testing.T) {
	sevs := memory.NewSEVStore()
	serviceSLAs := memory.NewServiceSLAStore()
	slack := &recordingSlackSender{}
	notifier := newTestSLANotifier(t, slack)

	started := time.Now().Add(-90 * time.Minute)
	sv := &store.SEV{
		Title: "checkout down", Status: store.SEVStatusResolved, SeverityLevel: 1,
		AffectedServices: []string{"checkout"}, StartedAt: &started,
		// Final MTTR already confirmed over target — never observed at_risk.
		MTTRSeconds: int64p(5400),
		CreatedBy:   "user-1", CreatedAt: started, UpdatedAt: started,
	}
	if err := sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("create SEV: %v", err)
	}
	if err := serviceSLAs.Upsert(context.Background(), &store.ServiceSLA{
		ServiceID: "checkout", SeverityLevel: 1, MTTRTargetSeconds: int64p(3600),
	}); err != nil {
		t.Fatalf("upsert service SLA: %v", err)
	}

	scanSLARisk(context.Background(), discardLogger(), sevs, serviceSLAs, notifier)

	if slack.calls != 1 {
		t.Fatalf("want exactly 1 notification (sla_breached, not sla_at_risk first), got %d", slack.calls)
	}
	got, err := sevs.Get(context.Background(), sv.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SLANotifiedStatus == nil || *got.SLANotifiedStatus != "breached" {
		t.Fatalf("SLANotifiedStatus = %v, want breached", got.SLANotifiedStatus)
	}
}

func TestScanSLARisk_NoServiceSLAConfigured_NoOp(t *testing.T) {
	sevs := memory.NewSEVStore()
	serviceSLAs := memory.NewServiceSLAStore()
	slack := &recordingSlackSender{}
	notifier := newTestSLANotifier(t, slack)

	started := time.Now().Add(-90 * time.Minute)
	sv := &store.SEV{
		Title: "checkout down", Status: store.SEVStatusOpen, SeverityLevel: 1,
		AffectedServices: []string{"checkout"}, StartedAt: &started,
		CreatedBy: "user-1", CreatedAt: started, UpdatedAt: started,
	}
	if err := sevs.Create(context.Background(), sv); err != nil {
		t.Fatalf("create SEV: %v", err)
	}
	// No ServiceSLA row configured for "checkout" at severity 1 — every
	// metric reads not_applicable regardless of elapsed time.

	scanSLARisk(context.Background(), discardLogger(), sevs, serviceSLAs, notifier)

	if slack.calls != 0 {
		t.Fatalf("want no notification with no SLA configured, got %d", slack.calls)
	}
}

func TestScanSLARisk_NoCandidateSEVs_NoPanic(t *testing.T) {
	sevs := memory.NewSEVStore()
	serviceSLAs := memory.NewServiceSLAStore()
	slack := &recordingSlackSender{}
	notifier := newTestSLANotifier(t, slack)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("scanSLARisk panicked with no candidate SEVs: %v", r)
		}
	}()
	scanSLARisk(context.Background(), discardLogger(), sevs, serviceSLAs, notifier)
	if slack.calls != 0 {
		t.Errorf("want no deliveries with no SEVs, got %d", slack.calls)
	}
}

func TestStartSLARiskScanner_StopsWhenContextCanceled(t *testing.T) {
	sevs := memory.NewSEVStore()
	serviceSLAs := memory.NewServiceSLAStore()
	notifier := newTestSLANotifier(t, &recordingSlackSender{})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		startSLARiskScanner(ctx, discardLogger(), sevs, serviceSLAs, notifier)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startSLARiskScanner did not return after its context was canceled")
	}
}
