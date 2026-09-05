import { Fragment, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Send, Trash2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { formatDateTime } from '@/lib/format'
import type {
  EscalationConfigResponse,
  NotificationConfigResponse,
  TestNotificationConfigRequest,
  TestNotificationResult,
} from '@/types/api'

/** Every event this codebase actually dispatches a notification for
 * (internal/api/grpc/sev.go, announcement.go, postmortem.go, and
 * cmd/server's escalation and SLA-risk scanners) — see
 * internal/api/grpc/config_notification.go's notificationEvents, which the
 * server validates against independently. */
const NOTIFICATION_EVENTS = [
  { value: 'sev.created', label: 'SEV opened' },
  { value: 'sev.updated', label: 'SEV updated' },
  { value: 'sev.status_changed', label: 'SEV status changed' },
  { value: 'announcement.created', label: 'Announcement posted' },
  { value: 'postmortem.due', label: 'Postmortem due (SEV resolved)' },
  { value: 'postmortem.approved', label: 'Postmortem approved' },
  { value: 'sev.escalation_no_ic', label: 'Escalation: no Incident Commander' },
  { value: 'sev.sla_at_risk', label: 'SLA at risk' },
  { value: 'sev.sla_breached', label: 'SLA breached' },
]

const ROLES = [
  { value: 'viewer', label: 'Viewer' },
  { value: 'responder', label: 'Responder' },
  { value: 'incident-commander', label: 'Incident Commander' },
  { value: 'admin', label: 'Admin' },
]

const CHANNEL_TYPES = [
  { value: 'slack', label: 'Slack' },
  { value: 'email', label: 'Email' },
]

const SEVERITY_LEVELS = [1, 2, 3, 4]

interface RuleForm {
  role: string
  events: string[]
  channelType: string
  channelTarget: string
  maxSeverityLevel: string // '' = every severity
}

function emptyRuleForm(): RuleForm {
  return {
    role: 'incident-commander',
    events: ['sev.created'],
    channelType: 'slack',
    channelTarget: '',
    maxSeverityLevel: '',
  }
}

function eventLabel(event: string): string {
  return NOTIFICATION_EVENTS.find((e) => e.value === event)?.label ?? event
}

function roleLabel(role: string): string {
  return ROLES.find((r) => r.value === role)?.label ?? role
}

/** Per-key (a rule's id, or 'draft' for the not-yet-saved Add-rule form)
 * outcome of the last "Send test" click — results on success (one per
 * event tested), error when the request itself failed (e.g. validation,
 * permissions) before any delivery was attempted. */
interface TestOutcome {
  results?: TestNotificationResult[]
  error?: string
}

function TestResultsList({ outcome }: { outcome?: TestOutcome }) {
  if (!outcome) return null
  if (outcome.error) {
    return (
      <p role="alert" className="mt-1 text-xs text-destructive">
        {outcome.error}
      </p>
    )
  }
  return (
    <ul className="mt-1 flex flex-col gap-0.5 text-xs">
      {(outcome.results ?? []).map((r) => (
        <li key={r.event} className={r.success ? 'text-muted-foreground' : 'text-destructive'}>
          {eventLabel(r.event)}: {r.success ? 'sent' : r.error || 'failed'}
        </li>
      ))}
    </ul>
  )
}

/** Admin routing-rules + escalation-threshold editor (Roadmap Phase 15):
 * §16/§18.5's "configurable notification channels/triggers/role-based
 * routing" and "escalate a SEV-1 open too long with no IC." Each routing
 * rule is a fixed broadcast route to one channel_target for one role, that
 * can cover several events at once — not per-user personalized delivery,
 * and not a separate rule required per event. Rules are id-identified. */
export function AdminNotificationsPage() {
  return (
    <div className="flex flex-col gap-4">
      <NotificationRulesSection />
      <EscalationThresholdsSection />
    </div>
  )
}

function NotificationRulesSection() {
  const queryClient = useQueryClient()
  const rules = useQuery({ queryKey: ['admin', 'notifications'], queryFn: api.config.notifications.list })

  const [adding, setAdding] = useState(false)
  const [form, setForm] = useState<RuleForm>(emptyRuleForm())
  const [addError, setAddError] = useState<string | null>(null)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'notifications'] })

  const addMutation = useMutation({
    mutationFn: () =>
      api.config.notifications.create({
        role: form.role,
        events: form.events,
        channel_type: form.channelType,
        channel_target: form.channelTarget,
        max_severity_level: form.maxSeverityLevel ? Number(form.maxSeverityLevel) : undefined,
      }),
    onSuccess: () => {
      invalidate()
      setAdding(false)
      setForm(emptyRuleForm())
      setAddError(null)
      setTestOutcomes((o) => {
        const { draft: _draft, ...rest } = o
        return rest
      })
    },
    onError: (err) => setAddError(err instanceof ApiError ? err.message : 'Failed to add rule'),
  })

  const deleteMutation = useMutation({
    mutationFn: (rule: NotificationConfigResponse) => api.config.notifications.delete(rule.id),
    onSuccess: invalidate,
  })

  // Keyed by rule id, or 'draft' for the Add-rule form — lets an admin test
  // a saved rule or one they haven't saved yet, without waiting for a real
  // SEV event to see whether the channel/integration actually works.
  const [testOutcomes, setTestOutcomes] = useState<Record<string, TestOutcome>>({})
  const testMutation = useMutation({
    mutationFn: ({ req }: { key: string; req: TestNotificationConfigRequest }) => api.config.notifications.test(req),
    onSuccess: (resp, { key }) => setTestOutcomes((o) => ({ ...o, [key]: { results: resp.results } })),
    onError: (err, { key }) =>
      setTestOutcomes((o) => ({ ...o, [key]: { error: err instanceof ApiError ? err.message : 'Failed to send test' } })),
  })

  function sendTest(key: string, rule: TestNotificationConfigRequest) {
    testMutation.mutate({ key, req: rule })
  }

  function toggleEvent(event: string) {
    setForm((f) => ({
      ...f,
      events: f.events.includes(event) ? f.events.filter((e) => e !== event) : [...f.events, event],
    }))
  }

  const list = rules.data?.configs ?? []

  return (
    <Section
      title="Notification routing"
      action={
        !adding && (
          <Button size="sm" onClick={() => setAdding(true)}>
            <Plus className="h-3.5 w-3.5" /> Add rule
          </Button>
        )
      }
    >
      <p className="text-sm text-muted-foreground">
        Each rule posts to one Slack channel or email address whenever the selected event fires, for the audience
        implied by the chosen role. This is a fixed broadcast route, not a personal notification — it does not target
        a specific person's own Slack/email identity.
      </p>

      {adding && (
        <div className="flex flex-col gap-3 rounded-md border border-border p-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Field label="Role" htmlFor="rule-new-role">
              <Select id="rule-new-role" value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })}>
                {ROLES.map((r) => (
                  <option key={r.value} value={r.value}>
                    {r.label}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Channel type" htmlFor="rule-new-channel-type">
              <Select
                id="rule-new-channel-type"
                value={form.channelType}
                onChange={(e) => setForm({ ...form, channelType: e.target.value })}
              >
                {CHANNEL_TYPES.map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Max severity (optional)" htmlFor="rule-new-max-severity">
              <Select
                id="rule-new-max-severity"
                value={form.maxSeverityLevel}
                onChange={(e) => setForm({ ...form, maxSeverityLevel: e.target.value })}
              >
                <option value="">Every severity</option>
                {SEVERITY_LEVELS.map((level) => (
                  <option key={level} value={level}>
                    SEV-{level} or more critical
                  </option>
                ))}
              </Select>
            </Field>
          </div>
          <fieldset className="flex flex-col gap-1.5">
            <legend className="text-sm font-medium">Events</legend>
            <p className="text-xs text-muted-foreground">
              Select every event this rule should fire for — one rule can cover several.
            </p>
            <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
              {NOTIFICATION_EVENTS.map((e) => (
                <label key={e.value} className="flex items-center gap-2 text-sm">
                  <Checkbox checked={form.events.includes(e.value)} onChange={() => toggleEvent(e.value)} />
                  {e.label}
                </label>
              ))}
            </div>
          </fieldset>
          <Field label="Channel target" htmlFor="rule-new-target">
            <Input
              id="rule-new-target"
              placeholder={form.channelType === 'email' ? 'oncall@example.com' : '#incidents'}
              value={form.channelTarget}
              onChange={(e) => setForm({ ...form, channelTarget: e.target.value })}
            />
          </Field>
          {addError && (
            <p role="alert" className="text-sm text-destructive">
              {addError}
            </p>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              onClick={() => addMutation.mutate()}
              disabled={addMutation.isPending || !form.channelTarget || form.events.length === 0}
            >
              {addMutation.isPending ? 'Adding…' : 'Add rule'}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                sendTest('draft', {
                  role: form.role,
                  events: form.events,
                  channel_type: form.channelType,
                  channel_target: form.channelTarget,
                  max_severity_level: form.maxSeverityLevel ? Number(form.maxSeverityLevel) : undefined,
                })
              }
              disabled={
                (testMutation.isPending && testMutation.variables?.key === 'draft') ||
                !form.channelTarget ||
                form.events.length === 0
              }
              title="Send a real test message for every selected event, without saving this rule"
            >
              <Send className="h-3.5 w-3.5" />
              {testMutation.isPending && testMutation.variables?.key === 'draft' ? 'Sending…' : 'Send test'}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                setAdding(false)
                setTestOutcomes((o) => {
                  const { draft: _draft, ...rest } = o
                  return rest
                })
              }}
            >
              Cancel
            </Button>
          </div>
          <TestResultsList outcome={testOutcomes.draft} />
        </div>
      )}

      {rules.isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : list.length === 0 ? (
        <p className="text-sm text-muted-foreground">No notification rules configured yet.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3">Role</th>
                <th className="py-2 pr-3">Events</th>
                <th className="py-2 pr-3">Channel</th>
                <th className="py-2 pr-3">Target</th>
                <th className="py-2 pr-3">Max severity</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {list.map((rule) => (
                <Fragment key={rule.id}>
                  <tr className="border-b border-border align-top last:border-0">
                    <td className="py-2 pr-3">{roleLabel(rule.role)}</td>
                    <td className="py-2 pr-3">
                      <div className="flex flex-wrap gap-1">
                        {rule.events.map((event) => (
                          <span
                            key={event}
                            className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground"
                          >
                            {eventLabel(event)}
                          </span>
                        ))}
                      </div>
                    </td>
                    <td className="py-2 pr-3 capitalize">{rule.channel_type}</td>
                    <td className="py-2 pr-3 text-muted-foreground">{rule.channel_target}</td>
                    <td className="py-2 pr-3 text-muted-foreground">
                      {rule.max_severity_level ? `SEV-${rule.max_severity_level}+` : 'Every severity'}
                    </td>
                    <td className="py-2 text-right">
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Send test notifications for ${roleLabel(rule.role)} on ${rule.events.map(eventLabel).join(', ')}`}
                        title="Send a real test message for every event this rule covers"
                        disabled={testMutation.isPending && testMutation.variables?.key === rule.id}
                        onClick={() =>
                          sendTest(rule.id, {
                            role: rule.role,
                            events: rule.events,
                            channel_type: rule.channel_type,
                            channel_target: rule.channel_target,
                            max_severity_level: rule.max_severity_level,
                          })
                        }
                      >
                        <Send className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Delete rule for ${roleLabel(rule.role)} on ${rule.events.map(eventLabel).join(', ')}`}
                        onClick={() => deleteMutation.mutate(rule)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </td>
                  </tr>
                  {testOutcomes[rule.id] && (
                    <tr className="border-b border-border last:border-0">
                      <td colSpan={6} className="pb-2 pl-0">
                        <TestResultsList outcome={testOutcomes[rule.id]} />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  )
}

interface ThresholdForm {
  thresholdMinutes: string
  enabled: boolean
}

function toThresholdForm(cfg?: EscalationConfigResponse): ThresholdForm {
  return {
    thresholdMinutes: String(cfg?.threshold_minutes ?? 0),
    enabled: cfg?.enabled ?? false,
  }
}

function EscalationThresholdsSection() {
  const queryClient = useQueryClient()
  const escalation = useQuery({ queryKey: ['admin', 'escalation'], queryFn: api.config.escalation.list })

  const [forms, setForms] = useState<Record<number, ThresholdForm>>({})
  const [errors, setErrors] = useState<Record<number, string>>({})

  const byLevel = new Map((escalation.data?.configs ?? []).map((c) => [c.severity_level, c]))

  const updateMutation = useMutation({
    mutationFn: ({ level, form }: { level: number; form: ThresholdForm }) =>
      api.config.escalation.upsert(level, {
        severity_level: level,
        threshold_minutes: Number(form.thresholdMinutes) || 0,
        enabled: form.enabled,
      }),
    onSuccess: (_data, { level }) => {
      void queryClient.invalidateQueries({ queryKey: ['admin', 'escalation'] })
      setErrors((e) => ({ ...e, [level]: '' }))
    },
    onError: (err, { level }) =>
      setErrors((e) => ({ ...e, [level]: err instanceof ApiError ? err.message : 'Failed to save' })),
  })

  function formFor(level: number): ThresholdForm {
    return forms[level] ?? toThresholdForm(byLevel.get(level))
  }

  function setFormFor(level: number, form: ThresholdForm) {
    setForms((f) => ({ ...f, [level]: form }))
  }

  if (escalation.isLoading) {
    return (
      <Section title="Escalation">
        <Skeleton className="h-32 w-full" />
      </Section>
    )
  }

  return (
    <Section title="Escalation">
      <p className="text-sm text-muted-foreground">
        Per-severity-level threshold: if a SEV at this level has been open longer than this many minutes with no
        Incident Commander assigned, an escalation notification fires once (routed the same way as any other event
        above, via a rule for the "Escalation: no Incident Commander" event).
      </p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="py-2 pr-3">Severity</th>
              <th className="py-2 pr-3">Threshold (minutes)</th>
              <th className="py-2 pr-3">Enabled</th>
              <th className="py-2 pr-3">Last updated</th>
              <th className="py-2" />
            </tr>
          </thead>
          <tbody>
            {SEVERITY_LEVELS.map((level) => {
              const cfg = byLevel.get(level)
              const form = formFor(level)
              return (
                <tr key={level} className="border-b border-border align-top last:border-0">
                  <td className="py-2 pr-3 font-medium">SEV-{level}</td>
                  <td className="py-2 pr-3">
                    <Input
                      type="number"
                      min={0}
                      aria-label={`Escalation threshold minutes for SEV-${level}`}
                      value={form.thresholdMinutes}
                      onChange={(e) => setFormFor(level, { ...form, thresholdMinutes: e.target.value })}
                      className="w-28"
                    />
                  </td>
                  <td className="py-2 pr-3">
                    <label className="flex items-center gap-2">
                      <Checkbox
                        aria-label={`Enable escalation for SEV-${level}`}
                        checked={form.enabled}
                        onChange={(e) => setFormFor(level, { ...form, enabled: e.target.checked })}
                      />
                      Enabled
                    </label>
                  </td>
                  <td className="py-2 pr-3 text-muted-foreground">{cfg ? formatDateTime(cfg.updated_at) : 'Not set'}</td>
                  <td className="py-2 text-right">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => updateMutation.mutate({ level, form })}
                      disabled={updateMutation.isPending && updateMutation.variables?.level === level}
                    >
                      Save
                    </Button>
                    {errors[level] && (
                      <p role="alert" className="mt-1 text-xs text-destructive">
                        {errors[level]}
                      </p>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </Section>
  )
}

function Field({ label, htmlFor, children }: { label: string; htmlFor?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
    </div>
  )
}
