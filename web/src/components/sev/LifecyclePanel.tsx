import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CalendarClock, Pencil } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Section } from '@/components/sev/Section'
import { formatDateTime, formatDurationSeconds } from '@/lib/format'
import type { SEVResponse } from '@/types/api'

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
      {editing ? (
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="lp-started" className="flex items-center gap-1.5">
                <CalendarClock className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
                Started at
              </Label>
              <Input
                id="lp-started"
                type="datetime-local"
                value={startedAt}
                onChange={(e) => setStartedAt(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="lp-detected" className="flex items-center gap-1.5">
                <CalendarClock className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
                Detected at
              </Label>
              <Input
                id="lp-detected"
                type="datetime-local"
                value={detectedAt}
                onChange={(e) => setDetectedAt(e.target.value)}
              />
            </div>
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
        <MetricField label="MTTD" value={sev.mttd_seconds} />
        <MetricField label="MTTM" value={sev.mttm_seconds} />
        <MetricField label="MTTR" value={sev.mttr_seconds} />
        <MetricField label="DTTM" value={sev.dttm_seconds} />
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

function MetricField({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="font-medium">{formatDurationSeconds(value)}</dd>
    </div>
  )
}
