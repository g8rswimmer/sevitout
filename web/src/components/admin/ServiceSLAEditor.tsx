import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import type { ServiceSLAResponse } from '@/types/api'

const SEVERITY_LEVELS = [1, 2, 3, 4]

interface RowForm {
  mttd: string // minutes, empty string = unset
  mttm: string
  mttr: string
}

function toForm(sla?: ServiceSLAResponse): RowForm {
  return {
    mttd: sla?.mttd_target_seconds ? String(Math.round(Number(sla.mttd_target_seconds) / 60)) : '',
    mttm: sla?.mttm_target_seconds ? String(Math.round(Number(sla.mttm_target_seconds) / 60)) : '',
    mttr: sla?.mttr_target_seconds ? String(Math.round(Number(sla.mttr_target_seconds) / 60)) : '',
  }
}

/** minutes (a form field's string value) → seconds, or undefined when the
 * field is blank (clears that metric's target — UpsertServiceSLA is a
 * full-replace, like UpdateRetentionConfigRequest, not a sparse patch). */
function minutesToSeconds(v: string): number | undefined {
  const n = Number(v)
  return v.trim() !== '' && n > 0 ? Math.round(n * 60) : undefined
}

/** Per-service, per-severity-level SLA target editor (Roadmap Phase 12) —
 * a 4-row table, one per severity level, modeled directly on
 * AdminRetentionPage.tsx's own per-severity-level table. Reached from
 * AdminServicesPage.tsx's per-service "SLAs" action. */
export function ServiceSLAEditor({ serviceId }: { serviceId: string }) {
  const queryClient = useQueryClient()
  const slas = useQuery({
    queryKey: ['admin', 'serviceSLA', serviceId],
    queryFn: () => api.config.serviceSLA.list(serviceId),
  })

  const [forms, setForms] = useState<Record<number, RowForm>>({})
  const [errors, setErrors] = useState<Record<number, string>>({})

  const byLevel = new Map((slas.data?.slas ?? []).map((s) => [s.severity_level, s]))

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'serviceSLA', serviceId] })

  const upsertMutation = useMutation({
    mutationFn: ({ level, form }: { level: number; form: RowForm }) =>
      api.config.serviceSLA.upsert(serviceId, level, {
        severity_level: level,
        mttd_target_seconds: minutesToSeconds(form.mttd),
        mttm_target_seconds: minutesToSeconds(form.mttm),
        mttr_target_seconds: minutesToSeconds(form.mttr),
      }),
    onSuccess: (_data, { level }) => {
      invalidate()
      setErrors((e) => ({ ...e, [level]: '' }))
    },
    onError: (err, { level }) =>
      setErrors((e) => ({ ...e, [level]: err instanceof ApiError ? err.message : 'Failed to save' })),
  })

  const clearMutation = useMutation({
    mutationFn: (level: number) => api.config.serviceSLA.delete(serviceId, level),
    onSuccess: (_data, level) => {
      invalidate()
      setForms((f) => ({ ...f, [level]: { mttd: '', mttm: '', mttr: '' } }))
    },
  })

  function formFor(level: number): RowForm {
    return forms[level] ?? toForm(byLevel.get(level))
  }

  function setFormFor(level: number, form: RowForm) {
    setForms((f) => ({ ...f, [level]: form }))
  }

  if (slas.isLoading) {
    return <Skeleton className="h-24 w-full" />
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-3">
      <p className="text-xs text-muted-foreground">
        Target response times per severity level, in minutes. A blank field means no SLA is set for that metric — no
        breach indicator shows on a SEV unless at least one of its affected services configures a target here. When a
        SEV has multiple affected services, the strictest configured target across all of them applies.
      </p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="py-2 pr-3">Severity</th>
              <th className="py-2 pr-3">MTTD target (min)</th>
              <th className="py-2 pr-3">MTTM target (min)</th>
              <th className="py-2 pr-3">MTTR target (min)</th>
              <th className="py-2" />
            </tr>
          </thead>
          <tbody>
            {SEVERITY_LEVELS.map((level) => {
              const sla = byLevel.get(level)
              const form = formFor(level)
              return (
                <tr key={level} className="border-b border-border align-top last:border-0">
                  <td className="py-2 pr-3 font-medium">SEV-{level}</td>
                  <td className="py-2 pr-3">
                    <Input
                      type="number"
                      min={0}
                      aria-label={`MTTD target minutes for SEV-${level}`}
                      value={form.mttd}
                      onChange={(e) => setFormFor(level, { ...form, mttd: e.target.value })}
                      className="w-24"
                    />
                  </td>
                  <td className="py-2 pr-3">
                    <Input
                      type="number"
                      min={0}
                      aria-label={`MTTM target minutes for SEV-${level}`}
                      value={form.mttm}
                      onChange={(e) => setFormFor(level, { ...form, mttm: e.target.value })}
                      className="w-24"
                    />
                  </td>
                  <td className="py-2 pr-3">
                    <Input
                      type="number"
                      min={0}
                      aria-label={`MTTR target minutes for SEV-${level}`}
                      value={form.mttr}
                      onChange={(e) => setFormFor(level, { ...form, mttr: e.target.value })}
                      className="w-24"
                    />
                  </td>
                  <td className="py-2 text-right">
                    <div className="flex justify-end gap-1">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => upsertMutation.mutate({ level, form })}
                        disabled={upsertMutation.isPending && upsertMutation.variables?.level === level}
                      >
                        {upsertMutation.isPending && upsertMutation.variables?.level === level ? 'Saving…' : 'Save'}
                      </Button>
                      {sla && (
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
                    {errors[level] && (
                      <p role="alert" className="mt-1 text-xs text-destructive">
                        {errors[level]}
                      </p>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
