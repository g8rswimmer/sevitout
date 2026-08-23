import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Section } from '@/components/sev/Section'
import { SEV_ROLE_LABELS, type SEVRoleType } from '@/types/api'

const ROLE_TYPES = Object.keys(SEV_ROLE_LABELS) as SEVRoleType[]

export function RolesPanel({ sevId, canManage }: { sevId: string; canManage: boolean }) {
  const queryClient = useQueryClient()
  const roles = useQuery({ queryKey: ['sevs', sevId, 'roles'], queryFn: () => api.roles.list(sevId) })

  const [roleType, setRoleType] = useState<SEVRoleType>('responder')
  const [displayName, setDisplayName] = useState('')
  const [error, setError] = useState<string | null>(null)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['sevs', sevId, 'roles'] })

  const assign = useMutation({
    mutationFn: () => api.roles.assign(sevId, { role_type: roleType, display_name: displayName }),
    onSuccess: () => {
      setDisplayName('')
      setError(null)
      void invalidate()
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to assign role'),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.roles.remove(sevId, id),
    onSuccess: () => void invalidate(),
  })

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
            </li>
          ))}
        </ul>
      )}

      {canManage && (
        <form
          className="flex flex-wrap items-end gap-2 border-t border-border pt-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (displayName.trim()) assign.mutate()
          }}
        >
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
