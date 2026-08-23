import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Pencil } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { InfoTooltip } from '@/components/ui/tooltip'
import { Section } from '@/components/sev/Section'
import { DateTimeField } from '@/components/sev/DateTimeField'
import { formatDateTime, formatDurationSeconds } from '@/lib/format'
import { SEV_LIFECYCLE_STAGES, SEV_STATUS_LABELS, type SEVResponse } from '@/types/api'

/** Definitions shown in each metric's info tooltip — docs/requirements.md
 * §2.2 spells out the formula; this is the plain-English acronym expansion. */
const METRIC_DEFINITIONS = {
  MTTD: 'Mean Time to Detect — detected_at − started_at',
  MTTM: 'Mean Time to Mitigate — mitigated_at − started_at',
  MTTR: 'Mean Time to Resolve — resolved_at − started_at',
  DTTM: 'Detection to Mitigation — mitigated_at − detected_at',
} as const

/** yyyy-MM-ddThh:mm for an <input type="datetime-local">, in local time. */
function toLocalInputValue(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function LifecyclePanel({ sev, canEdit }: { sev: SEVResponse; canEdit: boolean }) {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [startedAt, setStartedAt] = useState('')
  const [detectedAt, setDetectedAt] = useState('')
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: () =>
      api.sevs.update(sev.id, {
        started_at: startedAt ? new Date(startedAt).toISOString() : undefined,
        detected_at: detectedAt ? new Date(detectedAt).toISOString() : undefined,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sevs', 'detail', sev.id] })
      setEditing(false)
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to save'),
  })

  function startEditing() {
    setStartedAt(toLocalInputValue(sev.started_at))
    setDetectedAt(toLocalInputValue(sev.detected_at))
    setError(null)
    setEditing(true)
  }

  return (
    <Section
      title="Lifecycle"
      action={
        canEdit &&
        !editing && (
          <Button variant="ghost" size="sm" onClick={startEditing}>
            <Pencil className="h-3.5 w-3.5" /> Edit
          </Button>
        )
      }
    >
      <ol className="flex flex-wrap items-center gap-1.5 text-xs" aria-label="Lifecycle stages">
        {SEV_LIFECYCLE_STAGES.map((stage, i) => (
          <li key={stage} className="flex items-center gap-1.5">
            <span
              className={`rounded-md px-2 py-0.5 ${
                stage === sev.status
                  ? 'bg-primary font-medium text-primary-foreground'
                  : 'bg-muted text-muted-foreground'
              }`}
            >
              {SEV_STATUS_LABELS[stage]}
            </span>
            {i < SEV_LIFECYCLE_STAGES.length - 1 && <span className="text-muted-foreground">→</span>}
          </li>
        ))}
      </ol>

      {editing ? (
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <DateTimeField id="lp-started" label="Started at" value={startedAt} onChange={setStartedAt} />
            <DateTimeField id="lp-detected" label="Detected at" value={detectedAt} onChange={setDetectedAt} />
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex gap-2">
            <Button size="sm" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
              {mutation.isPending ? 'Saving…' : 'Save'}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setEditing(false)}>
              Cancel
            </Button>
          </div>
        </div>
      ) : (
        <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm sm:grid-cols-3">
          <TimestampField label="Started" value={sev.started_at} />
          <TimestampField label="Detected" value={sev.detected_at} />
          <TimestampField label="Mitigated" value={sev.mitigated_at} />
          <TimestampField label="Resolved" value={sev.resolved_at} />
          <TimestampField label="Postmortem complete" value={sev.postmortem_completed_at} />
        </dl>
      )}

      <div className="grid grid-cols-2 gap-x-4 gap-y-2 border-t border-border pt-3 text-sm sm:grid-cols-4">
        <MetricField label="MTTD" definition={METRIC_DEFINITIONS.MTTD} value={sev.mttd_seconds} />
        <MetricField label="MTTM" definition={METRIC_DEFINITIONS.MTTM} value={sev.mttm_seconds} />
        <MetricField label="MTTR" definition={METRIC_DEFINITIONS.MTTR} value={sev.mttr_seconds} />
        <MetricField label="DTTM" definition={METRIC_DEFINITIONS.DTTM} value={sev.dttm_seconds} />
      </div>
    </Section>
  )
}

function TimestampField({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd>{formatDateTime(value)}</dd>
    </div>
  )
}

function MetricField({ label, definition, value }: { label: string; definition: string; value?: string }) {
  return (
    <div>
      <dt className="flex items-center gap-1 text-xs text-muted-foreground">
        {label}
        <InfoTooltip text={definition} />
      </dt>
      <dd className="font-medium">{formatDurationSeconds(value)}</dd>
    </div>
  )
}
