package catalog_test

import (
	"testing"

	"github.com/g8rswimmer/sevitout/internal/integrations/catalog"
)

func TestAll_TypesAreUniqueAndNonEmpty(t *testing.T) {
	seen := make(map[string]bool)
	for _, i := range catalog.All {
		if i.Type == "" {
			t.Errorf("integration with empty Type (Label=%q)", i.Label)
		}
		if i.Label == "" {
			t.Errorf("integration %q has empty Label", i.Type)
		}
		if seen[i.Type] {
			t.Errorf("duplicate integration Type %q", i.Type)
		}
		seen[i.Type] = true
	}
}

func TestAll_FieldKeysAreUniqueWithinEachIntegration(t *testing.T) {
	for _, i := range catalog.All {
		seen := make(map[string]bool)
		for _, f := range append(append([]catalog.Field{}, i.CredentialFields...), i.SettingsFields...) {
			if f.Key == "" {
				t.Errorf("%s: field with empty Key (Label=%q)", i.Type, f.Label)
			}
			if f.Label == "" {
				t.Errorf("%s: field %q has empty Label", i.Type, f.Key)
			}
			if seen[f.Key] {
				t.Errorf("%s: duplicate field key %q", i.Type, f.Key)
			}
			seen[f.Key] = true
		}
	}
}

func TestAll_SelectFieldsHaveOptions_NonSelectFieldsDoNot(t *testing.T) {
	for _, i := range catalog.All {
		for _, f := range append(append([]catalog.Field{}, i.CredentialFields...), i.SettingsFields...) {
			switch f.Kind {
			case catalog.KindSelect:
				if len(f.Options) < 2 {
					t.Errorf("%s.%s: select field must list at least 2 options, got %v", i.Type, f.Key, f.Options)
				}
			case catalog.KindText, catalog.KindSecret:
				if len(f.Options) != 0 {
					t.Errorf("%s.%s: non-select field (%s) must not carry Options, got %v", i.Type, f.Key, f.Kind, f.Options)
				}
			default:
				t.Errorf("%s.%s: unrecognized Kind %q", i.Type, f.Key, f.Kind)
			}
		}
	}
}

func TestAll_CredentialFieldsAreSecret_SettingsFieldsAreNot(t *testing.T) {
	for _, i := range catalog.All {
		for _, f := range i.CredentialFields {
			if f.Kind != catalog.KindSecret {
				t.Errorf("%s: credential field %q has Kind %q, want %q", i.Type, f.Key, f.Kind, catalog.KindSecret)
			}
		}
		for _, f := range i.SettingsFields {
			if f.Kind == catalog.KindSecret {
				t.Errorf("%s: settings field %q must not be KindSecret (credentials only)", i.Type, f.Key)
			}
		}
	}
}

func TestAll_MonitoringHasNoCredentials(t *testing.T) {
	monitoring, ok := catalog.Find("monitoring")
	if !ok {
		t.Fatal(`catalog.Find("monitoring") not found`)
	}
	if len(monitoring.CredentialFields) != 0 {
		t.Errorf("monitoring should have no credential fields, got %v", monitoring.CredentialFields)
	}
}

func TestAll_FixedOrder(t *testing.T) {
	want := []string{"pagerduty", "github", "slack", "jira", "email", "monitoring"}
	if len(catalog.All) != len(want) {
		t.Fatalf("len(catalog.All) = %d, want %d", len(catalog.All), len(want))
	}
	for idx, i := range catalog.All {
		if i.Type != want[idx] {
			t.Errorf("catalog.All[%d].Type = %q, want %q", idx, i.Type, want[idx])
		}
	}
}

func TestFind_UnknownType(t *testing.T) {
	if _, ok := catalog.Find("datadog-nonexistent"); ok {
		t.Error("Find of an unregistered type should return ok=false")
	}
}

func TestIntegration_CredentialKeys(t *testing.T) {
	slack, ok := catalog.Find("slack")
	if !ok {
		t.Fatal(`catalog.Find("slack") not found`)
	}
	keys := slack.CredentialKeys()
	for _, want := range []string{"bot_token", "app_token"} {
		if !keys[want] {
			t.Errorf("slack.CredentialKeys() missing %q", want)
		}
	}
	if keys["cloud_id"] {
		t.Error("slack.CredentialKeys() should not contain a jira key")
	}
}

func TestIntegration_SettingsField(t *testing.T) {
	monitoring, ok := catalog.Find("monitoring")
	if !ok {
		t.Fatal(`catalog.Find("monitoring") not found`)
	}
	tool, ok := monitoring.SettingsField("tool")
	if !ok {
		t.Fatal(`monitoring.SettingsField("tool") not found`)
	}
	if tool.Kind != catalog.KindSelect {
		t.Errorf("tool.Kind = %q, want %q", tool.Kind, catalog.KindSelect)
	}
	if _, ok := monitoring.SettingsField("nonexistent"); ok {
		t.Error("SettingsField of an unknown key should return ok=false")
	}
}
