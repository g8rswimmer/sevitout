// Package config centralizes the environment-variable-driven configuration
// cmd/server's main() needs at startup — replacing what was previously a
// scatter of os.Getenv calls spread across main() itself.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// defaultJWTTTLHours is used when JWT_TTL_HOURS is unset, matching the
// value documented in README.md's environment-variable table.
const defaultJWTTTLHours = 24

// Config holds every environment-variable-derived setting cmd/server's
// main() needs. Fields correspond directly to the env vars documented in
// README.md's "Environment variables" table.
type Config struct {
	// DatabaseURL is DATABASE_URL, the PostgreSQL connection string. Empty
	// means "use the in-memory store" — that decision belongs to
	// cmd/server/main.go's buildStores, not to this package.
	DatabaseURL string

	// LogLevel is parsed from LOG_LEVEL via ParseLogLevel.
	LogLevel slog.Level

	// JWTSecret is the raw JWT_SECRET value, which may be empty. Whether an
	// empty secret is fatal (governed by AllowInsecureJWTSecret) is decided
	// in cmd/server/main.go — a fail-closed security decision kept visible
	// at its call site rather than folded into this generic loader.
	JWTSecret string

	// AllowInsecureJWTSecret is ALLOW_INSECURE_JWT_SECRET == "true".
	AllowInsecureJWTSecret bool

	// JWTTTLHours is JWT_TTL_HOURS, defaulting to defaultJWTTTLHours when
	// unset. A value that's set but not a positive integer is reported by
	// Load as an error rather than silently falling back to the default.
	JWTTTLHours int

	// PagerDutyAPIKey is PAGERDUTY_API_KEY; the PagerDuty on-call lookup
	// integration is enabled when this is non-empty.
	PagerDutyAPIKey string

	// GitHubToken is GITHUB_TOKEN; the GitHub Issues integration is enabled
	// when this is non-empty.
	GitHubToken string

	// EncryptionKey is the raw ENCRYPTION_KEY value (base64-encoded 32
	// bytes). Decoding it (via internal/store/crypto.DecodeKey) stays in
	// cmd/server/main.go so this package has no dependency on the store
	// layer.
	EncryptionKey string

	// JiraCloudID is JIRA_CLOUD_ID — the target Jira Cloud tenant's Cloud ID
	// (a UUID, not its site name; see
	// https://support.atlassian.com/user-management/docs/manage-api-tokens-for-service-accounts/
	// for how to find it), and JiraAPIToken is JIRA_API_TOKEN, sent as a
	// Bearer token — Jira Cloud's REST API v3 gateway (api.atlassian.com)
	// accepts Bearer auth, not HTTP Basic Auth, so no account email is
	// needed alongside it (see internal/integrations/tasktracker/jira). The
	// Jira Issues integration is enabled only when both are non-empty;
	// unlike GitHub's single GITHUB_TOKEN, a Cloud ID is required because
	// Jira Cloud instances are tenant-specific, with no shared production
	// API host to default to.
	JiraCloudID  string
	JiraAPIToken string

	// JiraSiteURL is JIRA_SITE_URL (e.g. "https://acme.atlassian.net") —
	// optional, and independent of whether the Jira integration itself is
	// enabled (that's governed by JiraCloudID/JiraAPIToken alone). It's
	// used purely to build human-facing "browse" links on created/fetched
	// Jira issues (internal/integrations/tasktracker/jira.Client.NewClient's
	// siteURL parameter) — the Cloud ID used for actual API calls doesn't
	// determine the tenant's site host, so this has to be supplied
	// separately if a clickable link is wanted. Left unset, issue links
	// fall back to the API's own non-browsable resource URL.
	JiraSiteURL string
}

// Load reads every environment variable cmd/server's main() needs into a
// Config. It performs no I/O beyond os.Getenv and never calls os.Exit —
// every problem it finds (currently: a JWT_TTL_HOURS that's set but not a
// positive integer) is reported via the returned error, leaving the
// decision of whether and how to exit to the caller. This keeps Load
// unit-testable in isolation from a real process exit, and keeps main() the
// single place that decides to exit.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		LogLevel:               ParseLogLevel(os.Getenv("LOG_LEVEL")),
		JWTSecret:              os.Getenv("JWT_SECRET"),
		AllowInsecureJWTSecret: os.Getenv("ALLOW_INSECURE_JWT_SECRET") == "true",
		JWTTTLHours:            defaultJWTTTLHours,
		PagerDutyAPIKey:        os.Getenv("PAGERDUTY_API_KEY"),
		GitHubToken:            os.Getenv("GITHUB_TOKEN"),
		EncryptionKey:          os.Getenv("ENCRYPTION_KEY"),
		JiraCloudID:            os.Getenv("JIRA_CLOUD_ID"),
		JiraAPIToken:           os.Getenv("JIRA_API_TOKEN"),
		JiraSiteURL:            os.Getenv("JIRA_SITE_URL"),
	}

	if v := os.Getenv("JWT_TTL_HOURS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("JWT_TTL_HOURS must be a positive integer, got %q", v)
		}
		cfg.JWTTTLHours = n
	}

	return cfg, nil
}

// ParseLogLevel maps LOG_LEVEL's value ("debug", "info", "warn"/"warning",
// "error", case-insensitive) to a slog.Level, defaulting to Info for an
// empty or unrecognized value so a typo degrades gracefully instead of
// silencing every log line.
func ParseLogLevel(v string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
