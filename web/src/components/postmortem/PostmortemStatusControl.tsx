import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { POSTMORTEM_STATUS_LABELS, VALID_POSTMORTEM_TRANSITIONS, type PostmortemResponse } from '@/types/api'

/** Status workflow controls (Draft → In Review → Approved), mirroring
 * StatusTransitionControl's pattern for the SEV itself — only offers the
 * state machine's actual valid next statuses (internal/postmortem/
 * statemachine.go); no timestamp capture needed here, unlike the SEV's
 * mitigated/resolved transitions. */
export function PostmortemStatusControl({
  sevId,
  postmortem,
  canTransition,
}: {
  sevId: string
  postmortem: PostmortemResponse
  canTransition: boolean
}) {
  const queryClient = useQueryClient()
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: (to_status: PostmortemResponse['status']) => api.postmortems.transition(sevId, { to_status }),
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['postmortems', sevId] })
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Transition failed'),
  })

  if (!canTransition) return null

  const nextStatuses = VALID_POSTMORTEM_TRANSITIONS[postmortem.status] ?? []
  if (nextStatuses.length === 0) return null

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm text-muted-foreground">Transition to:</span>
        {nextStatuses.map((s) => (
          <Button
            key={s}
            size="sm"
            variant="outline"
            onClick={() => mutation.mutate(s)}
            disabled={mutation.isPending}
          >
            {POSTMORTEM_STATUS_LABELS[s]}
          </Button>
        ))}
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}
