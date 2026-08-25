import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'

/**
 * Explicit per-user visibility grants for a Sensitive SEV (§14). Only
 * rendered when the SEV itself is sensitive — see SevDetailPage.tsx.
 * Grant/revoke require Incident Commander or Admin (canManage); anyone who
 * can already see the SEV (i.e. reached this page at all) can view the list.
 */
export function AllowedViewersPanel({ sevId, canManage }: { sevId: string; canManage: boolean }) {
  const queryClient = useQueryClient()
  const access = useQuery({ queryKey: ['sevs', sevId, 'access'], queryFn: () => api.sevAccess.list(sevId) })
  // Only fetched for the picker — canManage already gates rendering the form,
  // and GrantAccess is Admin/IC-gated server-side regardless.
  const users = useQuery({ queryKey: ['admin', 'users'], queryFn: () => api.config.users.list(), enabled: canManage })

  const [userId, setUserId] = useState('')
  const [error, setError] = useState<string | null>(null)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['sevs', sevId, 'access'] })

  const grant = useMutation({
    mutationFn: () => api.sevAccess.grant(sevId, { user_id: userId }),
    onSuccess: () => {
      setUserId('')
      setError(null)
      void invalidate()
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to grant access'),
  })

  const revoke = useMutation({
    mutationFn: (id: string) => api.sevAccess.revoke(sevId, id),
    onSuccess: () => void invalidate(),
  })

  const userLabel = (id: string) => {
    const u = users.data?.users?.find((u) => u.id === id)
    return u ? `${u.name} (${u.email})` : id
  }

  const grantable = (users.data?.users ?? []).filter(
    (u) => !(access.data?.access ?? []).some((a) => a.user_id === u.id),
  )

  return (
    <Section title="Allowed Viewers">
      {access.isLoading && <Skeleton className="h-8 w-full" />}
      {access.isError && (
        <p role="alert" className="text-sm text-destructive">
          Failed to load access grants: {(access.error as Error).message}
        </p>
      )}
      {access.data && (access.data.access ?? []).length === 0 && (
        <p className="text-sm text-muted-foreground">
          No one has been explicitly granted access yet — only Admins, Incident Commanders, and the reporter can view
          this SEV.
        </p>
      )}
      {access.data && (access.data.access ?? []).length > 0 && (
        <ul className="flex flex-col gap-2">
          {access.data.access!.map((a) => (
            <li key={a.id} className="flex items-center justify-between gap-2 text-sm">
              <span>{userLabel(a.user_id)}</span>
              {canManage && (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Revoke access for ${userLabel(a.user_id)}`}
                  onClick={() => revoke.mutate(a.id)}
                  disabled={revoke.isPending}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      {canManage && (
        <form
          className="flex flex-wrap items-end gap-2 border-t border-border pt-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (userId) grant.mutate()
          }}
        >
          <Select aria-label="User to grant access" value={userId} onChange={(e) => setUserId(e.target.value)} className="w-64">
            <option value="">Select a user…</option>
            {grantable.map((u) => (
              <option key={u.id} value={u.id}>
                {u.name} ({u.email})
              </option>
            ))}
          </Select>
          <Button type="submit" size="sm" disabled={grant.isPending || !userId}>
            {grant.isPending ? 'Granting…' : 'Grant access'}
          </Button>
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
