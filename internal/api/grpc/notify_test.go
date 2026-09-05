package grpc_test

import (
	"context"
	"encoding/json"
	"strings"
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
	tn.addRuleForEvents(t, role, []string{event}, channelType, target, maxSeverity)
}

func (tn *testNotifier) addRuleForEvents(t *testing.T, role store.OrgRole, events []string, channelType store.NotificationChannelType, target string, maxSeverity *int16) {
	t.Helper()
	if err := tn.configs.Create(context.Background(), &store.NotificationConfig{
		Role: role, Events: events, ChannelType: channelType, ChannelTarget: target, MaxSeverityLevel: maxSeverity,
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

func TestNotifier_Notify_MultiEventRule_MatchesEitherEvent(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	// One rule covering two events, rather than a separate row per event.
	tn.addRuleForEvents(t, store.OrgRoleAdmin,
		[]string{"sev.sla_at_risk", "sev.sla_breached"},
		store.NotificationChannelSlack, "#sla-alerts", nil)

	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.sla_at_risk", SEV: &store.SEV{ID: "sev-1", SeverityLevel: 1},
	})
	if tn.slack.calls != 1 {
		t.Fatalf("want 1 delivery for sev.sla_at_risk, got %d", tn.slack.calls)
	}

	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.sla_breached", SEV: &store.SEV{ID: "sev-1", SeverityLevel: 1},
	})
	if tn.slack.calls != 2 {
		t.Fatalf("want a 2nd delivery for sev.sla_breached (same rule, other event), got %d", tn.slack.calls)
	}

	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.created", SEV: &store.SEV{ID: "sev-1", SeverityLevel: 1},
	})
	if tn.slack.calls != 2 {
		t.Fatalf("sev.created is not in the rule's event list and should not match, got %d deliveries", tn.slack.calls)
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
	if err := tn.configs.Create(context.Background(), &store.NotificationConfig{
		Role: store.OrgRoleAdmin, Events: []string{"custom.event"}, ChannelType: store.NotificationChannelSlack, ChannelTarget: "#alerts",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{Type: "custom.event", Message: "hello"})
	if tn.slack.calls != 1 {
		t.Fatalf("want 1 delivery for a nil-SEV event against an unfiltered rule, got %d", tn.slack.calls)
	}
	if tn.slack.text != "hello" {
		t.Errorf("text = %q, want the overriding Message %q", tn.slack.text, "hello")
	}
}

func TestNotifier_Notify_MessageIncludesRealSEVIDNotJustSeverityLevel(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	tn.addRule(t, store.OrgRoleAdmin, "sev.created", store.NotificationChannelSlack, "#incidents", nil)

	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.created",
		SEV:  &store.SEV{ID: "SEV-2026-0042", Title: "Checkout down", SeverityLevel: 1, Status: store.SEVStatusOpen},
	})

	if !strings.Contains(tn.slack.text, "SEV-2026-0042") {
		t.Errorf("text = %q, want it to include the real case ID SEV-2026-0042, not just the severity level", tn.slack.text)
	}
}

func TestNotifier_Notify_SlackIncludesIncidentChannelMentionWhenSet(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	tn.addRule(t, store.OrgRoleAdmin, "sev.status_changed", store.NotificationChannelSlack, "#incidents", nil)

	channelID := "C0123456"
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.status_changed",
		SEV: &store.SEV{
			ID: "SEV-2026-0042", Title: "Checkout down", SeverityLevel: 1,
			Status: store.SEVStatusInvestigating, SlackChannelID: &channelID,
		},
	})

	if !strings.Contains(tn.slack.text, "<#C0123456>") {
		t.Errorf("text = %q, want it to include the incident channel mention <#C0123456>", tn.slack.text)
	}
}

func TestNotifier_Notify_SlackOmitsChannelMentionWhenUnset(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")
	tn.addRule(t, store.OrgRoleAdmin, "sev.created", store.NotificationChannelSlack, "#incidents", nil)

	// sev.created fires from the same event that triggers cmd/slackbot's
	// channel creation in a separate process — SlackChannelID is normally
	// still nil at this point (see notify.go's deliverSlack doc comment).
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.created",
		SEV:  &store.SEV{ID: "SEV-2026-0042", Title: "Checkout down", SeverityLevel: 1, Status: store.SEVStatusOpen},
	})

	if strings.Contains(tn.slack.text, "Incident channel") {
		t.Errorf("text = %q, want no channel mention when SlackChannelID is unset", tn.slack.text)
	}
}

func TestNotifier_Notify_EmailIncludesSlackDeepLinkWhenChannelSet(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedEmailConfig(t, "smtpuser", "smtppass")
	tn.addRule(t, store.OrgRoleAdmin, "sev.status_changed", store.NotificationChannelEmail, "oncall@example.com", nil)

	channelID := "C0123456"
	tn.notifier.Notify(context.Background(), grpchandler.NotifyEvent{
		Type: "sev.status_changed",
		SEV: &store.SEV{
			ID: "SEV-2026-0042", Title: "Checkout down", SeverityLevel: 1,
			Status: store.SEVStatusInvestigating, SlackChannelID: &channelID,
		},
	})

	if !strings.Contains(tn.email.body, "https://slack.com/app_redirect?channel=C0123456") {
		t.Errorf("body = %q, want it to include the Slack deep link", tn.email.body)
	}
}

func TestNotifier_Test_NilReceiver_NoOp(t *testing.T) {
	var n *grpchandler.Notifier
	if got := n.Test(context.Background(), &store.NotificationConfig{Events: []string{"sev.created"}}); got != nil {
		t.Errorf("want nil result from a nil *Notifier, got %v", got)
	}
}

func TestNotifier_Test_NilConfig_NoOp(t *testing.T) {
	tn := newTestNotifier(t)
	if got := tn.notifier.Test(context.Background(), nil); got != nil {
		t.Errorf("want nil result for a nil cfg, got %v", got)
	}
}

func TestNotifier_Test_OneResultPerEvent(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")

	cfg := &store.NotificationConfig{
		Role: store.OrgRoleAdmin, Events: []string{"sev.sla_at_risk", "sev.sla_breached"},
		ChannelType: store.NotificationChannelSlack, ChannelTarget: "#sla-alerts",
	}
	results := tn.notifier.Test(context.Background(), cfg)

	if len(results) != 2 {
		t.Fatalf("want 2 results (one per event), got %d", len(results))
	}
	for i, want := range []string{"sev.sla_at_risk", "sev.sla_breached"} {
		if results[i].Event != want {
			t.Errorf("results[%d].Event = %q, want %q", i, results[i].Event, want)
		}
		if results[i].Err != nil {
			t.Errorf("results[%d].Err = %v, want nil", i, results[i].Err)
		}
	}
	if tn.slack.calls != 2 {
		t.Errorf("want 2 slack deliveries, got %d", tn.slack.calls)
	}
	if tn.slack.channel != "#sla-alerts" {
		t.Errorf("channel = %q, want %q", tn.slack.channel, "#sla-alerts")
	}
}

func TestNotifier_Test_UnconfiguredIntegration_ReturnsError(t *testing.T) {
	tn := newTestNotifier(t)
	// No "slack" integration seeded at all.
	cfg := &store.NotificationConfig{
		Role: store.OrgRoleAdmin, Events: []string{"sev.created"},
		ChannelType: store.NotificationChannelSlack, ChannelTarget: "#incidents",
	}
	results := tn.notifier.Test(context.Background(), cfg)

	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Error("want a non-nil error when the slack integration isn't configured")
	}
	if tn.slack.calls != 0 {
		t.Errorf("want no delivery attempt to reach the fake sender, got %d calls", tn.slack.calls)
	}
}

func TestNotifier_Test_IgnoresListForEventAndMaxSeverityLevel(t *testing.T) {
	tn := newTestNotifier(t)
	tn.seedSlackConfig(t, "xoxb-fake")

	// No NotificationConfig rows exist in tn.configs at all — Test must not
	// consult ListForEvent, so this still delivers based purely on cfg's own
	// fields (and works for a rule that hasn't been saved yet, i.e. ID == 0).
	// MaxSeverityLevel is set here specifically to confirm Test ignores it —
	// there's no triggering SEV/severity for a manual test to filter against.
	cfg := &store.NotificationConfig{
		Role: store.OrgRoleAdmin, Events: []string{"sev.created"},
		ChannelType: store.NotificationChannelSlack, ChannelTarget: "#incidents",
		MaxSeverityLevel: int16p(1),
	}
	results := tn.notifier.Test(context.Background(), cfg)

	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("want 1 successful result even with no saved rule and a MaxSeverityLevel set, got %+v", results)
	}
	if tn.slack.calls != 1 {
		t.Errorf("want 1 delivery, got %d", tn.slack.calls)
	}
}

func TestNotifier_Test_EmptyEvents_ReturnsEmptyResults(t *testing.T) {
	tn := newTestNotifier(t)
	cfg := &store.NotificationConfig{
		Role: store.OrgRoleAdmin, ChannelType: store.NotificationChannelSlack, ChannelTarget: "#incidents",
	}
	if got := tn.notifier.Test(context.Background(), cfg); len(got) != 0 {
		t.Errorf("want 0 results for a cfg with no events, got %d", len(got))
	}
}
