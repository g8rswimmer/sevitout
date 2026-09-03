import { useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { formatDateTime } from '@/lib/format'
import type { EscalationConfigResponse, NotificationConfigResponse } from '@/types/api'

/** Every event this codebase actually dispatches a notification for
 * (internal/api/grpc/sev.go, announcement.go, postmortem.go, and
 * cmd/server's escalation scanner) — see internal/api/grpc/config_notification.go's
 * notificationEvents, which the server validates against independently. */
const NOTIFICATION_EVENTS = [
  { value: 'sev.created', label: 'SEV opened' },
  { value: 'sev.updated', label: 'SEV updated' },
  { value: 'sev.status_changed', label: 'SEV status changed' },
  { value: 'announcement.created', label: 'Announcement posted' },
  { value: 'postmortem.due', label: 'Postmortem due (SEV resolved)' },
  { value: 'postmortem.approved', label: 'Postmortem approved' },
  { value: 'sev.escalation_no_ic', label: 'Escalation: no Incident Commander' },
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
  event: string
  channelType: string
  channelTarget: string
  maxSeverityLevel: string // '' = every severity
}

function emptyRuleForm(): RuleForm {
  return { role: 'incident-commander', event: 'sev.created', channelType: 'slack', channelTarget: '', maxSeverityLevel: '' }
}

function eventLabel(event: string): string {
  return NOTIFICATION_EVENTS.find((e) => e.value === event)?.label ?? event
}

function roleLabel(role: string): string {
  return ROLES.find((r) => r.value === role)?.label ?? role
}

/** Admin routing-rules + escalation-threshold editor (Roadmap Phase 15):
 * §16/§18.5's "configurable notification channels/triggers/role-based
 * routing" and "escalate a SEV-1 open too long with no IC." Each routing
 * rule is a fixed broadcast route — role/event/channel_type identify it,
 * channel_target is where it posts — not per-user personalized delivery. */
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
      api.config.notifications.upsert({
        role: form.role,
        event: form.event,
        channel_type: form.channelType,
        channel_target: form.channelTarget,
        max_severity_level: form.maxSeverityLevel ? Number(form.maxSeverityLevel) : undefined,
      }),
    onSuccess: () => {
      invalidate()
      setAdding(false)
      setForm(emptyRuleForm())
      setAddError(null)
    },
    onError: (err) => setAddError(err instanceof ApiError ? err.message : 'Failed to add rule'),
  })

  const deleteMutation = useMutation({
    mutationFn: (rule: NotificationConfigResponse) =>
      api.config.notifications.delete(rule.role, rule.event, rule.channel_type),
    onSuccess: invalidate,
  })

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
            <Field label="Event" htmlFor="rule-new-event">
              <Select id="rule-new-event" value={form.event} onChange={(e) => setForm({ ...form, event: e.target.value })}>
                {NOTIFICATION_EVENTS.map((e) => (
                  <option key={e.value} value={e.value}>
                    {e.label}
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
          <div className="flex gap-2">
            <Button size="sm" onClick={() => addMutation.mutate()} disabled={addMutation.isPending || !form.channelTarget}>
              {addMutation.isPending ? 'Adding…' : 'Add rule'}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setAdding(false)}>
              Cancel
            </Button>
          </div>
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
                <th className="py-2 pr-3">Event</th>
                <th className="py-2 pr-3">Channel</th>
                <th className="py-2 pr-3">Target</th>
                <th className="py-2 pr-3">Max severity</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {list.map((rule) => (
                <tr key={`${rule.role}-${rule.event}-${rule.channel_type}`} className="border-b border-border last:border-0">
                  <td className="py-2 pr-3">{roleLabel(rule.role)}</td>
                  <td className="py-2 pr-3">{eventLabel(rule.event)}</td>
                  <td className="py-2 pr-3 capitalize">{rule.channel_type}</td>
                  <td className="py-2 pr-3 text-muted-foreground">{rule.channel_target}</td>
                  <td className="py-2 pr-3 text-muted-foreground">
                    {rule.max_severity_level ? `SEV-${rule.max_severity_level}+` : 'Every severity'}
                  </td>
                  <td className="py-2 text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Delete rule for ${roleLabel(rule.role)} on ${eventLabel(rule.event)}`}
                      onClick={() => deleteMutation.mutate(rule)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </td>
                </tr>
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
