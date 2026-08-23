import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Trash2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { SEV_RELATIONSHIP_LABELS, type SEVRelationshipType } from '@/types/api'

const RELATIONSHIP_TYPES = Object.keys(SEV_RELATIONSHIP_LABELS) as SEVRelationshipType[]

export function LinkedSevsPanel({ sevId, canManage }: { sevId: string; canManage: boolean }) {
  const queryClient = useQueryClient()
  const links = useQuery({ queryKey: ['sevs', sevId, 'links'], queryFn: () => api.sevLinks.list(sevId) })

  const [targetSevId, setTargetSevId] = useState('')
  const [relationshipType, setRelationshipType] = useState<SEVRelationshipType>('related')
  const [error, setError] = useState<string | null>(null)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['sevs', sevId, 'links'] })

  const create = useMutation({
    mutationFn: () => api.sevLinks.link(sevId, { target_sev_id: targetSevId, relationship_type: relationshipType }),
    onSuccess: () => {
      setTargetSevId('')
      setError(null)
      void invalidate()
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to link SEV'),
  })

  const remove = useMutation({
    mutationFn: (target: string) => api.sevLinks.unlink(sevId, target),
    onSuccess: () => void invalidate(),
  })

  const list = links.data?.links ?? []

  return (
    <Section title="Linked SEVs">
      {links.isLoading && <Skeleton className="h-8 w-full" />}
      {links.isError && (
        <p role="alert" className="text-sm text-destructive">
          Failed to load linked SEVs: {(links.error as Error).message}
        </p>
      )}
      {links.data && list.length === 0 && <p className="text-sm text-muted-foreground">No linked SEVs yet.</p>}
      {list.length > 0 && (
        <ul className="flex flex-col gap-2">
          {list.map((l) => (
            <li key={l.id} className="flex items-center justify-between gap-2 text-sm">
              <div className="flex items-center gap-2">
                <Badge variant="outline">{SEV_RELATIONSHIP_LABELS[l.relationship_type]}</Badge>
                <Link to={`/sevs/${l.target_sev_id}`} className="font-medium hover:underline">
                  {l.target_sev_id}
                </Link>
              </div>
              {canManage && (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Unlink ${l.target_sev_id}`}
                  onClick={() => remove.mutate(l.target_sev_id)}
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
            if (targetSevId.trim()) create.mutate()
          }}
        >
          <Input
            aria-label="Target SEV ID"
            placeholder="SEV-2026-0001"
            value={targetSevId}
            onChange={(e) => setTargetSevId(e.target.value)}
            className="w-44"
          />
          <Select
            aria-label="Relationship type"
            value={relationshipType}
            onChange={(e) => setRelationshipType(e.target.value as SEVRelationshipType)}
            className="w-44"
          >
            {RELATIONSHIP_TYPES.map((rt) => (
              <option key={rt} value={rt}>
                {SEV_RELATIONSHIP_LABELS[rt]}
              </option>
            ))}
          </Select>
          <Button type="submit" size="sm" disabled={create.isPending || !targetSevId.trim()}>
            {create.isPending ? 'Linking…' : 'Link'}
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
