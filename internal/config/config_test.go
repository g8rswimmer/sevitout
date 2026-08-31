package config

import (
	"log/slog"
	"testing"
)

// clearEnv unsets every variable Load reads, so each test starts from a
// clean slate regardless of what's set in the surrounding environment (e.g.
// a developer's local .env-derived shell) and regardless of test order.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"DATABASE_URL",
		"LOG_LEVEL",
		"JWT_SECRET",
		"ALLOW_INSECURE_JWT_SECRET",
		"JWT_TTL_HOURS",
		"PAGERDUTY_API_KEY",
		"GITHUB_TOKEN",
		"ENCRYPTION_KEY",
		"JIRA_CLOUD_ID",
		"JIRA_API_TOKEN",
		"JIRA_SITE_URL",
		"SLACKBOT_SERVICE_EMAIL",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := &Config{
		JWTTTLHours: defaultJWTTTLHours,
		LogLevel:    slog.LevelInfo,
	}
	if *cfg != *want {
		t.Errorf("Load() = %+v, want %+v", *cfg, *want)
	}
}

func TestLoad_ReadsEveryField(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("JWT_SECRET", "s3cret")
	t.Setenv("ALLOW_INSECURE_JWT_SECRET", "true")
	t.Setenv("JWT_TTL_HOURS", "48")
	t.Setenv("PAGERDUTY_API_KEY", "pd-key")
	t.Setenv("GITHUB_TOKEN", "gh-token")
	t.Setenv("ENCRYPTION_KEY", "enc-key")
	t.Setenv("JIRA_CLOUD_ID", "1a11d016-8984-4c3e-b9ab-142dd06acb1b")
	t.Setenv("JIRA_API_TOKEN", "jira-token")
	t.Setenv("JIRA_SITE_URL", "https://acme.atlassian.net")
	t.Setenv("SLACKBOT_SERVICE_EMAIL", "slackbot@example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	want := &Config{
		DatabaseURL:            "postgres://example",
		LogLevel:               slog.LevelDebug,
		JWTSecret:              "s3cret",
		AllowInsecureJWTSecret: true,
		JWTTTLHours:            48,
		PagerDutyAPIKey:        "pd-key",
		GitHubToken:            "gh-token",
		EncryptionKey:          "enc-key",
		JiraCloudID:            "1a11d016-8984-4c3e-b9ab-142dd06acb1b",
		JiraAPIToken:           "jira-token",
		JiraSiteURL:            "https://acme.atlassian.net",
		SlackbotServiceEmail:   "slackbot@example.com",
	}
	if *cfg != *want {
		t.Errorf("Load() = %+v, want %+v", *cfg, *want)
	}
}

func TestLoad_AllowInsecureJWTSecret_OnlyTrueEnables(t *testing.T) {
	cases := []string{"false", "TRUE", "1", "yes", ""}
	for _, v := range cases {
		clearEnv(t)
		t.Setenv("ALLOW_INSECURE_JWT_SECRET", v)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.AllowInsecureJWTSecret {
			t.Errorf("ALLOW_INSECURE_JWT_SECRET=%q: AllowInsecureJWTSecret = true, want false", v)
		}
	}
}

func TestLoad_JWTTTLHours(t *testing.T) {
	cases := []struct {
		name    string
		v       string
		want    int
		wantErr bool
	}{
		{"unset defaults", "", defaultJWTTTLHours, false},
		{"valid override", "12", 12, false},
		{"zero rejected", "0", 0, true},
		{"negative rejected", "-1", 0, true},
		{"non-numeric rejected", "soon", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearEnv(t)
			if c.v != "" {
				t.Setenv("JWT_TTL_HOURS", c.v)
			}
			cfg, err := Load()
			if c.wantErr {
				if err == nil {
					t.Fatalf("Load() error = nil, want error for JWT_TTL_HOURS=%q", c.v)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if cfg.JWTTTLHours != c.want {
				t.Errorf("JWTTTLHours = %d, want %d", cfg.JWTTTLHours, c.want)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"  debug  ", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"nonsense", slog.LevelInfo},
	}
	for _, c := range cases {
		if got := ParseLogLevel(c.in); got != c.want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
