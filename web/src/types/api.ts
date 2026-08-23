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
 * backend enum (root_cause_category stays free text server-side, same as
 * MonitoringTool), just what the dropdown offers before an "Other" free-text
 * entry. */
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

/** The monitoring tools named throughout docs/requirements.md §13.4 — not an
 * exhaustive backend enum (monitoring_tool stays free text server-side), just
 * what the dropdown offers directly before falling back to a free-text
 * "Other" entry. */
export type MonitoringTool = 'datadog' | 'prometheus' | 'cloudwatch'

export const MONITORING_TOOL_LABELS: Record<MonitoringTool, string> = {
  datadog: 'Datadog',
  prometheus: 'Prometheus',
  cloudwatch: 'CloudWatch',
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
  monitoring_tool?: string
  // alert_url, metric_link, and snapshot_url are optional supporting links —
  // see internal/store.SEV's matching fields for the full rationale.
  alert_url?: string
  metric_link?: string
  snapshot_url?: string
  // github_repo is the "owner/repo" this SEV's code lives in — see
  // internal/store.SEV.GitHubRepo. Set via UpdateSEV, not at creation.
  github_repo?: string
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
  monitoring_tool?: string
  alert_url?: string
  metric_link?: string
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
  monitoring_tool?: string
  alert_url?: string
  metric_link?: string
  snapshot_url?: string
  github_repo?: string
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

export interface TaskResponse {
  id: string
  sev_id: string
  external_system: string
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

// --- Services (read-only picker use in this milestone; full CRUD is M14d) -

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
