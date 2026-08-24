import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { formatDateTime } from '@/lib/format'
import type { RetentionConfigResponse } from '@/types/api'

const SEVERITY_LEVELS = [1, 2, 3, 4]

interface RowForm {
  retentionDays: string
  hardDelete: boolean
}

function toForm(cfg?: RetentionConfigResponse): RowForm {
  return {
    retentionDays: String(cfg?.retention_days ?? 0),
    hardDelete: cfg?.hard_delete ?? false,
  }
}

export function AdminRetentionPage() {
  const queryClient = useQueryClient()
  const retention = useQuery({ queryKey: ['admin', 'retention'], queryFn: api.config.retention.list })

  const [forms, setForms] = useState<Record<number, RowForm>>({})
  const [errors, setErrors] = useState<Record<number, string>>({})

  const byLevel = new Map((retention.data?.configs ?? []).map((c) => [c.severity_level, c]))

  const updateMutation = useMutation({
    mutationFn: ({ level, form }: { level: number; form: RowForm }) =>
      api.config.retention.update(level, {
        severity_level: level,
        retention_days: Number(form.retentionDays) || 0,
        hard_delete: form.hardDelete,
      }),
    onSuccess: (_data, { level }) => {
      void queryClient.invalidateQueries({ queryKey: ['admin', 'retention'] })
      setErrors((e) => ({ ...e, [level]: '' }))
    },
    onError: (err, { level }) =>
      setErrors((e) => ({ ...e, [level]: err instanceof ApiError ? err.message : 'Failed to save' })),
  })

  function formFor(level: number): RowForm {
    return forms[level] ?? toForm(byLevel.get(level))
  }

  function setFormFor(level: number, form: RowForm) {
    setForms((f) => ({ ...f, [level]: form }))
  }

  if (retention.isLoading) {
    return (
      <Section title="Data retention">
        <Skeleton className="h-32 w-full" />
      </Section>
    )
  }

  return (
    <Section title="Data retention">
      <p className="text-sm text-muted-foreground">
        Per-severity-level retention policy for closed SEVs. A retention of 0 days means retain forever. Hard delete
        purges the record permanently on expiry; otherwise it's archived (soft-deleted).
      </p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-xs text-muted-foreground">
              <th className="py-2 pr-3">Severity</th>
              <th className="py-2 pr-3">Retention (days)</th>
              <th className="py-2 pr-3">On expiry</th>
              <th className="py-2 pr-3">Last updated</th>
              <th className="py-2" />
            </tr>
          </thead>
          <tbody>
            {SEVERITY_LEVELS.map((level) => {
              const cfg = byLevel.get(level)
              const form = formFor(level)
              return (
                <tr key={level} className="border-b border-border align-top">
                  <td className="py-2 pr-3 font-medium">SEV-{level}</td>
                  <td className="py-2 pr-3">
                    <Input
                      type="number"
                      min={0}
                      aria-label={`Retention days for SEV-${level}`}
                      value={form.retentionDays}
                      onChange={(e) => setFormFor(level, { ...form, retentionDays: e.target.value })}
                      className="w-28"
                    />
                  </td>
                  <td className="py-2 pr-3">
                    <label className="flex items-center gap-2">
                      <Checkbox
                        aria-label={`Hard delete for SEV-${level}`}
                        checked={form.hardDelete}
                        onChange={(e) => setFormFor(level, { ...form, hardDelete: e.target.checked })}
                      />
                      Hard delete
                    </label>
                  </td>
                  <td className="py-2 pr-3 text-muted-foreground">{cfg ? formatDateTime(cfg.updated_at) : 'Not set'}</td>
                  <td className="py-2 text-right">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => updateMutation.mutate({ level, form })}
                      disabled={updateMutation.isPending && updateMutation.variables?.level === level}
                    >
                      Save
                    </Button>
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
    </Section>
  )
}
