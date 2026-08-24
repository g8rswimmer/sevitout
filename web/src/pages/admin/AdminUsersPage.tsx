import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { formatDateTime } from '@/lib/format'
import { ORG_ROLE_LABELS, type OrgRole, type UserResponse } from '@/types/api'

const ORG_ROLES = Object.keys(ORG_ROLE_LABELS) as OrgRole[]

export function AdminUsersPage() {
  const queryClient = useQueryClient()
  const [queryInput, setQueryInput] = useState('')
  const [query, setQuery] = useState('')
  const [rowError, setRowError] = useState<{ id: string; message: string } | null>(null)

  const users = useQuery({
    queryKey: ['admin', 'users', query],
    queryFn: () => api.config.users.list(query || undefined),
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'users'] })

  const roleMutation = useMutation({
    mutationFn: ({ id, org_role }: { id: string; org_role: OrgRole }) => api.config.users.updateRole(id, { org_role }),
    onSuccess: () => {
      invalidate()
      setRowError(null)
    },
    onError: (err, { id }) =>
      setRowError({ id, message: err instanceof ApiError ? err.message : 'Failed to update role' }),
  })

  const activeMutation = useMutation({
    mutationFn: ({ id, active }: { id: string; active: boolean }) =>
      active ? api.config.users.reactivate(id) : api.config.users.deactivate(id),
    onSuccess: () => {
      invalidate()
      setRowError(null)
    },
    onError: (err, { id }) =>
      setRowError({ id, message: err instanceof ApiError ? err.message : 'Failed to update user' }),
  })

  function handleSearchSubmit(e: FormEvent) {
    e.preventDefault()
    setQuery(queryInput.trim())
  }

  const list: UserResponse[] = users.data?.users ?? []

  return (
    <Section title="User management">
      <form onSubmit={handleSearchSubmit} className="flex gap-2">
        <Input
          aria-label="Search users"
          placeholder="Search by name or email…"
          value={queryInput}
          onChange={(e) => setQueryInput(e.target.value)}
        />
        <Button type="submit" variant="outline" size="sm">
          <Search className="h-3.5 w-3.5" /> Search
        </Button>
      </form>

      {users.isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : list.length === 0 ? (
        <p className="text-sm text-muted-foreground">No users found.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3">Name</th>
                <th className="py-2 pr-3">Email</th>
                <th className="py-2 pr-3">Role</th>
                <th className="py-2 pr-3">Status</th>
                <th className="py-2 pr-3">Joined</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {list.map((u) => (
                <tr key={u.id} className="border-b border-border align-top">
                  <td className="py-2 pr-3">{u.name}</td>
                  <td className="py-2 pr-3 text-muted-foreground">{u.email}</td>
                  <td className="py-2 pr-3">
                    <Select
                      aria-label={`Role for ${u.name}`}
                      value={u.org_role}
                      disabled={roleMutation.isPending && roleMutation.variables?.id === u.id}
                      onChange={(e) => roleMutation.mutate({ id: u.id, org_role: e.target.value as OrgRole })}
                      className="w-44"
                    >
                      {ORG_ROLES.map((r) => (
                        <option key={r} value={r}>
                          {ORG_ROLE_LABELS[r]}
                        </option>
                      ))}
                    </Select>
                  </td>
                  <td className="py-2 pr-3">
                    <Badge variant={u.active === false ? 'outline' : 'secondary'}>
                      {u.active === false ? 'Deactivated' : 'Active'}
                    </Badge>
                  </td>
                  <td className="py-2 pr-3 text-muted-foreground">{formatDateTime(u.created_at)}</td>
                  <td className="py-2 text-right">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={activeMutation.isPending && activeMutation.variables?.id === u.id}
                      onClick={() => activeMutation.mutate({ id: u.id, active: u.active === false })}
                    >
                      {u.active === false ? 'Reactivate' : 'Deactivate'}
                    </Button>
                    {rowError?.id === u.id && (
                      <p role="alert" className="mt-1 text-xs text-destructive">
                        {rowError.message}
                      </p>
                    )}
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
