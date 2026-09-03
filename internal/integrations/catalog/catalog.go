// Package catalog is the single source of truth for which integration_types
// exist, what credential/settings keys each one accepts, and how the admin
// UI should render each field (plain text, password-masked secret, or a
// closed select). It is a dependency-free static registry, not a client for
// any one integration — deliberately outside internal/integrations/{slack,
// pagerduty,...} — so internal/api/grpc can import it without a cycle, and
// it's importable in turn by anything else that needs to know the fixed
// integration set (docs/roadmap.md Phase 9).
//
// Every entry's storage keys reuse the convention already in place before
// this package existed (see cmd/server's *HealthChecker types and
// internal/api/grpc/config_integration.go) — no data migration is needed.
package catalog

// Kind describes how a Field should be rendered and, for Select, validated.
type Kind string

const (
	// KindText is a plain, visible text input (e.g. a Jira Cloud ID).
	KindText Kind = "text"
	// KindSecret is a credential — rendered password-masked, write-only
	// (never returned decrypted by any RPC; see
	// internal/api/grpc/config_integration.go's DecryptIntegrationCredentials
	// doc comment for the one deliberate exception).
	KindSecret Kind = "secret"
	// KindSelect is a closed set of values; Options lists them.
	KindSelect Kind = "select"
)

// Field describes one credential or settings key.
type Field struct {
	// Key is the storage key exactly as read/written today — e.g.
	// "bot_token", "cloud_id" — never itself displayed to an admin.
	Key string
	// Label is the human-facing field name — e.g. "Bot Token" for "bot_token".
	Label string
	Kind  Kind
	// Required is a UI-only affordance in this phase — see
	// UpsertIntegrationConfig's validation doc comment for why it isn't
	// enforced server-side at upsert time.
	Required bool
	// Help, when non-empty, is a short explanatory line shown under the
	// field (e.g. where to find a Jira Cloud ID).
	Help string
	// Options lists the valid values for a KindSelect field; empty for
	// every other Kind.
	Options []string
}

// Integration describes one fixed integration_type's complete field schema.
type Integration struct {
	// Type is the integration_type value stored/queried today (e.g.
	// "pagerduty") — never itself displayed to an admin (see Label).
	Type string
	// Label is the human-facing integration name — e.g. "PagerDuty".
	Label string
	// CredentialFields holds this integration's write-only, encrypted
	// storage keys — empty for a settings-only integration (Monitoring).
	CredentialFields []Field
	// SettingsFields holds this integration's non-secret storage keys.
	SettingsFields []Field
}

// All is the fixed, ordered set of every integration_type the admin UI and
// UpsertIntegrationConfig's validation recognize. Order matters: it's the
// order the admin page's sidebar renders in. A 6th integration is a
// one-file, one-entry change here — the catalog is static Go code, not
// itself admin-editable (see demo/admin-integrations-settings.md's Known
// limitations for why that's an accepted trade-off, not a gap).
var All = []Integration{
	{
		Type:  "pagerduty",
		Label: "PagerDuty",
		CredentialFields: []Field{
			{Key: "api_key", Label: "API Key", Kind: KindSecret, Required: true},
		},
	},
	{
		Type:  "github",
		Label: "GitHub",
		CredentialFields: []Field{
			{Key: "token", Label: "Token", Kind: KindSecret, Required: true, Help: "A PAT with repo scope"},
		},
	},
	{
		Type:  "slack",
		Label: "Slack",
		CredentialFields: []Field{
			{Key: "bot_token", Label: "Bot Token", Kind: KindSecret, Required: true},
			{Key: "app_token", Label: "App Token", Kind: KindSecret, Required: true},
		},
		SettingsFields: []Field{
			{Key: "default_channel", Label: "Default Channel", Kind: KindText},
			{Key: "channel_naming_convention", Label: "Channel Naming Convention", Kind: KindText},
		},
	},
	{
		Type:  "jira",
		Label: "Jira",
		CredentialFields: []Field{
			{Key: "api_token", Label: "API Token", Kind: KindSecret, Required: true},
		},
		SettingsFields: []Field{
			{Key: "cloud_id", Label: "Cloud ID", Kind: KindText, Required: true, Help: "The Jira Cloud tenant's Cloud ID (a UUID) — find it under admin.atlassian.com, not the site's *.atlassian.net name"},
			{Key: "site_url", Label: "Site URL", Kind: KindText, Help: "e.g. https://acme.atlassian.net — used only to build clickable issue links"},
		},
	},
	{
		// Email backs the Slack/email routing rules configured in
		// ConfigService's NotificationConfig RPCs (docs/roadmap.md Phase 15).
		// Only username/password are genuinely secret; host/port/from_address
		// are plain configuration (like Jira's cloud_id/site_url), so they're
		// settings, not credentials — matching this file's convention that
		// every CredentialField is KindSecret and no SettingsField is.
		Type:  "email",
		Label: "Email",
		CredentialFields: []Field{
			{Key: "smtp_username", Label: "SMTP Username", Kind: KindSecret, Help: "Leave blank for an unauthenticated relay"},
			{Key: "smtp_password", Label: "SMTP Password", Kind: KindSecret},
		},
		SettingsFields: []Field{
			{Key: "smtp_host", Label: "SMTP Host", Kind: KindText, Required: true},
			{Key: "smtp_port", Label: "SMTP Port", Kind: KindText, Required: true, Help: "e.g. 587 for STARTTLS"},
			{Key: "from_address", Label: "From Address", Kind: KindText, Required: true},
		},
	},
	{
		// Monitoring has no credentials at all — deliberately, per
		// docs/requirements.md §18.4: it's tool type + base URL only, with
		// no live health check (nothing to poll) and no new integration
		// client. Closes the one integration in §18.4 that had no admin UI
		// at all before this phase.
		Type:  "monitoring",
		Label: "Monitoring",
		SettingsFields: []Field{
			{
				Key: "tool", Label: "Tool", Kind: KindSelect, Required: true,
				// Deliberately no "other" option: there's no base_url shape
				// to assume for an unnamed tool (see this field's doc
				// comment in docs/roadmap.md Phase 9).
				Options: []string{"datadog", "prometheus", "cloudwatch"},
			},
			{Key: "base_url", Label: "Base URL", Kind: KindText},
		},
	},
}

// Find returns the Integration for integrationType, or (zero, false) if
// integrationType isn't in All.
func Find(integrationType string) (Integration, bool) {
	for _, i := range All {
		if i.Type == integrationType {
			return i, true
		}
	}
	return Integration{}, false
}

// CredentialKeys returns the set of valid credential keys for i, as a
// membership map for O(1) lookup.
func (i Integration) CredentialKeys() map[string]bool {
	return fieldKeySet(i.CredentialFields)
}

// SettingsField returns the Field describing key within i.SettingsFields,
// or (zero, false) if key isn't a recognized settings key for i.
func (i Integration) SettingsField(key string) (Field, bool) {
	for _, f := range i.SettingsFields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

func fieldKeySet(fields []Field) map[string]bool {
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		set[f.Key] = true
	}
	return set
}
