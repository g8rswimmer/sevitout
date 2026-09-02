import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Skeleton } from '@/components/ui/skeleton'
// SEVERITY_LEVELS is generic ([1, 2, 3, 4]) despite its doc comment reading
// as SLA-specific — reused here rather than duplicating the same constant in
// a new file.
import { SEVERITY_LEVELS } from '@/lib/slaTargets'

/** Per-service, per-severity-level SEV leveling guidance editor (Roadmap
 * Phase 14) — a 4-row table, one per severity level, modeled directly on
 * ServiceSLAEditor.tsx's own per-severity-level table. Reached from
 * AdminServicesPage.tsx's per-service "Leveling criteria" action. Unlike
 * ServiceSLAEditor, this is a single free-text column per row, since it's
 * advisory guidance text, not a numeric target — never enforced or
 * validated against. */
export function LevelingCriteriaEditor({ serviceId }: { serviceId: string }) {
  const queryClient = useQueryClient()
  const criteria = useQuery({
    queryKey: ['admin', 'levelingCriteria', serviceId],
    queryFn: () => api.config.levelingCriteria.list(serviceId),
  })

  const [forms, setForms] = useState<Record<number, string>>({})
  const [errors, setErrors] = useState<Record<number, string>>({})

  const byLevel = new Map((criteria.data?.criteria ?? []).map((c) => [c.severity_level, c]))

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'levelingCriteria', serviceId] })

  const upsertMutation = useMutation({
    mutationFn: ({ level, text }: { level: number; text: string }) =>
      api.config.levelingCriteria.upsert(serviceId, level, { severity_level: level, criteria: text }),
    onSuccess: (_data, { level }) => {
      invalidate()
      setErrors((e) => ({ ...e, [level]: '' }))
    },
    onError: (err, { level }) =>
      setErrors((e) => ({ ...e, [level]: err instanceof ApiError ? err.message : 'Failed to save' })),
  })

  const clearMutation = useMutation({
    mutationFn: (level: number) => api.config.levelingCriteria.delete(serviceId, level),
    onSuccess: (_data, level) => {
      invalidate()
      setForms((f) => ({ ...f, [level]: '' }))
    },
    onError: (err, level) =>
      setErrors((e) => ({ ...e, [level]: err instanceof ApiError ? err.message : 'Failed to clear' })),
  })

  function textFor(level: number): string {
    return forms[level] ?? byLevel.get(level)?.criteria ?? ''
  }

  function setTextFor(level: number, text: string) {
    setForms((f) => ({ ...f, [level]: text }))
  }

  if (criteria.isLoading) {
    return <Skeleton className="h-24 w-full" />
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-3">
      <p className="text-xs text-muted-foreground">
        Guidance for what qualifies as each severity level for this service — shown on the SEV creation form and the
        postmortem page to help pick (and later sanity-check) the right level. This is advisory only: it's never
        enforced or validated against, so a SEV can still be opened or transitioned at any level regardless of what's
        written here.
      </p>
      <div className="flex flex-col gap-3">
        {SEVERITY_LEVELS.map((level) => {
          const row = byLevel.get(level)
          const text = textFor(level)
          return (
            <div key={level} className="flex flex-col gap-1.5">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium">SEV-{level}</span>
                <div className="flex gap-1">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => upsertMutation.mutate({ level, text })}
                    disabled={upsertMutation.isPending && upsertMutation.variables?.level === level}
                  >
                    {upsertMutation.isPending && upsertMutation.variables?.level === level ? 'Saving…' : 'Save'}
                  </Button>
                  {row && (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => clearMutation.mutate(level)}
                      disabled={clearMutation.isPending && clearMutation.variables === level}
                    >
                      Clear
                    </Button>
                  )}
                </div>
              </div>
              <Textarea
                aria-label={`Leveling criteria for SEV-${level}`}
                value={text}
                onChange={(e) => setTextFor(level, e.target.value)}
                placeholder={`What makes an incident SEV-${level} for this service?`}
                className="min-h-16 text-sm"
              />
              {errors[level] && (
                <p role="alert" className="text-xs text-destructive">
                  {errors[level]}
                </p>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
