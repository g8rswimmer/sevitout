import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { MessageSquarePlus, Trash2, X } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Section } from '@/components/sev/Section'
import { useEnabledIntegrations } from '@/lib/useEnabledIntegrations'
import { SEV_ROLE_LABELS, type DirectoryUser, type SEVRoleType } from '@/types/api'

const ROLE_TYPES = Object.keys(SEV_ROLE_LABELS) as SEVRoleType[]

export function RolesPanel({
  sevId,
  canManage,
  slackChannelId,
}: {
  sevId: string
  canManage: boolean
  /** SEVResponse.slack_channel_id — gates the per-role "Add to chat" button
   * (Roadmap Phase 10e): disabled (not hidden — the intent stays visible
   * even for older SEVs) whenever the SEV has no incident channel. */
  slackChannelId?: string
}) {
  const queryClient = useQueryClient()
  const roles = useQuery({ queryKey: ['sevs', sevId, 'roles'], queryFn: () => api.roles.list(sevId) })
  // Roadmap Phase 11b: the per-role "Add to chat" button renders only when
  // the "slack" integration is configured — not just when a channel exists.
  // (Self-service "Join Slack channel" lives at the top of the SEV detail
  // page — see JoinSlackChannelButton.tsx — not in this section.)
  const { isEnabled: isIntegrationEnabled } = useEnabledIntegrations()
  const slackEnabled = isIntegrationEnabled('slack')

  const [roleType, setRoleType] = useState<SEVRoleType>('responder')
  const [displayName, setDisplayName] = useState('')
  const [pickedUser, setPickedUser] = useState<DirectoryUser | null>(null)
  const [pickerQuery, setPickerQuery] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [inviteError, setInviteError] = useState<{ id: string; message: string } | null>(null)

  // Only searches once at least 2 characters are typed, so every keystroke
  // on a short/empty query doesn't fire a request.
  const directory = useQuery({
    queryKey: ['directory', pickerQuery],
    queryFn: () => api.auth.directory({ query: pickerQuery }),
    enabled: pickerQuery.trim().length >= 2,
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['sevs', sevId, 'roles'] })

  const assign = useMutation({
    mutationFn: () => api.roles.assign(sevId, { role_type: roleType, display_name: displayName, user_id: pickedUser?.id }),
    onSuccess: () => {
      setDisplayName('')
      setPickedUser(null)
      setPickerQuery('')
      setError(null)
      void invalidate()
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to assign role'),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.roles.remove(sevId, id),
    onSuccess: () => void invalidate(),
  })

  const inviteToSlack = useMutation({
    mutationFn: (id: string) => api.roles.inviteToSlack(sevId, id),
    onSuccess: () => setInviteError(null),
    onError: (err, id) =>
      setInviteError({ id, message: err instanceof ApiError ? err.message : 'Failed to invite to Slack' }),
  })

  function pickUser(u: DirectoryUser) {
    setPickedUser(u)
    setPickerQuery('')
    if (!displayName.trim()) setDisplayName(u.name)
  }

  const directoryMatches = pickerQuery.trim().length >= 2 ? (directory.data?.users ?? []) : []

  return (
    <Section title="Roles">
      {roles.isLoading && <Skeleton className="h-8 w-full" />}
      {roles.isError && (
        <p role="alert" className="text-sm text-destructive">
          Failed to load roles: {(roles.error as Error).message}
        </p>
      )}
      {roles.data && (roles.data.roles ?? []).length === 0 && (
        <p className="text-sm text-muted-foreground">No roles assigned yet.</p>
      )}
      {roles.data && (roles.data.roles ?? []).length > 0 && (
        <ul className="flex flex-col gap-2">
          {roles.data.roles!.map((r) => (
            <li key={r.id} className="flex items-center justify-between gap-2 text-sm">
              <div className="flex items-center gap-2">
                <Badge variant="secondary">{SEV_ROLE_LABELS[r.role_type]}</Badge>
                <span>{r.display_name || r.user_id || '—'}</span>
              </div>
              <div className="flex items-center gap-1">
                {canManage && slackEnabled && (
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Add ${SEV_ROLE_LABELS[r.role_type]} to Slack channel`}
                    title={slackChannelId ? 'Add to chat' : 'This SEV has no Slack channel'}
                    onClick={() => inviteToSlack.mutate(r.id)}
                    disabled={!slackChannelId || (inviteToSlack.isPending && inviteToSlack.variables === r.id)}
                  >
                    <MessageSquarePlus className="h-3.5 w-3.5" />
                  </Button>
                )}
                {canManage && (
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Remove ${SEV_ROLE_LABELS[r.role_type]}`}
                    onClick={() => remove.mutate(r.id)}
                    disabled={remove.isPending}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {inviteError && (
        <p role="alert" className="text-sm text-destructive">
          {inviteError.message}
        </p>
      )}

      {canManage && (
        <form
          className="flex flex-col gap-2 border-t border-border pt-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (displayName.trim()) assign.mutate()
          }}
        >
          <div className="flex flex-wrap items-end gap-2">
            <Select
              aria-label="Role type"
              value={roleType}
              onChange={(e) => setRoleType(e.target.value as SEVRoleType)}
              className="w-48"
            >
              {ROLE_TYPES.map((rt) => (
                <option key={rt} value={rt}>
                  {SEV_ROLE_LABELS[rt]}
                </option>
              ))}
            </Select>
            <Input
              aria-label="Person's name"
              placeholder="Name"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              className="w-48"
            />
            <Button type="submit" size="sm" disabled={assign.isPending || !displayName.trim()}>
              {assign.isPending ? 'Assigning…' : 'Assign'}
            </Button>
          </div>

          {/* Optional user picker (Roadmap Phase 10c): finding a real user
              here sets user_id on the AssignRole call, so Slack auto-invite
              and the "Add to chat" button can resolve them without relying
              on the free-text display_name above. The free-text-only path
              stays fully supported — this is additive, not a replacement. */}
          <div className="flex flex-col gap-1">
            {pickedUser ? (
              <div className="flex w-fit items-center gap-1.5 rounded-md border border-border bg-muted/50 px-2 py-1 text-xs">
                Linked to <span className="font-medium">{pickedUser.name}</span> ({pickedUser.email})
                <button
                  type="button"
                  aria-label="Unlink picked user"
                  onClick={() => setPickedUser(null)}
                  className="text-muted-foreground hover:text-foreground"
                >
                  <X className="h-3 w-3" />
                </button>
              </div>
            ) : (
              <div className="relative w-64">
                <Input
                  aria-label="Search users to link"
                  placeholder="Search users to link (optional)…"
                  value={pickerQuery}
                  onChange={(e) => setPickerQuery(e.target.value)}
                />
                {directoryMatches.length > 0 && (
                  <ul className="absolute z-10 mt-1 w-full rounded-md border border-border bg-popover shadow-md">
                    {directoryMatches.slice(0, 8).map((u) => (
                      <li key={u.id}>
                        <button
                          type="button"
                          onClick={() => pickUser(u)}
                          className="flex w-full flex-col items-start px-2 py-1.5 text-left text-xs hover:bg-accent"
                        >
                          <span className="font-medium">{u.name}</span>
                          <span className="text-muted-foreground">{u.email}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
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
