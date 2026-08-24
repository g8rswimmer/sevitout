import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { formatDateTime } from '@/lib/format'
import { AUDIENCE_LABELS, type AnnouncementAudience } from '@/types/api'

const AUDIENCES = Object.keys(AUDIENCE_LABELS) as AnnouncementAudience[]

export function AnnouncementsPanel({ sevId, canPost }: { sevId: string; canPost: boolean }) {
  const queryClient = useQueryClient()
  const announcements = useQuery({
    queryKey: ['sevs', sevId, 'announcements'],
    queryFn: () => api.announcements.list(sevId),
  })

  const [message, setMessage] = useState('')
  const [audience, setAudience] = useState<AnnouncementAudience>('internal')
  const [isMilestone, setIsMilestone] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const create = useMutation({
    mutationFn: () => api.announcements.create(sevId, { message, audience, is_milestone: isMilestone }),
    onSuccess: () => {
      setMessage('')
      setIsMilestone(false)
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['sevs', sevId, 'announcements'] })
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to post announcement'),
  })

  const list = [...(announcements.data?.announcements ?? [])].sort(
    (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
  )

  return (
    <Section title="Announcements">
      {announcements.isLoading && <Skeleton className="h-8 w-full" />}
      {announcements.isError && (
        <p role="alert" className="text-sm text-destructive">
          Failed to load announcements: {(announcements.error as Error).message}
        </p>
      )}
      {announcements.data && list.length === 0 && (
        <p className="text-sm text-muted-foreground">No announcements yet.</p>
      )}
      {list.length > 0 && (
        <ul className="flex flex-col gap-3">
          {list.map((a) => (
            <li key={a.id} className="flex flex-col gap-1 border-b border-border pb-3 last:border-0 last:pb-0">
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Badge variant="outline">{AUDIENCE_LABELS[a.audience]}</Badge>
                {a.is_milestone && <Badge variant="secondary">Milestone</Badge>}
                <span>{formatDateTime(a.created_at)}</span>
              </div>
              <p className="text-sm">{a.message}</p>
            </li>
          ))}
        </ul>
      )}

      {canPost && (
        <form
          className="flex flex-col gap-2 border-t border-border pt-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (message.trim()) create.mutate()
          }}
        >
          <Textarea
            aria-label="Announcement message"
            placeholder="What's the update?"
            value={message}
            onChange={(e) => setMessage(e.target.value)}
          />
          <div className="flex flex-wrap items-center gap-2">
            <Select
              aria-label="Audience"
              value={audience}
              onChange={(e) => setAudience(e.target.value as AnnouncementAudience)}
              className="w-40"
            >
              {AUDIENCES.map((aud) => (
                <option key={aud} value={aud}>
                  {AUDIENCE_LABELS[aud]}
                </option>
              ))}
            </Select>
            <label className="flex items-center gap-1.5 text-sm">
              <Checkbox checked={isMilestone} onChange={(e) => setIsMilestone(e.target.checked)} />
              Milestone
            </label>
            <Button type="submit" size="sm" disabled={create.isPending || !message.trim()}>
              {create.isPending ? 'Posting…' : 'Post'}
            </Button>
          </div>
        </form>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </Section>
  )
}
