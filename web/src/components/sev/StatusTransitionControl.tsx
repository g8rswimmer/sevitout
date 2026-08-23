import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { SEV_STATUS_LABELS, VALID_STATUS_TRANSITIONS, type SEVResponse, type SEVStatus } from '@/types/api'

/** now(), formatted for an <input type="datetime-local">. */
function nowLocalInputValue(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** Statuses whose transition benefits from capturing a timestamp — without
 * one, internal/api/grpc/sev.go's TransitionStatus leaves mitigated_at/
 * resolved_at unset (it only defaults postmortem_completed_at), which would
 * leave MTTM/MTTR uncomputed. */
const TIMESTAMP_FIELD: Partial<Record<SEVStatus, 'mitigated_at' | 'resolved_at'>> = {
  mitigated: 'mitigated_at',
  resolved: 'resolved_at',
}

export function StatusTransitionControl({ sev, canTransition }: { sev: SEVResponse; canTransition: boolean }) {
  const queryClient = useQueryClient()
  const [pendingStatus, setPendingStatus] = useState<SEVStatus | null>(null)
  const [timestamp, setTimestamp] = useState('')
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: (toStatus: SEVStatus) => {
      const field = TIMESTAMP_FIELD[toStatus]
      return api.sevs.transition(sev.id, {
        to_status: toStatus,
        ...(field ? { [field]: timestamp ? new Date(timestamp).toISOString() : new Date().toISOString() } : {}),
      })
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sevs', 'detail', sev.id] })
      void queryClient.invalidateQueries({ queryKey: ['sevs', 'active'] })
      setPendingStatus(null)
      setError(null)
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Transition failed'),
  })

  if (!canTransition) return null

  const nextStatuses = VALID_STATUS_TRANSITIONS[sev.status] ?? []
  if (nextStatuses.length === 0) return null

  if (sev.locked) {
    return (
      <p className="text-sm text-muted-foreground">
        This SEV is locked (postmortem complete). Unlocking is available from the postmortem page.
      </p>
    )
  }

  function pick(toStatus: SEVStatus) {
    setError(null)
    if (TIMESTAMP_FIELD[toStatus]) {
      setTimestamp(nowLocalInputValue())
      setPendingStatus(toStatus)
    } else {
      mutation.mutate(toStatus)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm text-muted-foreground">Transition to:</span>
        {nextStatuses.map((s) => (
          <Button key={s} size="sm" variant="outline" onClick={() => pick(s)} disabled={mutation.isPending}>
            {SEV_STATUS_LABELS[s]}
          </Button>
        ))}
      </div>

      {pendingStatus && (
        <div className="flex items-center gap-2 rounded-md border border-border p-3">
          <label className="text-sm" htmlFor="transition-ts">
            {SEV_STATUS_LABELS[pendingStatus]} at:
          </label>
          <Input
            id="transition-ts"
            type="datetime-local"
            value={timestamp}
            onChange={(e) => setTimestamp(e.target.value)}
            className="w-56"
          />
          <Button size="sm" onClick={() => mutation.mutate(pendingStatus)} disabled={mutation.isPending}>
            {mutation.isPending ? 'Transitioning…' : 'Confirm'}
          </Button>
          <Button size="sm" variant="outline" onClick={() => setPendingStatus(null)}>
            Cancel
          </Button>
        </div>
      )}

      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}
