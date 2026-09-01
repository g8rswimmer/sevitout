import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { InfoTooltip } from '@/components/ui/tooltip'
import { METRIC_DEFINITIONS } from '@/lib/metricDefinitions'
import { emptySLARowForm, minutesToSeconds, SEVERITY_LEVELS, type SLARowForm } from '@/lib/slaTargets'
import type { ServiceSLAResponse } from '@/types/api'

function toForm(sla?: ServiceSLAResponse): SLARowForm {
  return {
    mttd: sla?.mttd_target_seconds ? String(Math.round(Number(sla.mttd_target_seconds) / 60)) : '',
    mttm: sla?.mttm_target_seconds ? String(Math.round(Number(sla.mttm_target_seconds) / 60)) : '',
    mttr: sla?.mttr_target_seconds ? String(Math.round(Number(sla.mttr_target_seconds) / 60)) : '',
    rtpc: sla?.rtpc_target_seconds ? String(Math.round(Number(sla.rtpc_target_seconds) / 60)) : '',
  }
}

/** One target-minutes column header, an acronym plus an info icon whose
 * hover/focus tooltip gives the plain-English definition — the same
 * METRIC_DEFINITIONS text LifecyclePanel.tsx shows per-SEV, so an admin
 * unfamiliar with "MTTD" isn't left guessing what they're configuring.
 * Exported for reuse by AdminServicesPage.tsx's "New service" form, which
 * renders the identical header row while collecting initial SLA targets. */
export function ColumnHeader({ label, definition }: { label: string; definition: string }) {
  return (
    <th className="py-2 pr-3">
      <span className="inline-flex items-center gap-1">
        {label} target (min)
        <InfoTooltip text={definition} />
      </span>
    </th>
  )
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

  const [forms, setForms] = useState<Record<number, SLARowForm>>({})
  const [errors, setErrors] = useState<Record<number, string>>({})

  const byLevel = new Map((slas.data?.slas ?? []).map((s) => [s.severity_level, s]))

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'serviceSLA', serviceId] })

  const upsertMutation = useMutation({
    mutationFn: ({ level, form }: { level: number; form: SLARowForm }) =>
      api.config.serviceSLA.upsert(serviceId, level, {
        severity_level: level,
        mttd_target_seconds: minutesToSeconds(form.mttd),
        mttm_target_seconds: minutesToSeconds(form.mttm),
        mttr_target_seconds: minutesToSeconds(form.mttr),
        rtpc_target_seconds: minutesToSeconds(form.rtpc),
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
      setForms((f) => ({ ...f, [level]: emptySLARowForm() }))
    },
  })

  function formFor(level: number): SLARowForm {
    return forms[level] ?? toForm(byLevel.get(level))
  }

  function setFormFor(level: number, form: SLARowForm) {
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
              <ColumnHeader label="MTTD" definition={METRIC_DEFINITIONS.MTTD} />
              <ColumnHeader label="MTTM" definition={METRIC_DEFINITIONS.MTTM} />
              <ColumnHeader label="MTTR" definition={METRIC_DEFINITIONS.MTTR} />
              <ColumnHeader label="RTPC" definition={METRIC_DEFINITIONS.RTPC} />
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
                  <td className="py-2 pr-3">
                    <Input
                      type="number"
                      min={0}
                      aria-label={`RTPC target minutes for SEV-${level}`}
                      value={form.rtpc}
                      onChange={(e) => setFormFor(level, { ...form, rtpc: e.target.value })}
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
