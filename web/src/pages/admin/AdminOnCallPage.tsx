import { useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { DateTimeField } from '@/components/sev/DateTimeField'
import { formatDateTime, toDateTimeLocalValue } from '@/lib/format'
import type { OnCallRotationResponse } from '@/types/api'

interface RotationForm {
  name: string
  serviceId: string
  pagerdutyScheduleId: string
  manualUserId: string
  manualDisplayName: string
  overrideStart: string
  overrideEnd: string
}

function toForm(r?: OnCallRotationResponse): RotationForm {
  return {
    name: r?.name ?? '',
    serviceId: r?.service_id ?? '',
    pagerdutyScheduleId: r?.pagerduty_schedule_id ?? '',
    manualUserId: r?.manual_user_id ?? '',
    manualDisplayName: r?.manual_display_name ?? '',
    overrideStart: toDateTimeLocalValue(r?.override_start),
    overrideEnd: toDateTimeLocalValue(r?.override_end),
  }
}

function formToRequest(f: RotationForm) {
  return {
    name: f.name,
    service_id: f.serviceId || undefined,
    pagerduty_schedule_id: f.pagerdutyScheduleId || undefined,
    manual_user_id: f.manualUserId || undefined,
    manual_display_name: f.manualDisplayName || undefined,
    override_start: f.overrideStart ? new Date(f.overrideStart).toISOString() : undefined,
    override_end: f.overrideEnd ? new Date(f.overrideEnd).toISOString() : undefined,
  }
}

export function AdminOnCallPage() {
  const queryClient = useQueryClient()
  const rotations = useQuery({ queryKey: ['admin', 'oncall'], queryFn: api.config.oncall.list })
  const services = useQuery({ queryKey: ['services'], queryFn: api.services.list })

  const [creating, setCreating] = useState(false)
  const [createForm, setCreateForm] = useState<RotationForm>(() => toForm())
  const [createError, setCreateError] = useState<string | null>(null)

  const [editingId, setEditingId] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<RotationForm>(() => toForm())
  const [editError, setEditError] = useState<string | null>(null)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'oncall'] })

  const createMutation = useMutation({
    mutationFn: () => api.config.oncall.create(formToRequest(createForm)),
    onSuccess: () => {
      invalidate()
      setCreating(false)
      setCreateForm(toForm())
      setCreateError(null)
    },
    onError: (err) => setCreateError(err instanceof ApiError ? err.message : 'Failed to create rotation'),
  })

  const updateMutation = useMutation({
    mutationFn: (id: string) => api.config.oncall.update(id, formToRequest(editForm)),
    onSuccess: () => {
      invalidate()
      setEditingId(null)
      setEditError(null)
    },
    onError: (err) => setEditError(err instanceof ApiError ? err.message : 'Failed to save rotation'),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.config.oncall.delete(id),
    onSuccess: invalidate,
  })

  function startEdit(r: OnCallRotationResponse) {
    setEditingId(r.id)
    setEditForm(toForm(r))
    setEditError(null)
  }

  const serviceOptions = services.data?.services ?? []
  const list = rotations.data?.rotations ?? []

  function serviceLabel(id?: string): string {
    if (!id) return '—'
    return serviceOptions.find((s) => s.id === id)?.name ?? id
  }

  function renderForm(form: RotationForm, setForm: (f: RotationForm) => void, idPrefix: string) {
    return (
      <div className="flex flex-col gap-3">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Name" htmlFor={`${idPrefix}-name`}>
            <Input id={`${idPrefix}-name`} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </Field>
          <Field label="Service" htmlFor={`${idPrefix}-service`}>
            <Select
              id={`${idPrefix}-service`}
              value={form.serviceId}
              onChange={(e) => setForm({ ...form, serviceId: e.target.value })}
            >
              <option value="">— None —</option>
              {serviceOptions.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </Select>
          </Field>
        </div>
        <Field label="PagerDuty schedule ID" htmlFor={`${idPrefix}-pd`}>
          <Input
            id={`${idPrefix}-pd`}
            placeholder="Leave blank if this is a manual-only rotation"
            value={form.pagerdutyScheduleId}
            onChange={(e) => setForm({ ...form, pagerdutyScheduleId: e.target.value })}
          />
        </Field>
        <p className="text-xs text-muted-foreground">
          Manual override — for a planned change during a specific window; leave all four blank for a normal
          PagerDuty-backed rotation.
        </p>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Manual user ID" htmlFor={`${idPrefix}-manual-user`}>
            <Input
              id={`${idPrefix}-manual-user`}
              value={form.manualUserId}
              onChange={(e) => setForm({ ...form, manualUserId: e.target.value })}
            />
          </Field>
          <Field label="Manual display name" htmlFor={`${idPrefix}-manual-name`}>
            <Input
              id={`${idPrefix}-manual-name`}
              value={form.manualDisplayName}
              onChange={(e) => setForm({ ...form, manualDisplayName: e.target.value })}
            />
          </Field>
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <DateTimeField
            id={`${idPrefix}-start`}
            label="Override start"
            value={form.overrideStart}
            onChange={(v) => setForm({ ...form, overrideStart: v })}
          />
          <DateTimeField
            id={`${idPrefix}-end`}
            label="Override end"
            value={form.overrideEnd}
            onChange={(v) => setForm({ ...form, overrideEnd: v })}
          />
        </div>
      </div>
    )
  }

  return (
    <Section
      title="On-call rotations"
      action={
        !creating && (
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="h-3.5 w-3.5" /> New rotation
          </Button>
        )
      }
    >
      {creating && (
        <div className="flex flex-col gap-3 rounded-md border border-border p-3">
          {renderForm(createForm, setCreateForm, 'oncall-new')}
          {createError && (
            <p role="alert" className="text-sm text-destructive">
              {createError}
            </p>
          )}
          <div className="flex gap-2">
            <Button size="sm" onClick={() => createMutation.mutate()} disabled={createMutation.isPending || !createForm.name}>
              {createMutation.isPending ? 'Creating…' : 'Create'}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setCreating(false)}>
              Cancel
            </Button>
          </div>
        </div>
      )}

      {rotations.isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : list.length === 0 ? (
        <p className="text-sm text-muted-foreground">No on-call rotations configured yet.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3">Name</th>
                <th className="py-2 pr-3">Service</th>
                <th className="py-2 pr-3">PagerDuty schedule</th>
                <th className="py-2 pr-3">Manual override</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {list.map((r) =>
                editingId === r.id ? (
                  <tr key={r.id} className="border-b border-border align-top">
                    <td colSpan={5} className="py-3">
                      <div className="flex flex-col gap-3">
                        {renderForm(editForm, setEditForm, `oncall-${r.id}`)}
                        {editError && (
                          <p role="alert" className="text-sm text-destructive">
                            {editError}
                          </p>
                        )}
                        <div className="flex gap-2">
                          <Button size="sm" onClick={() => updateMutation.mutate(r.id)} disabled={updateMutation.isPending}>
                            {updateMutation.isPending ? 'Saving…' : 'Save'}
                          </Button>
                          <Button size="sm" variant="outline" onClick={() => setEditingId(null)}>
                            Cancel
                          </Button>
                        </div>
                      </div>
                    </td>
                  </tr>
                ) : (
                  <tr key={r.id} className="border-b border-border">
                    <td className="py-2 pr-3">{r.name}</td>
                    <td className="py-2 pr-3 text-muted-foreground">{serviceLabel(r.service_id)}</td>
                    <td className="py-2 pr-3 text-muted-foreground">{r.pagerduty_schedule_id || '—'}</td>
                    <td className="py-2 pr-3 text-muted-foreground">
                      {r.manual_display_name ? (
                        <>
                          {r.manual_display_name}
                          {r.override_start && ` (${formatDateTime(r.override_start)} – ${formatDateTime(r.override_end)})`}
                        </>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className="py-2 text-right">
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" size="icon" aria-label={`Edit ${r.name}`} onClick={() => startEdit(r)}>
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`Delete ${r.name}`}
                          onClick={() => deleteMutation.mutate(r.id)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  )
}

function Field({ label, htmlFor, children }: { label: string; htmlFor?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
    </div>
  )
}
