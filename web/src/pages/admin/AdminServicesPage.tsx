import { Fragment, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Gauge, Pencil, Plus, Trash2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { recordToTagRows, tagRowsToRecord, TagRowsEditor, type TagRow } from '@/components/sev/TagRowsEditor'
import { ColumnHeader, ServiceSLAEditor } from '@/components/admin/ServiceSLAEditor'
import { emptySLARowForm, hoursToSeconds, SEVERITY_LEVELS, slaRowFormHasAnyValue, type SLARowForm } from '@/lib/slaTargets'
import { METRIC_DEFINITIONS } from '@/lib/metricDefinitions'
import type { ServiceResponse } from '@/types/api'

interface ServiceForm {
  id: string
  name: string
  description: string
  owningTeam: string
  pagerdutyServiceId: string
  tags: TagRow[]
  active: boolean
}

function toForm(svc?: ServiceResponse): ServiceForm {
  return {
    id: svc?.id ?? '',
    name: svc?.name ?? '',
    description: svc?.description ?? '',
    owningTeam: svc?.owning_team ?? '',
    pagerdutyServiceId: svc?.pagerduty_service_id ?? '',
    tags: recordToTagRows(svc?.tags),
    active: svc?.active ?? true,
  }
}

export function AdminServicesPage() {
  const queryClient = useQueryClient()
  const services = useQuery({ queryKey: ['admin', 'services'], queryFn: api.services.list })

  const [creating, setCreating] = useState(false)
  const [createForm, setCreateForm] = useState<ServiceForm>(() => toForm())
  const [createError, setCreateError] = useState<string | null>(null)
  // Optional SLA targets set alongside creation, so an admin doesn't need a
  // separate "Manage SLAs" step right after adding a service. Same
  // per-severity-level shape ServiceSLAEditor.tsx edits after the fact.
  const [createSlaForms, setCreateSlaForms] = useState<Record<number, SLARowForm>>({})

  const [editingId, setEditingId] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<ServiceForm>(() => toForm())
  const [editError, setEditError] = useState<string | null>(null)

  // Roadmap Phase 12: at most one service's SLA editor is expanded at a
  // time, independent of editingId (name/description edit and SLA editing
  // are separate concerns, not mutually exclusive).
  const [slaServiceId, setSlaServiceId] = useState<string | null>(null)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'services'] })

  function createSlaFormFor(level: number): SLARowForm {
    return createSlaForms[level] ?? emptySLARowForm()
  }

  function setCreateSlaFormFor(level: number, form: SLARowForm) {
    setCreateSlaForms((f) => ({ ...f, [level]: form }))
  }

  const createMutation = useMutation({
    mutationFn: async () => {
      const svc = await api.services.create({
        id: createForm.id,
        name: createForm.name,
        description: createForm.description || undefined,
        owning_team: createForm.owningTeam || undefined,
        pagerduty_service_id: createForm.pagerdutyServiceId || undefined,
        tags: tagRowsToRecord(createForm.tags),
      })
      // Invalidate as soon as the service itself exists, before the SLA
      // upserts below — if one of those fails, the service is still visible
      // in the list (and createError explains the partial failure) instead
      // of silently existing but hidden, which would make "Create" look
      // like it failed outright and invite a confusing duplicate-ID retry.
      invalidate()
      // Only severity levels where at least one target was actually filled
      // in get an UpsertServiceSLA call — an all-blank row means "no SLA at
      // this severity level," not a zeroed-out row worth persisting.
      const levelsWithSla = SEVERITY_LEVELS.filter((level) => slaRowFormHasAnyValue(createSlaFormFor(level)))
      await Promise.all(
        levelsWithSla.map((level) => {
          const form = createSlaFormFor(level)
          return api.config.serviceSLA.upsert(svc.id, level, {
            severity_level: level,
            mttd_target_seconds: hoursToSeconds(form.mttd),
            mttm_target_seconds: hoursToSeconds(form.mttm),
            mttr_target_seconds: hoursToSeconds(form.mttr),
            rtpc_target_seconds: hoursToSeconds(form.rtpc),
          })
        }),
      )
      return svc
    },
    onSuccess: () => {
      invalidate()
      setCreating(false)
      setCreateForm(toForm())
      setCreateSlaForms({})
      setCreateError(null)
    },
    onError: (err) => setCreateError(err instanceof ApiError ? err.message : 'Failed to create service'),
  })

  const updateMutation = useMutation({
    mutationFn: (id: string) =>
      api.services.update(id, {
        name: editForm.name,
        description: editForm.description,
        owning_team: editForm.owningTeam,
        pagerduty_service_id: editForm.pagerdutyServiceId,
        tags: tagRowsToRecord(editForm.tags) ?? {},
        active: editForm.active,
      }),
    onSuccess: () => {
      invalidate()
      setEditingId(null)
      setEditError(null)
    },
    onError: (err) => setEditError(err instanceof ApiError ? err.message : 'Failed to save service'),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.services.delete(id),
    onSuccess: invalidate,
  })

  function startEdit(svc: ServiceResponse) {
    setEditingId(svc.id)
    setEditForm(toForm(svc))
    setEditError(null)
  }

  const list = services.data?.services ?? []

  return (
    <div className="flex flex-col gap-4">
      <Section
        title="Service registry"
        action={
          !creating && (
            <Button size="sm" onClick={() => setCreating(true)}>
              <Plus className="h-3.5 w-3.5" /> New service
            </Button>
          )
        }
      >
        {creating && (
          <div className="flex flex-col gap-3 rounded-md border border-border p-3">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="ID (slug)" htmlFor="svc-new-id">
                <Input
                  id="svc-new-id"
                  placeholder="checkout"
                  value={createForm.id}
                  onChange={(e) => setCreateForm((f) => ({ ...f, id: e.target.value }))}
                />
              </Field>
              <Field label="Name" htmlFor="svc-new-name">
                <Input
                  id="svc-new-name"
                  value={createForm.name}
                  onChange={(e) => setCreateForm((f) => ({ ...f, name: e.target.value }))}
                />
              </Field>
            </div>
            <Field label="Description" htmlFor="svc-new-description">
              <Input
                id="svc-new-description"
                value={createForm.description}
                onChange={(e) => setCreateForm((f) => ({ ...f, description: e.target.value }))}
              />
            </Field>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <Field label="Owning team" htmlFor="svc-new-team">
                <Input
                  id="svc-new-team"
                  value={createForm.owningTeam}
                  onChange={(e) => setCreateForm((f) => ({ ...f, owningTeam: e.target.value }))}
                />
              </Field>
              <Field label="PagerDuty service ID" htmlFor="svc-new-pd">
                <Input
                  id="svc-new-pd"
                  value={createForm.pagerdutyServiceId}
                  onChange={(e) => setCreateForm((f) => ({ ...f, pagerdutyServiceId: e.target.value }))}
                />
              </Field>
            </div>
            <Field label="Tags">
              <TagRowsEditor rows={createForm.tags} onChange={(tags) => setCreateForm((f) => ({ ...f, tags }))} />
            </Field>
            <div className="flex flex-col gap-1.5">
              <Label>SLA targets (optional)</Label>
              <p className="text-xs text-muted-foreground">
                Target response times per severity level, in whole hours. Leave a field blank to skip it — targets can
                always be added or changed later from this service's "Manage SLAs" action.
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
                    </tr>
                  </thead>
                  <tbody>
                    {SEVERITY_LEVELS.map((level) => {
                      const form = createSlaFormFor(level)
                      return (
                        <tr key={level} className="border-b border-border last:border-0">
                          <td className="py-2 pr-3 font-medium">SEV-{level}</td>
                          <td className="py-2 pr-3">
                            <Input
                              type="number"
                              min={0}
                              aria-label={`New service MTTD target hours for SEV-${level}`}
                              value={form.mttd}
                              onChange={(e) => setCreateSlaFormFor(level, { ...form, mttd: e.target.value })}
                              className="w-24"
                            />
                          </td>
                          <td className="py-2 pr-3">
                            <Input
                              type="number"
                              min={0}
                              aria-label={`New service MTTM target hours for SEV-${level}`}
                              value={form.mttm}
                              onChange={(e) => setCreateSlaFormFor(level, { ...form, mttm: e.target.value })}
                              className="w-24"
                            />
                          </td>
                          <td className="py-2 pr-3">
                            <Input
                              type="number"
                              min={0}
                              aria-label={`New service MTTR target hours for SEV-${level}`}
                              value={form.mttr}
                              onChange={(e) => setCreateSlaFormFor(level, { ...form, mttr: e.target.value })}
                              className="w-24"
                            />
                          </td>
                          <td className="py-2 pr-3">
                            <Input
                              type="number"
                              min={0}
                              aria-label={`New service RTPC target hours for SEV-${level}`}
                              value={form.rtpc}
                              onChange={(e) => setCreateSlaFormFor(level, { ...form, rtpc: e.target.value })}
                              className="w-24"
                            />
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </div>
            {createError && (
              <p role="alert" className="text-sm text-destructive">
                {createError}
              </p>
            )}
            <div className="flex gap-2">
              <Button
                size="sm"
                onClick={() => createMutation.mutate()}
                disabled={createMutation.isPending || !createForm.id || !createForm.name}
              >
                {createMutation.isPending ? 'Creating…' : 'Create'}
              </Button>
              <Button size="sm" variant="outline" onClick={() => setCreating(false)}>
                Cancel
              </Button>
            </div>
          </div>
        )}

        {services.isLoading ? (
          <Skeleton className="h-32 w-full" />
        ) : list.length === 0 ? (
          <p className="text-sm text-muted-foreground">No services registered yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="py-2 pr-3">ID</th>
                  <th className="py-2 pr-3">Name</th>
                  <th className="py-2 pr-3">Owning team</th>
                  <th className="py-2 pr-3">PagerDuty</th>
                  <th className="py-2 pr-3">Status</th>
                  <th className="py-2 pr-3">Tags</th>
                  <th className="py-2" />
                </tr>
              </thead>
              <tbody>
                {list.map((svc) => (
                  <Fragment key={svc.id}>
                  {editingId === svc.id ? (
                    <tr className="border-b border-border align-top">
                      <td colSpan={7} className="py-3">
                        <div className="flex flex-col gap-3">
                          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                            <Field label="Name" htmlFor={`svc-${svc.id}-name`}>
                              <Input
                                id={`svc-${svc.id}-name`}
                                value={editForm.name}
                                onChange={(e) => setEditForm((f) => ({ ...f, name: e.target.value }))}
                              />
                            </Field>
                            <Field label="Owning team" htmlFor={`svc-${svc.id}-team`}>
                              <Input
                                id={`svc-${svc.id}-team`}
                                value={editForm.owningTeam}
                                onChange={(e) => setEditForm((f) => ({ ...f, owningTeam: e.target.value }))}
                              />
                            </Field>
                          </div>
                          <Field label="Description" htmlFor={`svc-${svc.id}-description`}>
                            <Input
                              id={`svc-${svc.id}-description`}
                              value={editForm.description}
                              onChange={(e) => setEditForm((f) => ({ ...f, description: e.target.value }))}
                            />
                          </Field>
                          <Field label="PagerDuty service ID" htmlFor={`svc-${svc.id}-pd`}>
                            <Input
                              id={`svc-${svc.id}-pd`}
                              value={editForm.pagerdutyServiceId}
                              onChange={(e) => setEditForm((f) => ({ ...f, pagerdutyServiceId: e.target.value }))}
                            />
                          </Field>
                          <Field label="Tags">
                            <TagRowsEditor rows={editForm.tags} onChange={(tags) => setEditForm((f) => ({ ...f, tags }))} />
                          </Field>
                          <label className="flex items-center gap-2 text-sm">
                            <Checkbox
                              checked={editForm.active}
                              onChange={(e) => setEditForm((f) => ({ ...f, active: e.target.checked }))}
                            />
                            Active
                          </label>
                          {editError && (
                            <p role="alert" className="text-sm text-destructive">
                              {editError}
                            </p>
                          )}
                          <div className="flex gap-2">
                            <Button
                              size="sm"
                              onClick={() => updateMutation.mutate(svc.id)}
                              disabled={updateMutation.isPending}
                            >
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
                    <tr className="border-b border-border">
                      <td className="py-2 pr-3 font-mono text-xs">{svc.id}</td>
                      <td className="py-2 pr-3">{svc.name}</td>
                      <td className="py-2 pr-3 text-muted-foreground">{svc.owning_team || '—'}</td>
                      <td className="py-2 pr-3 text-muted-foreground">{svc.pagerduty_service_id || '—'}</td>
                      <td className="py-2 pr-3">
                        <Badge variant={svc.active === false ? 'outline' : 'secondary'}>
                          {svc.active === false ? 'Inactive' : 'Active'}
                        </Badge>
                      </td>
                      <td className="py-2 pr-3">
                        <div className="flex flex-wrap gap-1">
                          {Object.entries(svc.tags ?? {}).map(([k, v]) => (
                            <Badge key={k} variant="outline">
                              {k}={v}
                            </Badge>
                          ))}
                        </div>
                      </td>
                      <td className="py-2 text-right">
                        <div className="flex justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={`Manage SLAs for ${svc.name}`}
                            onClick={() => setSlaServiceId(slaServiceId === svc.id ? null : svc.id)}
                          >
                            <Gauge className="h-3.5 w-3.5" />
                          </Button>
                          <Button variant="ghost" size="icon" aria-label={`Edit ${svc.name}`} onClick={() => startEdit(svc)}>
                            <Pencil className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={`Delete ${svc.name}`}
                            onClick={() => deleteMutation.mutate(svc.id)}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  )}
                  {slaServiceId === svc.id && (
                    <tr className="border-b border-border">
                      <td colSpan={7} className="py-3">
                        <ServiceSLAEditor serviceId={svc.id} />
                      </td>
                    </tr>
                  )}
                  </Fragment>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Section>
    </div>
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
