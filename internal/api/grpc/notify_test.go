package grpc_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	grpchandler "github.com/g8rswimmer/sevitout/internal/api/grpc"
	"github.com/g8rswimmer/sevitout/internal/store"
	"github.com/g8rswimmer/sevitout/internal/store/memory"
)

// fakeSlackSender is a grpchandler.SlackSender that records every call.
type fakeSlackSender struct {
	channel string
	text    string
	calls   int
}

func (f *fakeSlackSender) PostMessage(_ context.Context, channelID, text string) error {
	f.channel = channelID
	f.text = text
	f.calls++
	return nil
}

// fakeEmailSender is a grpchandler.EmailSender that records every call.
type fakeEmailSender struct {
	to, subject, body string
	calls             int
}

func (f *fakeEmailSender) Send(_ context.Context, to, subject, body string) error {
	f.to, f.subject, f.body = to, subject, body
	f.calls++
	return nil
}

// testNotifier bundles a Notifier with its backing stores and fake senders,
// for exercising Notify end-to-end.
type testNotifier struct {
	notifier     *grpchandler.Notifier
	configs      *memory.NotificationConfigStore
	integrations *memory.IntegrationConfigStore
	enc          grpchandler.Encryptor
	slack        *fakeSlackSender
	email        *fakeEmailSender
}

func newTestNotifier(t *testing.T) *testNotifier {
	t.Helper()
	configs := memory.NewNotificationConfigStore()
	integrations := memory.NewIntegrationConfigStore()
	enc := testEncryptor(t)
	slackFake := &fakeSlackSender{}
	emailFake := &fakeEmailSender{}
	notifier := grpchandler.NewNotifier(grpchandler.NotifierParams{
		Configs:      configs,
		Integrations: integrations,
		Crypto:       enc,
		SlackFactory: func(string) grpchandler.SlackSender { return slackFake },
		EmailFactory: func(map[string]string) grpchandler.EmailSender { return emailFake },
	})
	return &testNotifier{notifier: notifier, configs: configs, integrations: integrations, enc: enc, slack: slackFake, email: emailFake}
}

func (tn *testNotifier) seedSlackConfig(t *testing.T, botToken string) {
	t.Helper()
	tn.seedIntegration(t, "slack", map[string]string{"bot_token": botToken}, nil)
}

func (tn *testNotifier) seedEmailConfig(t *testing.T, username, password string) {
	t.Helper()
	tn.seedIntegration(t, "email", map[string]string{"smtp_username": username, "smtp_password": password},
		map[string]any{"smtp_host": "smtp.example.com", "smtp_port": "587", "from_address": "sevitout@example.com"})
}

func (tn *testNotifier) seedIntegration(t *testing.T, integrationType string, creds map[string]string, settings map[string]any) {
	t.Helper()
	raw, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	encrypted, err := tn.enc.Encrypt(raw)
	if err != nil {
		t.Fatalf("encrypt credentials: %v", err)
	}
	now := time.Now()
	if err := tn.integrations.Upsert(context.Background(), &store.IntegrationConfig{
		IntegrationType: integrationType, EncryptedCredentials: encrypted, Settings: settings, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed %s config: %v", integrationType, err)
	}
}

func (tn *testNotifier) addRule(t *testing.T, role store.OrgRole, event string, channelType store.NotificationChannelType, target string, maxSeverity *int16) {
	t.Helper()
	if err := tn.configs.Upsert(context.Background(), &store.NotificationConfig{
		Role: role, Event: event, ChannelType: channelType, ChannelTarget: target, MaxSeverityLevel: maxSeverity,
	}); err != nil {
		t.Fatalf("addRule: %v", err)
	}
}

func int16p(v int16) *int16 { return &v }

func TestNotifier_Notify_NilReceiver_NoOp(t *testing.T) {
	var n *grpchandler.Notifier
	// Must not panic — matches Publisher's nil-is-a-no-op convention.
	n.Notify(context.Background(), grpchandler.NotifyEvent{Type: "sev.created"})
}

func TestNotifier_Notify_NoMatchingRule_NoDelivery(t *testing.T) {
	tn := newTestNotifier(t)
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{Type: "sev.created"})
	if tn.slack.calls != 0 || tn.email.calls != 0 {
		t.Errorf("expected no delivery with no matching rule, got slack=%d email=%d", tn.slack.calls, tn.email.calls)
	}
}

func TestNotifier_Notify_SlackDelivery(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	tn.addRule(t, store.OrgRoleIncidentCommander, "sev.created", store.NotificationChannelSlack, "#incidents", nil)

	sevRecord := &store.SEV{ID: "sev-1", Title: "checkout down", SeverityLevel: 1, Status: store.SEVStatusOpen}
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{Type: "sev.created", SEV: sevRecord})

	if tn.slack.calls != 1 {
		t.Fatalf("want 1 slack delivery, got %d", tn.slack.calls)
	}
	if tn.slack.channel != "#incidents" {
		t.Errorf("channel = %q, want %q", tn.slack.channel, "#incidents")
	}
	if tn.email.calls != 0 {
		t.Errorf("want no email delivery for a slack-only rule, got %d", tn.email.calls)
	}
}

func TestNotifier_Notify_EmailDelivery(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedEmailConfig(t, "smtpuser", "smtppass")
	tn.addRule(t, store.OrgRoleAdmin, "sev.created", store.NotificationChannelEmail, "oncall@example.com", nil)

	sevRecord := &store.SEV{ID: "sev-1", Title: "checkout down", SeverityLevel: 1, Status: store.SEVStatusOpen}
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{Type: "sev.created", SEV: sevRecord})

	if tn.email.calls != 1 {
		t.Fatalf("want 1 email delivery, got %d", tn.email.calls)
	}
	if tn.email.to != "oncall@example.com" {
		t.Errorf("to = %q, want %q", tn.email.to, "oncall@example.com")
	}
	if tn.email.subject == "" || tn.email.body == "" {
		t.Errorf("expected non-empty subject/body, got subject=%q body=%q", tn.email.subject, tn.email.body)
	}
}

func TestNotifier_Notify_SeverityFilter(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	// max_severity_level=2: "SEV-1/SEV-2 opens only."
	tn.addRule(t, store.OrgRoleAdmin, "sev.created", store.NotificationChannelSlack, "#management", int16p(2))

	// Severity 3 (less critical than the max) should not match.
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.created", SEV: &store.SEV{ID: "sev-3", SeverityLevel: 3},
	})
	if tn.slack.calls != 0 {
		t.Fatalf("severity 3 should not match a max_severity_level=2 rule, got %d deliveries", tn.slack.calls)
	}

	// Severity 1 (more critical) should match.
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.created", SEV: &store.SEV{ID: "sev-1", SeverityLevel: 1},
	})
	if tn.slack.calls != 1 {
		t.Fatalf("severity 1 should match a max_severity_level=2 rule, got %d deliveries", tn.slack.calls)
	}
}

func TestNotifier_Notify_UnconfiguredIntegration_SkipsGracefully(t *testing.T) {
	tn := newTestNotifier(t)
	// A rule exists, but no "slack" integration config was ever seeded.
	tn.addRule(t, store.OrgRoleAdmin, "sev.created", store.NotificationChannelSlack, "#incidents", nil)

	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.created", SEV: &store.SEV{ID: "sev-1", SeverityLevel: 1},
	})
	if tn.slack.calls != 0 {
		t.Errorf("want no delivery attempt without a configured integration, got %d", tn.slack.calls)
	}
}

func TestNotifier_Notify_EventWithNoSEV_MatchesEveryRule(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	tn.addRule(t, store.OrgRoleAdmin, "sev.escalation_no_ic", store.NotificationChannelSlack, "#alerts", int16p(2))

	// SEV is set here (escalation events always carry one), but confirm a
	// nil-severity NotifyEvent (Message-only) still matches a rule with no
	// MaxSeverityLevel filter at all.
	tn.configs.Upsert(context.Background(), &store.NotificationConfig{
		Role: store.OrgRoleAdmin, Event: "custom.event", ChannelType: store.NotificationChannelSlack, ChannelTarget: "#alerts",
	})
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{Type: "custom.event", Message: "hello"})
	if tn.slack.calls != 1 {
		t.Fatalf("want 1 delivery for a nil-SEV event against an unfiltered rule, got %d", tn.slack.calls)
	}
	if tn.slack.text != "hello" {
		t.Errorf("text = %q, want the overriding Message %q", tn.slack.text, "hello")
	}
}
