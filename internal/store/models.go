package store

import "time"

// SEVStatus represents the lifecycle state of a SEV.
type SEVStatus string

const (
	SEVStatusOpen                 SEVStatus = "open"
	SEVStatusInvestigating        SEVStatus = "investigating"
	SEVStatusMitigated            SEVStatus = "mitigated"
	SEVStatusResolved             SEVStatus = "resolved"
	SEVStatusPostmortemInProgress SEVStatus = "postmortem_in_progress"
	SEVStatusPostmortemComplete   SEVStatus = "postmortem_complete"
)

// SEVRoleType identifies the role a person holds on a SEV.
type SEVRoleType string

const (
	SEVRoleOnCall            SEVRoleType = "on-call"
	SEVRoleDetectedBy        SEVRoleType = "detected-by"
	SEVRoleIncidentCommander SEVRoleType = "incident-commander"
	SEVRoleCommsLead         SEVRoleType = "comms-lead"
	SEVRoleRecorder          SEVRoleType = "recorder"
	SEVRoleResponder         SEVRoleType = "responder"
)

// AudienceType controls who can see an announcement.
type AudienceType string

const (
	AudienceInternal   AudienceType = "internal"
	AudienceExternal   AudienceType = "external"
	AudienceStatusPage AudienceType = "status-page"
)

// TaskRelationshipType describes how a linked task relates to a SEV.
type TaskRelationshipType string

const (
	TaskRelationshipActionItem         TaskRelationshipType = "action-item"
	TaskRelationshipContributingFactor TaskRelationshipType = "contributing-factor"
	TaskRelationshipFollowUp           TaskRelationshipType = "follow-up"
	TaskRelationshipDependency         TaskRelationshipType = "dependency"
)

// TaskPriority controls the default SLA due-date applied at link time.
type TaskPriority string

const (
	TaskPriorityCritical    TaskPriority = "critical"
	TaskPriorityNonCritical TaskPriority = "non-critical"
)

// SEVRelationshipType describes how two SEVs relate to each other.
type SEVRelationshipType string

const (
	SEVRelationshipRelated      SEVRelationshipType = "related"
	SEVRelationshipCausedBy     SEVRelationshipType = "caused-by"
	SEVRelationshipDuplicate    SEVRelationshipType = "duplicate"
	SEVRelationshipRecurrenceOf SEVRelationshipType = "recurrence-of"
)

// PostmortemStatus is the lifecycle state of a postmortem document.
type PostmortemStatus string

const (
	PostmortemStatusDraft    PostmortemStatus = "draft"
	PostmortemStatusInReview PostmortemStatus = "in-review"
	PostmortemStatusApproved PostmortemStatus = "approved"
)

// OrgRole is the organisation-wide role assigned to a user.
type OrgRole string

const (
	OrgRoleViewer            OrgRole = "viewer"
	OrgRoleResponder         OrgRole = "responder"
	OrgRoleIncidentCommander OrgRole = "incident-commander"
	OrgRoleAdmin             OrgRole = "admin"
)

// AIHandlerType indicates whether a plugin is built-in or calls an HTTP endpoint.
type AIHandlerType string

const (
	AIHandlerBuiltin AIHandlerType = "builtin"
	AIHandlerHTTP    AIHandlerType = "http"
)

// DetectionMethod is how a SEV was first identified (docs/requirements.md §4.2).
// This vocabulary is closed and validated in internal/api/grpc/sev.go the same
// way role.go validates SEVRoleType — as is MonitoringTool below.
type DetectionMethod string

const (
	DetectionMethodAlert               DetectionMethod = "alert"
	DetectionMethodMonitoringDashboard DetectionMethod = "monitoring-dashboard"
	DetectionMethodCustomerReport      DetectionMethod = "customer-report"
	DetectionMethodSyntheticTest       DetectionMethod = "synthetic-test"
	DetectionMethodManualDiscovery     DetectionMethod = "manual-discovery"
	DetectionMethodSlackEscalation     DetectionMethod = "slack-escalation"
)

// MonitoringTool identifies which monitoring platform detected/tracks a SEV
// (docs/requirements.md §13.4). Closed and validated in internal/api/grpc/sev.go
// via validateMonitoringTool, the same pattern DetectionMethod uses above —
// "other" is itself a valid closed value (there's no companion free-text label
// for it; a caller that needs to name a specific tool outside this list can
// still say so in AlertName or the SEV description).
type MonitoringTool string

const (
	MonitoringToolDatadog    MonitoringTool = "datadog"
	MonitoringToolPrometheus MonitoringTool = "prometheus"
	MonitoringToolCloudWatch MonitoringTool = "cloudwatch"
	MonitoringToolOther      MonitoringTool = "other"
)

// SEV is the central incident record.
type SEV struct {
	ID                   string
	Title                string
	Description          string
	SeverityLevel        int16
	Status               SEVStatus
	RootCauseCategory    *string
	RootCauseDescription *string
	Mitigation           *string
	Prevention           *string
	BusinessImpact       *string
	AffectedServices     []string
	DetectionMethod      *string
	AlertName            *string
	MonitoringTool       *string
	// AlertURL, DashboardURL, Query, and SnapshotURL are optional supporting
	// detail for the detection metadata above — the alert that fired, a link
	// to the monitoring dashboard, a saved query/expression run against
	// MonitoringTool (e.g. a PromQL or Datadog query string — deliberately
	// not a URL), and a snapshot image of the chart, respectively.
	// AlertURL/DashboardURL/SnapshotURL are plain URLs (no file upload/blob
	// storage — see docs/requirements.md §13.4's "link a dashboard URL or
	// saved query" framing, which is exactly the two concepts DashboardURL
	// and Query split apart).
	AlertURL     *string
	DashboardURL *string
	Query        *string
	SnapshotURL  *string
	// GitHubRepo is the "owner/repo" this SEV's code lives in (e.g.
	// "acme-corp/checkout-service") — shown as a link in the Details panel,
	// and used to pre-fill TaskService.CreateGitHubIssue's owner/repo fields
	// so a Responder doesn't have to retype it.
	GitHubRepo *string
	// RootCauseReferenceURL links to the concrete change that caused the
	// incident — e.g. a PR/commit diff or a config-management change —
	// alongside RootCauseCategory/RootCauseDescription's classification and
	// narrative.
	RootCauseReferenceURL *string
	RightPeoplePresent    *bool
	RightPeopleNotes      *string
	Tags                  map[string]string
	StartedAt             *time.Time
	DetectedAt            *time.Time
	MitigatedAt           *time.Time
	ResolvedAt            *time.Time
	PostmortemCompletedAt *time.Time
	MTTDSeconds           *int64
	MTTMSeconds           *int64
	MTTRSeconds           *int64
	DTTMSeconds           *int64
	// RTPCSeconds is Resolution to Postmortem Complete — the same "point A to
	// point B" shape as DTTMSeconds above (postmortem_completed_at −
	// resolved_at), not "from StartedAt" like MTTD/MTTM/MTTR (Phase 12
	// follow-up: an SLA target teams want on the postmortem tail, not just
	// incident response). Measured from ResolvedAt, not MitigatedAt — the
	// postmortem clock starts once the incident itself is resolved.
	RTPCSeconds *int64
	Locked      bool
	Sensitive   bool
	// AIDisabled opts this specific SEV out of all AI plugin dispatch
	// (proactive and user-triggered), independent of the global per-plugin
	// enabled/trigger flags. See docs/requirements.md §11.3.
	AIDisabled bool
	// SlackChannelID is the incident channel cmd/slackbot auto-created for
	// this SEV, written back via UpdateSEV right after creation (§13.1,
	// docs/roadmap.md Phase 10e). Nil for SEVs created before this shipped,
	// and for any SEV whose channel-creation step failed or never ran (e.g.
	// Slack not configured) — callers must treat a nil value as "no channel
	// to invite/join", not an error.
	SlackChannelID *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      string
}

// SEVSortField selects the column SEVStore.List orders results by.
type SEVSortField string

const (
	SEVSortStartedAt SEVSortField = "started_at"
	SEVSortSeverity  SEVSortField = "severity"
	SEVSortMTTR      SEVSortField = "mttr"
	SEVSortUpdatedAt SEVSortField = "updated_at"
)

// SEVFilter narrows the result set returned by SEVStore.List.
type SEVFilter struct {
	SeverityLevels    []int16
	Statuses          []SEVStatus
	OnCallUser        string
	ServiceIDs        []string
	Tags              map[string]string
	RootCauseCategory string
	StartedAfter      *time.Time
	StartedBefore     *time.Time
	// IDs, when non-nil, restricts results to this set of SEV IDs (an empty
	// but non-nil slice matches nothing). Lets callers compose filters
	// computed from other stores (e.g. role assignments, announcement text
	// matches) without SEVStore needing to know about them.
	IDs    []string
	Search string
	// ExcludeSensitive drops SEVs with Sensitive==true from the result set.
	// There is no per-user visibility/ACL mechanism for sensitive SEVs yet
	// (see docs/requirements.md §14), so callers that surface SEVs by
	// keyword/content match (e.g. SearchService) set this to avoid making
	// sensitive SEVs newly discoverable; callers that already scope access
	// some other way (e.g. GetSEV by known ID) leave it false.
	ExcludeSensitive bool
	// Sort selects the ordering column; the zero value preserves the legacy
	// default (most recently created first).
	Sort     SEVSortField
	SortDesc bool
	Limit    int
	Offset   int
}

// SEVStatusHistory is one entry in the immutable status-transition log.
type SEVStatusHistory struct {
	ID             int64
	SEVID          string
	FromStatus     *SEVStatus
	ToStatus       SEVStatus
	UserID         string
	TransitionedAt time.Time
}

// SEVRole is a person assigned to a named role on a SEV.
type SEVRole struct {
	ID          int64
	SEVID       string
	RoleType    SEVRoleType
	UserID      *string
	DisplayName string
	CreatedAt   time.Time
	CreatedBy   string
}

// SEVAccess is an explicit per-user visibility grant on a SEV flagged
// Sensitive (§14: "only explicitly added users can view"). It is consulted
// only when the SEV's own Sensitive flag is true — see
// internal/api/grpc.sensitiveSEVVisible.
type SEVAccess struct {
	ID        int64
	SEVID     string
	UserID    string
	CreatedAt time.Time
	CreatedBy string
}

// Announcement is a time-ordered status update on a SEV.
type Announcement struct {
	ID          int64
	SEVID       string
	AuthorID    string
	Message     string
	Audience    AudienceType
	IsMilestone bool
	CreatedAt   time.Time
}

// ChatEntry is one captured message from an incident communication channel.
type ChatEntry struct {
	ID         int64
	SEVID      string
	OccurredAt time.Time
	Source     string
	Author     string
	Content    string
	AddedAt    time.Time
	AddedBy    string
}

// LinkedTask is an external task (e.g., GitHub Issue) linked to a SEV.
type LinkedTask struct {
	ID               int64
	SEVID            string
	ExternalSystem   string
	TaskID           string
	URL              string
	Title            string
	Description      *string
	RelationshipType TaskRelationshipType
	Priority         TaskPriority
	DueDate          *time.Time
	Overdue          bool
	// Assignee is the tracker-native assignee identifier set at issue-creation
	// time (a GitHub login, or a Jira account ID) — see
	// TaskServer.CreateGitHubIssue/CreateJiraIssue (docs/roadmap.md Phase
	// 10f). Nil for tasks linked via plain LinkTask, or created before this
	// field existed.
	Assignee *string
	// AssigneeName is the Sevitout display name of the assignee, resolved
	// server-side at creation time from the assignee_user_id the picker
	// sends — a snapshot, not a live lookup (the same trade-off already
	// accepted for SEVRole.DisplayName), so it's what the UI shows instead
	// of Assignee's opaque tracker-native value. Nil when the assignee
	// wasn't picked from Sevitout's own directory (e.g. a raw API call);
	// callers should fall back to Assignee for display in that case.
	AssigneeName *string
	CreatedAt    time.Time
	CreatedBy    string
}

// SEVLink is a typed directional relationship between two SEVs.
// Bidirectionality is maintained by the application: linking A→B also inserts B→A.
type SEVLink struct {
	ID               int64
	SourceSEVID      string
	TargetSEVID      string
	RelationshipType SEVRelationshipType
	CreatedAt        time.Time
	CreatedBy        string
}

// SLI is one SLI violation record on a SEV.
type SLI struct {
	ID             int64
	SEVID          string
	ServiceID      *string
	SLIName        string
	SLOThreshold   string
	MeasuredValue  string
	ViolationStart *time.Time
	ViolationEnd   *time.Time
	DashboardURL   *string
	CreatedAt      time.Time
}

// Postmortem is the required postmortem document attached to every SEV.
type Postmortem struct {
	ID        int64
	SEVID     string
	Status    PostmortemStatus
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
	UpdatedBy *string
}

// AuditEntry is one immutable entry in the append-only audit log.
type AuditEntry struct {
	ID        int64
	SEVID     string
	UserID    string
	Action    string
	FieldName *string
	OldValue  *string
	NewValue  *string
	CreatedAt time.Time
}

// AIPlugin is a registered AI provider plugin configuration.
type AIPlugin struct {
	ID                        int64
	Name                      string
	Version                   string
	Description               *string
	HandlerType               AIHandlerType
	HTTPEndpoint              *string
	Provider                  *string
	Model                     *string
	EncryptedAPIKey           []byte
	Enabled                   bool
	TriggerOnOpen             bool
	TriggerOnMitigated        bool
	TriggerOnResolved         bool
	TriggerOnPostmortemReview bool
	// RateLimitPerMinute caps how many AI actions this plugin may run per
	// minute across the whole org; 0 means unlimited. Enforced by
	// internal/ai.Dispatcher, not at the store layer.
	RateLimitPerMinute int32
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// AIOutput is one stored result of an AI plugin action against a SEV —
// either a proactive lifecycle trigger or a user-triggered action. Outputs
// are additive and never mutate the SEV record directly (§11).
type AIOutput struct {
	ID       int64
	SEVID    string
	PluginID int64
	// TriggerEvent is the lifecycle event that caused this output (e.g.
	// "sev.opened", "sev.mitigated", "sev.resolved",
	// "postmortem.in_review"), or "manual" for a user-triggered action.
	TriggerEvent string
	// Action is the Provider method invoked (e.g. "summarize",
	// "suggest_root_cause"); see internal/ai.Action.
	Action    string
	Content   string
	CreatedAt time.Time
}

// Service is an entry in the lightweight internal service registry.
type Service struct {
	ID                 string
	Name               string
	Description        *string
	OwningTeam         *string
	PagerDutyServiceID *string
	Tags               map[string]string
	Active             bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// ServiceSLA defines the target response times for one service at one
// severity level (1-4) — docs/roadmap.md Phase 12. A nil target field means
// that metric has no SLA configured for this service/severity; EvaluateSLA
// (internal/sev/sla.go) treats it as not applicable rather than an instant
// breach.
type ServiceSLA struct {
	ID                int64
	ServiceID         string
	SeverityLevel     int16
	MTTDTargetSeconds *int64
	MTTMTargetSeconds *int64
	MTTRTargetSeconds *int64
	// RTPCTargetSeconds targets RTPCSeconds (Resolution to Postmortem
	// Complete) on SEV — see that field's doc comment.
	RTPCTargetSeconds *int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ServiceLevelingCriteria is free-text guidance for what qualifies as one
// severity level for one service (docs/roadmap.md Phase 14) — e.g. "SEV-1
// for checkout: >50% of checkout traffic failing." Purely advisory: no
// domain logic reads or evaluates this (contrast ServiceSLA, which
// internal/sev/sla.go's EvaluateSLA dereferences on every SEV read).
// Surfaced to a human on SEV creation (SevCreatePage.tsx, to help pick the
// right level) and again, read-only, on the postmortem page
// (PostmortemPage.tsx, to help confirm the level chosen was correct) — never
// enforced or validated against.
type ServiceLevelingCriteria struct {
	ID            int64
	ServiceID     string
	SeverityLevel int16
	Criteria      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// User is a registered user who authenticates with email and password.
type User struct {
	ID           string
	Email        string
	Name         string
	AvatarURL    *string
	OrgRole      OrgRole
	Active       bool
	PasswordHash string
	// SlackUserID, GitHubUsername, and JiraAccountID are self-service
	// integration identities (docs/requirements.md's Slack/GitHub/Jira
	// integrations, docs/roadmap.md Phase 10) a user manages for themselves
	// via AuthService.UpdateMyIntegrationIdentities. Nullable, no uniqueness
	// constraint — a stale/duplicate value just resolves to the wrong/no
	// invite or assignee, not an integrity risk worth enforcing.
	SlackUserID    *string
	GitHubUsername *string
	JiraAccountID  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// OnCallRotation defines a named on-call entry, with optional PagerDuty backing and manual overrides.
type OnCallRotation struct {
	ID                  int64
	Name                string
	ServiceID           *string
	PagerDutyScheduleID *string
	ManualUserID        *string
	ManualDisplayName   *string
	OverrideStart       *time.Time
	OverrideEnd         *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// IntegrationConfig holds credentials and settings for one third-party integration.
// Credentials are stored encrypted at the application layer; this struct holds the
// encrypted bytes as returned by the store.
type IntegrationConfig struct {
	ID                   int64
	IntegrationType      string
	EncryptedCredentials []byte
	Settings             map[string]any
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// RetentionConfig is the retention policy for one severity level.
// RetentionDays == 0 means retain forever (the default for every level).
type RetentionConfig struct {
	ID            int64
	SeverityLevel int16
	RetentionDays int
	// HardDelete controls what happens to a SEV on expiry: false (default)
	// archives it (soft-delete); true purges it permanently.
	HardDelete bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ShareableLink is a public, revocable read-only token for a SEV.
type ShareableLink struct {
	ID        int64
	SEVID     string
	Token     string
	CreatedBy string
	ExpiresAt *time.Time
	Revoked   bool
	RevokedBy *string
	RevokedAt *time.Time
	CreatedAt time.Time
}
