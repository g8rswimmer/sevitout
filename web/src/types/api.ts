// Wire types for the REST endpoints this app calls, mirroring the proto
// messages under proto/sevitout/v1/*.proto. The gateway marshals with
// protojson + UseProtoNames, so JSON keys are snake_case (matching the .proto
// field names verbatim) rather than camelCase. Per the protojson spec, int64
// fields are encoded as JSON strings, not numbers — confirmed against a live
// server for both the *_seconds duration fields and every sub-resource's
// database `id` (roles, announcements, chat entries, tasks, SEV links — all
// `int64` in proto, e.g. role.proto's SEVRoleResponse.id). Every such field
// is typed `string` here for that reason, even though it looks numeric.
//
// protojson also omits any proto3 scalar/repeated/map field holding its zero
// value ("", 0, false, [], {}) from the JSON output entirely, rather than
// emitting an explicit zero — confirmed against a live response in
// demo/M14a-shell-auth-dashboard.md's walkthrough. Every field below that
// isn't guaranteed non-zero (an ID, an enum-like status string, a count that
// only appears once positive) is typed optional for that reason, even though
// the corresponding proto field isn't itself `optional`. Code reading these
// must not assume a scalar field is present just because "the type says
// string" — use `?? fallback` or optional chaining, as DashboardPage.tsx and
// lib/format.ts's formatters do.

export type OrgRole = 'viewer' | 'responder' | 'incident-commander' | 'admin'

export const ORG_ROLE_LABELS: Record<OrgRole, string> = {
  viewer: 'Viewer',
  responder: 'Responder',
  'incident-commander': 'Incident Commander',
  admin: 'Admin',
}

const ORG_ROLE_RANK: Record<OrgRole, number> = {
  viewer: 1,
  responder: 2,
  'incident-commander': 3,
  admin: 4,
}

/** True if `role` meets or exceeds `min` in the RBAC hierarchy (internal/auth/rbac.go). */
export function hasRole(role: OrgRole | undefined, min: OrgRole): boolean {
  if (!role) return false
  return ORG_ROLE_RANK[role] >= ORG_ROLE_RANK[min]
}

export interface WhoAmIResponse {
  id: string
  email: string
  name: string
  avatar_url?: string
  org_role: OrgRole
  oauth_provider?: string
}

export interface PublicUser {
  id: string
  email: string
  name: string
  org_role: OrgRole
}

export interface AuthResponse {
  token: string
  user: PublicUser
}

export type SEVStatus =
  | 'open'
  | 'investigating'
  | 'mitigated'
  | 'resolved'
  | 'postmortem_in_progress'
  | 'postmortem_complete'

export const SEV_STATUS_LABELS: Record<SEVStatus, string> = {
  open: 'Open',
  investigating: 'Investigating',
  mitigated: 'Mitigated',
  resolved: 'Resolved',
  postmortem_in_progress: 'Postmortem In Progress',
  postmortem_complete: 'Postmortem Complete',
}

/** Badge color classes per status, roughly ordered by urgency (open is the
 * most attention-grabbing; postmortem_complete fades into the background
 * since that SEV is done and archived) — passed as Badge's `className`,
 * which wins over its default variant classes via cn()'s tailwind-merge. */
export const SEV_STATUS_BADGE_CLASS: Record<SEVStatus, string> = {
  open: 'border-transparent bg-amber-500 text-white dark:bg-amber-600',
  investigating: 'border-transparent bg-blue-500 text-white dark:bg-blue-600',
  mitigated: 'border-transparent bg-violet-500 text-white dark:bg-violet-600',
  resolved: 'border-transparent bg-emerald-600 text-white',
  postmortem_in_progress: 'border-transparent bg-slate-500 text-white dark:bg-slate-600',
  postmortem_complete: 'border-transparent bg-muted text-muted-foreground',
}

/** Non-terminal statuses — what the dashboard considers "active". */
export const ACTIVE_SEV_STATUSES: SEVStatus[] = [
  'open',
  'investigating',
  'mitigated',
  'resolved',
  'postmortem_in_progress',
]

/** The full lifecycle in order (docs/requirements.md §2.3): Open →
 * Investigating → Mitigated → Resolved → Postmortem In Progress → Postmortem
 * Complete. The state machine (internal/sev/statemachine.go) also allows
 * stepping backward/re-opening, so this is a display ordering, not a claim
 * that a SEV only ever moves forward through it. */
export const SEV_LIFECYCLE_STAGES: SEVStatus[] = [
  'open',
  'investigating',
  'mitigated',
  'resolved',
  'postmortem_in_progress',
  'postmortem_complete',
]

/** docs/requirements.md §4.2's root-cause-category examples ("e.g.,
 * deployment, configuration, hardware, dependency") — not an enforced
 * backend enum (root_cause_category stays free text server-side), just what
 * the dropdown offers before an "Other" free-text entry. */
export type RootCauseCategory = 'deployment' | 'configuration' | 'hardware' | 'dependency'

export const ROOT_CAUSE_CATEGORY_LABELS: Record<RootCauseCategory, string> = {
  deployment: 'Deployment',
  configuration: 'Configuration',
  hardware: 'Hardware',
  dependency: 'Dependency',
}

/** docs/requirements.md §4.2's fixed detection-method vocabulary — enforced
 * server-side (internal/api/grpc/sev.go's validateDetectionMethod). Unlike
 * MonitoringTool below, there's no "other" escape hatch: this list is closed. */
export type DetectionMethod =
  | 'alert'
  | 'monitoring-dashboard'
  | 'customer-report'
  | 'synthetic-test'
  | 'manual-discovery'
  | 'slack-escalation'

export const DETECTION_METHOD_LABELS: Record<DetectionMethod, string> = {
  alert: 'Alert',
  'monitoring-dashboard': 'Monitoring Dashboard',
  'customer-report': 'Customer Report',
  'synthetic-test': 'Synthetic Test',
  'manual-discovery': 'Manual Discovery',
  'slack-escalation': 'Slack Escalation',
}

/** docs/requirements.md §13.4's fixed monitoring-tool vocabulary — enforced
 * server-side (internal/api/grpc/sev.go's validateMonitoringTool), the same
 * closed-enum pattern as DetectionMethod above. "other" is itself a valid
 * value, not a free-text escape hatch — there's no companion custom-name
 * field. */
export type MonitoringTool = 'datadog' | 'prometheus' | 'cloudwatch' | 'other'

export const MONITORING_TOOL_LABELS: Record<MonitoringTool, string> = {
  datadog: 'Datadog',
  prometheus: 'Prometheus',
  cloudwatch: 'CloudWatch',
  other: 'Other',
}

export interface SEVResponse {
  id: string
  title: string
  description?: string
  severity_level: number
  status: SEVStatus
  root_cause_category?: string
  root_cause_description?: string
  mitigation?: string
  prevention?: string
  business_impact?: string
  affected_services?: string[]
  detection_method?: DetectionMethod
  alert_name?: string
  monitoring_tool?: MonitoringTool
  // alert_url, dashboard_url, query, and snapshot_url are optional
  // supporting detail — see internal/store.SEV's matching fields for the
  // full rationale.
  alert_url?: string
  dashboard_url?: string
  query?: string
  snapshot_url?: string
  // github_repo is the "owner/repo" this SEV's code lives in — see
  // internal/store.SEV.GitHubRepo. Set via UpdateSEV, not at creation.
  github_repo?: string
  // root_cause_reference_url links to the concrete change that caused the
  // incident (a PR/commit diff, a config-management change, etc.) — see
  // internal/store.SEV.RootCauseReferenceURL. Set via UpdateSEV, not at
  // creation.
  root_cause_reference_url?: string
  right_people_present?: boolean
  right_people_notes?: string
  tags?: Record<string, string>
  started_at?: string
  detected_at?: string
  mitigated_at?: string
  resolved_at?: string
  postmortem_completed_at?: string
  mttd_seconds?: string
  mttm_seconds?: string
  mttr_seconds?: string
  dttm_seconds?: string
  locked?: boolean
  sensitive?: boolean
  created_at: string
  updated_at: string
  created_by?: string
  ai_disabled?: boolean
}

export interface ListSEVsResponse {
  sevs: SEVResponse[]
  total: number
}

export interface ListSEVsParams {
  severity_levels?: number[]
  statuses?: string[]
  on_call_user?: string
  search?: string
  limit?: number
  offset?: number
}

export interface ActiveSEVCount {
  severity_level: number
  count: number
}

export interface MTTRTrend {
  window_days: number
  average_mttr_seconds?: string
  sample_size?: number
}

export interface ServiceLevelFrequency {
  service_id: string
  severity_level: number
  count: number
}

export interface DashboardMetricsResponse {
  active_by_level?: ActiveSEVCount[]
  mttr_trends?: MTTRTrend[]
  frequency_by_service_and_level?: ServiceLevelFrequency[]
  postmortem_completion_rate?: number
  overdue_task_count?: number
}

// --- CreateSEV / UpdateSEV / TransitionStatus request bodies -------------

export interface CreateSEVRequest {
  title: string
  description?: string
  severity_level: number
  started_at?: string
  detected_at?: string
  affected_services?: string[]
  detection_method?: DetectionMethod | ''
  alert_name?: string
  monitoring_tool?: MonitoringTool | ''
  alert_url?: string
  dashboard_url?: string
  query?: string
  snapshot_url?: string
  tags?: Record<string, string>
  sensitive?: boolean
  ai_disabled?: boolean
}

export interface UpdateSEVRequest {
  title?: string
  description?: string
  severity_level?: number
  root_cause_category?: string
  root_cause_description?: string
  mitigation?: string
  prevention?: string
  business_impact?: string
  affected_services?: string[]
  detection_method?: DetectionMethod | ''
  alert_name?: string
  monitoring_tool?: MonitoringTool | ''
  alert_url?: string
  dashboard_url?: string
  query?: string
  snapshot_url?: string
  github_repo?: string
  root_cause_reference_url?: string
  right_people_present?: boolean
  right_people_notes?: string
  tags?: Record<string, string>
  started_at?: string
  detected_at?: string
  sensitive?: boolean
  ai_disabled?: boolean
  /** Required when the SEV is locked (postmortem_complete) — see SEVResponse.locked. */
  unlock_token?: string
}

/** Mirrors internal/sev/statemachine.go's validTransitions map. The server
 * is the actual authority (this is UX sugar to only offer valid buttons);
 * an out-of-sync copy here would just mean a rejected request, not a
 * security or data-integrity issue. */
export const VALID_STATUS_TRANSITIONS: Record<SEVStatus, SEVStatus[]> = {
  open: ['investigating', 'mitigated'],
  investigating: ['mitigated', 'open'],
  mitigated: ['resolved', 'investigating'],
  resolved: ['postmortem_in_progress'],
  postmortem_in_progress: ['postmortem_complete'],
  postmortem_complete: ['open'],
}

export interface TransitionStatusRequest {
  to_status: SEVStatus
  mitigated_at?: string
  resolved_at?: string
  postmortem_completed_at?: string
  unlock_token?: string
}

// --- Search -----------------------------------------------------------

export type QuickView = 'open' | 'my_sevs' | 'awaiting_postmortem'

export const QUICK_VIEW_LABELS: Record<QuickView, string> = {
  open: 'Open',
  my_sevs: 'My SEVs',
  awaiting_postmortem: 'Awaiting Postmortem',
}

export type SEVSortField = 'started_at' | 'severity' | 'mttr' | 'updated_at'

export interface SearchSEVsParams {
  query?: string
  severity_levels?: number[]
  statuses?: SEVStatus[]
  service_ids?: string[]
  on_call_user?: string
  detected_by?: string
  root_cause_category?: string
  quick_view?: QuickView | ''
  sort?: SEVSortField
  sort_desc?: boolean
  limit?: number
  offset?: number
}

export interface SearchSEVsResponse {
  sevs?: SEVResponse[]
  total?: number
}

// --- Roles --------------------------------------------------------------

export type SEVRoleType =
  | 'on-call'
  | 'detected-by'
  | 'incident-commander'
  | 'comms-lead'
  | 'recorder'
  | 'responder'

export const SEV_ROLE_LABELS: Record<SEVRoleType, string> = {
  'on-call': 'On-call',
  'detected-by': 'Detected By',
  'incident-commander': 'Incident Commander',
  'comms-lead': 'Communications Lead',
  recorder: 'Recorder',
  responder: 'Responder',
}

export interface SEVRoleResponse {
  id: string
  sev_id: string
  role_type: SEVRoleType
  user_id?: string
  display_name?: string
  created_at: string
  created_by?: string
}

export interface AssignRoleRequest {
  role_type: SEVRoleType
  user_id?: string
  display_name: string
}

export interface ListRolesResponse {
  roles?: SEVRoleResponse[]
}

// --- Sensitive SEV access grants (§14) -------------------------------------

export interface SEVAccessResponse {
  id: string
  sev_id: string
  user_id: string
  created_at: string
  created_by?: string
}

export interface GrantAccessRequest {
  user_id: string
}

export interface ListAccessResponse {
  access?: SEVAccessResponse[]
}

// --- Announcements --------------------------------------------------------

export type AnnouncementAudience = 'internal' | 'external' | 'status-page'

export const AUDIENCE_LABELS: Record<AnnouncementAudience, string> = {
  internal: 'Internal',
  external: 'External',
  'status-page': 'Status Page',
}

export interface AnnouncementResponse {
  id: string
  sev_id: string
  author_id?: string
  message: string
  audience: AnnouncementAudience
  is_milestone?: boolean
  created_at: string
}

export interface CreateAnnouncementRequest {
  message: string
  audience: AnnouncementAudience
  is_milestone?: boolean
}

export interface ListAnnouncementsResponse {
  announcements?: AnnouncementResponse[]
}

// --- Chat log -------------------------------------------------------------

export interface ChatEntryResponse {
  id: string
  sev_id: string
  occurred_at: string
  source: string
  author: string
  content: string
  added_at: string
  added_by?: string
}

export interface AddChatEntryRequest {
  occurred_at: string
  source: string
  author: string
  content: string
}

export interface ListChatEntriesResponse {
  entries?: ChatEntryResponse[]
}

// --- Linked tasks -----------------------------------------------------

export type TaskRelationshipType = 'action-item' | 'contributing-factor' | 'follow-up' | 'dependency'

export const TASK_RELATIONSHIP_LABELS: Record<TaskRelationshipType, string> = {
  'action-item': 'Action Item',
  'contributing-factor': 'Contributing Factor',
  'follow-up': 'Follow-up',
  dependency: 'Dependency',
}

export type TaskPriority = 'critical' | 'non-critical'

/** The external_system values TasksPanel/badges.tsx know how to label and
 * badge distinctly. Not an enforced backend enum — external_system stays
 * unvalidated free text server-side (LinkTask accepts anything), so
 * TaskResponse.external_system below widens this with `(string & {})` rather
 * than closing it: any value round-trips, only these three render specially. */
export type KnownExternalSystem = 'github' | 'jira' | 'generic'

export const EXTERNAL_SYSTEM_LABELS: Record<KnownExternalSystem, string> = {
  github: 'GitHub',
  jira: 'Jira',
  generic: 'Link',
}

/** Badge color classes per tracker, same `className`-wins-via-cn() pattern
 * as SEV_STATUS_BADGE_CLASS above. */
export const EXTERNAL_SYSTEM_BADGE_CLASS: Record<KnownExternalSystem, string> = {
  github: 'border-transparent bg-slate-700 text-white dark:bg-slate-600',
  jira: 'border-transparent bg-blue-600 text-white dark:bg-blue-500',
  generic: 'border-border text-foreground',
}

export interface TaskResponse {
  id: string
  sev_id: string
  external_system: KnownExternalSystem | (string & {})
  task_id: string
  url: string
  title: string
  description?: string
  relationship_type: TaskRelationshipType
  priority: TaskPriority
  due_date?: string
  overdue?: boolean
  created_at: string
  created_by?: string
}

export interface LinkTaskRequest {
  external_system: string
  task_id: string
  url: string
  title: string
  description?: string
  relationship_type: TaskRelationshipType
  priority: TaskPriority
  due_date?: string
}

export interface CreateGitHubIssueRequest {
  owner: string
  repo: string
  title: string
  body?: string
  relationship_type: TaskRelationshipType
  priority: TaskPriority
}

export interface CreateJiraIssueRequest {
  /** project_key is the Jira project's key (e.g. "OPS"), not its numeric ID. */
  project_key: string
  /** issue_type is the target project's issue type name (e.g. "Task", "Bug")
   * — it must already exist on that project. */
  issue_type: string
  /** summary is Jira's naming for the field GitHub calls "title". */
  summary: string
  description?: string
  relationship_type: TaskRelationshipType
  priority: TaskPriority
}

export interface ListTasksResponse {
  tasks?: TaskResponse[]
}

// --- Linked SEVs --------------------------------------------------------

export type SEVRelationshipType = 'related' | 'caused-by' | 'duplicate' | 'recurrence-of'

export const SEV_RELATIONSHIP_LABELS: Record<SEVRelationshipType, string> = {
  related: 'Related',
  'caused-by': 'Caused By',
  duplicate: 'Duplicate',
  'recurrence-of': 'Recurrence Of',
}

export interface SEVLinkResponse {
  id: string
  source_sev_id: string
  target_sev_id: string
  relationship_type: SEVRelationshipType
  created_at: string
  created_by?: string
}

export interface LinkSEVsRequest {
  target_sev_id: string
  relationship_type: SEVRelationshipType
}

export interface ListLinkedSEVsResponse {
  links?: SEVLinkResponse[]
}

// --- Services (Viewer+ read for the affected-services picker; full CRUD is
// Admin-only, see "Admin configuration" below) --------------------------

export interface ServiceResponse {
  id: string
  name: string
  description?: string
  owning_team?: string
  pagerduty_service_id?: string
  tags?: Record<string, string>
  active?: boolean
  created_at: string
  updated_at: string
}

export interface ListServicesResponse {
  services?: ServiceResponse[]
}

// --- WebSocket event envelope (internal/api/ws/hub.go's Event) ----------

export interface WSEvent {
  type: string
  sev_id: string
  payload: unknown
}

// --- Postmortem ----------------------------------------------------------

export type PostmortemStatus = 'draft' | 'in-review' | 'approved'

export const POSTMORTEM_STATUS_LABELS: Record<PostmortemStatus, string> = {
  draft: 'Draft',
  'in-review': 'In Review',
  approved: 'Approved',
}

export const POSTMORTEM_STATUS_BADGE_CLASS: Record<PostmortemStatus, string> = {
  draft: 'border-transparent bg-slate-500 text-white dark:bg-slate-600',
  'in-review': 'border-transparent bg-blue-500 text-white dark:bg-blue-600',
  approved: 'border-transparent bg-emerald-600 text-white',
}

/** Mirrors internal/postmortem/statemachine.go's validTransitions — UX sugar
 * only, same caveat as SEVResponse's VALID_STATUS_TRANSITIONS: the server is
 * the actual authority. */
export const VALID_POSTMORTEM_TRANSITIONS: Record<PostmortemStatus, PostmortemStatus[]> = {
  draft: ['in-review'],
  'in-review': ['approved', 'draft'],
  approved: [],
}

export interface PostmortemResponse {
  id: string
  sev_id: string
  status: PostmortemStatus
  content?: string
  created_at: string
  updated_at: string
  updated_by?: string
}

export interface UpdatePostmortemRequest {
  content: string
  /** Required when the SEV is locked — see SEVResponse.locked. */
  unlock_token?: string
}

export interface TransitionPostmortemStatusRequest {
  to_status: PostmortemStatus
}

export interface UnlockSEVRequest {
  reason: string
}

export interface UnlockSEVResponse {
  unlock_token: string
}

// --- AI plugin system (§11) -----------------------------------------------

/** Matches proto/sevitout/v1/ai.proto's AIAction enum member names exactly —
 * protojson encodes/decodes proto enums by name, not number, by default. */
export type AIAction =
  | 'AI_ACTION_SUMMARIZE'
  | 'AI_ACTION_SUGGEST_ROOT_CAUSE'
  | 'AI_ACTION_DRAFT_POSTMORTEM'
  | 'AI_ACTION_SUGGEST_TASKS'
  | 'AI_ACTION_FIND_SIMILAR'
  | 'AI_ACTION_SUGGEST_RESPONDERS'
  | 'AI_ACTION_DRAFT_ANNOUNCEMENT'

export interface AIOutputResponse {
  id: string
  sev_id: string
  plugin_id?: string
  trigger_event?: string
  action: AIAction
  /** Plain text for narrative actions (Summarize, DraftPostmortem,
   * DraftAnnouncement); JSON text for structured/list-shaped actions. Always
   * label this as AI-generated wherever it's rendered (§11.3). */
  content?: string
  created_at: string
}

export interface TriggerActionRequest {
  action: AIAction
  plugin_id?: string
}

export interface ListAIOutputsResponse {
  outputs?: AIOutputResponse[]
}

export interface AvailablePlugin {
  id: string
  name: string
  provider: string
}

export interface ListPluginsResponse {
  plugins?: AvailablePlugin[]
}

// --- Admin configuration (ConfigService, §18) -----------------------------
// Every RPC here is Admin-only except ListServices/ListOnCallRotations
// (Viewer+, used elsewhere in the app for pickers/on-call display) — see
// internal/auth/rbac.go's ConfigService entries.

export interface CreateServiceRequest {
  // id is a caller-supplied slug (e.g. "checkout"); it is the stable
  // identifier referenced elsewhere (affected services, SLIs, on-call) and
  // cannot be changed after creation.
  id: string
  name: string
  description?: string
  owning_team?: string
  pagerduty_service_id?: string
  tags?: Record<string, string>
}

export interface UpdateServiceRequest {
  name?: string
  description?: string
  owning_team?: string
  pagerduty_service_id?: string
  tags?: Record<string, string>
  active?: boolean
}

export interface UserResponse {
  id: string
  email: string
  name: string
  avatar_url?: string
  org_role: OrgRole
  active?: boolean
  created_at: string
  updated_at: string
}

export interface ListUsersResponse {
  users?: UserResponse[]
}

export interface UpdateUserRoleRequest {
  org_role: OrgRole
}

export interface OnCallRotationResponse {
  id: string
  name: string
  service_id?: string
  pagerduty_schedule_id?: string
  // manual_user_id/manual_display_name plus an override_start/override_end
  // window define a manual override for a planned change; a normal
  // PagerDuty-backed rotation has none of the four set.
  manual_user_id?: string
  manual_display_name?: string
  override_start?: string
  override_end?: string
  created_at: string
  updated_at: string
}

export interface ListOnCallRotationsResponse {
  rotations?: OnCallRotationResponse[]
}

export interface CreateOnCallRotationRequest {
  name: string
  service_id?: string
  pagerduty_schedule_id?: string
  manual_user_id?: string
  manual_display_name?: string
  override_start?: string
  override_end?: string
}

export interface UpdateOnCallRotationRequest {
  name?: string
  service_id?: string
  pagerduty_schedule_id?: string
  manual_user_id?: string
  manual_display_name?: string
  override_start?: string
  override_end?: string
}

export interface IntegrationConfigResponse {
  integration_type: string
  // credentials_configured is true when a non-empty credentials blob has
  // been stored; the decrypted value itself is never returned by any RPC.
  credentials_configured?: boolean
  settings?: Record<string, string>
  created_at: string
  updated_at: string
}

export interface ListIntegrationConfigsResponse {
  configs?: IntegrationConfigResponse[]
}

export interface UpsertIntegrationConfigRequest {
  integration_type: string
  // credentials is write-only. Omit (leave undefined) to keep the existing
  // stored credentials unchanged while updating settings.
  credentials?: Record<string, string>
  settings?: Record<string, string>
}

export type IntegrationHealthStatus = 'connected' | 'error' | 'not_configured' | 'unknown'

/** One entry from GET /admin/integrations/health — a plain HTTP handler, not
 * part of ConfigService's proto/gRPC-gateway surface (see
 * internal/api/grpc/integrations_health.go), so its JSON shape is hand-rolled
 * rather than protojson-generated: fields are still snake_case, but there's
 * no int64-as-string quirk to account for since it has no int64 fields. */
export interface IntegrationHealth {
  integration_type: string
  status: IntegrationHealthStatus
  error?: string
}

export interface IntegrationsHealthResponse {
  integrations?: IntegrationHealth[]
}

export interface RetentionConfigResponse {
  severity_level: number
  // retention_days == 0 (the proto3 zero value) means retain forever — and,
  // per this file's header comment, protojson omits it from the wire
  // entirely rather than sending an explicit 0, confirmed live: a severity
  // level that has never been explicitly configured returns a config row
  // (from ListRetentionConfig's defaults) with no retention_days key at all.
  retention_days?: number
  // hard_delete controls what happens on expiry: false archives
  // (soft-delete), true purges permanently.
  hard_delete?: boolean
  created_at: string
  updated_at: string
}

export interface ListRetentionConfigResponse {
  configs?: RetentionConfigResponse[]
}

export interface UpdateRetentionConfigRequest {
  severity_level: number
  retention_days: number
  hard_delete: boolean
}

export type AIHandlerType = 'builtin' | 'http'

export interface AIPluginResponse {
  id: string
  name: string
  version?: string
  description?: string
  handler_type: AIHandlerType
  http_endpoint?: string
  provider?: string
  model?: string
  // api_key_configured is true when an encrypted API key has been stored;
  // the decrypted value itself is never returned by any RPC.
  api_key_configured?: boolean
  enabled?: boolean
  trigger_on_open?: boolean
  trigger_on_mitigated?: boolean
  trigger_on_resolved?: boolean
  trigger_on_postmortem_review?: boolean
  // rate_limit_per_minute caps AI actions per minute for this plugin; 0 (or
  // absent, since protojson omits the zero value) means unlimited.
  rate_limit_per_minute?: number
  created_at: string
  updated_at: string
}

export interface ListAIPluginsResponse {
  plugins?: AIPluginResponse[]
}

export interface CreateAIPluginRequest {
  name: string
  version?: string
  description?: string
  handler_type: AIHandlerType
  http_endpoint?: string
  provider?: string
  model?: string
  // api_key is write-only: encrypted before storage, never returned.
  api_key?: string
  enabled?: boolean
  trigger_on_open?: boolean
  trigger_on_mitigated?: boolean
  trigger_on_resolved?: boolean
  trigger_on_postmortem_review?: boolean
  rate_limit_per_minute?: number
}

export interface UpdateAIPluginRequest {
  name?: string
  version?: string
  description?: string
  handler_type?: AIHandlerType
  http_endpoint?: string
  provider?: string
  model?: string
  // api_key, when non-empty, replaces the stored key; omit to keep the
  // existing one unchanged.
  api_key?: string
  // These four booleans and rate_limit_per_minute are backed by
  // google.protobuf.*Value wrappers server-side specifically so an explicit
  // false/0 is distinguishable from "not supplied" — always send the whole
  // current form state on every save (not just changed fields) so an
  // intentional false/0 is never silently dropped as falsy.
  enabled?: boolean
  trigger_on_open?: boolean
  trigger_on_mitigated?: boolean
  trigger_on_resolved?: boolean
  trigger_on_postmortem_review?: boolean
  rate_limit_per_minute?: number
}

// --- Reporting & trends (ReportService, §17) ------------------------------

export interface RecurringPattern {
  service_id: string
  root_cause_category: string
  count: number
  // sev_ids is sorted most-recently-created first.
  sev_ids?: string[]
}

export interface SEVTrendsResponse {
  recurring_patterns?: RecurringPattern[]
}

export interface ExportSEVsParams {
  severity_levels?: number[]
  statuses?: string[]
  service_ids?: string[]
  root_cause_category?: string
  started_after?: string
  started_before?: string
}

// --- Public shareable links (ShareService, §14.1) -------------------------

export interface ShareLinkResponse {
  id: string
  sev_id: string
  token: string
  // path is the public view's URL path on this server, e.g. "/s/<token>" —
  // the backend computes it so the frontend never has to know the route
  // shape itself.
  path: string
  expires_at?: string
  revoked?: boolean
  created_by?: string
  created_at: string
}

export interface CreateShareLinkRequest {
  // expires_in_days: how many days from now the link stays valid. Defaults
  // to 30 server-side when unset or <= 0.
  expires_in_days?: number
}

// --- Public share view (GET /s/{token} — a plain net/http handler, not
// gRPC/grpc-gateway, so this shape is hand-rolled Go JSON via
// encoding/json, not protojson: no int64-as-string quirk (severity_level is
// a plain number) and no zero-value omission beyond normal `omitempty` tags
// — see internal/api/grpc/share_view.go's sharedSEVResponse.) -------------

export interface SharedAnnouncement {
  message: string
  created_at: string
}

/** The curated, public-safe view of a SEV — only what
 * docs/requirements.md §14.1 lists (title, severity, status, lifecycle
 * timestamps, `external`-audience announcements, business impact); nothing
 * else from the SEV record is present, not just hidden by a zero value. */
export interface SharedSEVResponse {
  id: string
  title: string
  severity_level: number
  status: SEVStatus
  started_at?: string
  detected_at?: string
  mitigated_at?: string
  resolved_at?: string
  postmortem_completed_at?: string
  business_impact?: string
  announcements: SharedAnnouncement[]
}
