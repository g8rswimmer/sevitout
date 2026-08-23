// Wire types for the REST endpoints this app calls, mirroring the proto
// messages under proto/sevitout/v1/*.proto. The gateway marshals with
// protojson + UseProtoNames, so JSON keys are snake_case (matching the .proto
// field names verbatim) rather than camelCase. Per the protojson spec, int64
// fields (the *_seconds duration fields below) are encoded as JSON strings,
// not numbers — every such field is typed `string` here for that reason.
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
  detection_method?: string
  alert_name?: string
  monitoring_tool?: string
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
