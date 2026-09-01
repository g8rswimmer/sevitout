import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { LogIn } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { useEnabledIntegrations } from '@/lib/useEnabledIntegrations'

/** Self-service "add me to the incident channel" action (Roadmap Phase
 * 11c). Lives at the top of the SEV detail page next to Share/Postmortem —
 * not tucked inside the Roles section — since it's about the *viewer
 * themselves* joining, not role management, and needs to be easy to spot.
 *
 * Rendered whenever the "slack" integration is enabled; disabled (not
 * hidden) when this SEV has no recorded incident channel yet (an older SEV,
 * or one where Slack hasn't created a channel at all) so the action stays
 * discoverable with an explanatory tooltip rather than silently absent. */
export function JoinSlackChannelButton({ sevId, slackChannelId }: { sevId: string; slackChannelId?: string }) {
  const { isEnabled } = useEnabledIntegrations()
  const [error, setError] = useState<string | null>(null)
  const [joined, setJoined] = useState(false)

  const join = useMutation({
    mutationFn: () => api.roles.joinSlackChannel(sevId),
    onSuccess: () => {
      setError(null)
      setJoined(true)
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to join Slack channel'),
  })

  if (!isEnabled('slack')) return null

  return (
    <div className="flex flex-col items-end gap-1">
      <Button
        type="button"
        size="sm"
        variant="outline"
        title={slackChannelId ? undefined : 'This SEV has no Slack channel'}
        onClick={() => join.mutate()}
        disabled={!slackChannelId || join.isPending}
      >
        <LogIn className="h-3.5 w-3.5" /> {join.isPending ? 'Joining…' : joined ? 'Joined' : 'Join Slack channel'}
      </Button>
      {error && (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}
