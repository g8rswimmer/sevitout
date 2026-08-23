import { useState, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, Pencil } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'
import { Section } from '@/components/sev/Section'
import { ServiceChipEditor } from '@/components/sev/ServiceChipEditor'
import { recordToTagRows, tagRowsToRecord, TagRowsEditor, type TagRow } from '@/components/sev/TagRowsEditor'
import { DetectionFields, SnapshotPreview, type DetectionFieldsValue } from '@/components/sev/DetectionFields'
import { DETECTION_METHOD_LABELS, type DetectionMethod, type SEVResponse } from '@/types/api'

interface FormState {
  description: string
  rootCauseCategory: string
  rootCauseDescription: string
  mitigation: string
  prevention: string
  businessImpact: string
  affectedServices: string[]
  alertName: string
  detection: DetectionFieldsValue
  rightPeoplePresent: boolean
  rightPeopleNotes: string
  tags: TagRow[]
  sensitive: boolean
  aiDisabled: boolean
}

function toFormState(sev: SEVResponse): FormState {
  return {
    description: sev.description ?? '',
    rootCauseCategory: sev.root_cause_category ?? '',
    rootCauseDescription: sev.root_cause_description ?? '',
    mitigation: sev.mitigation ?? '',
    prevention: sev.prevention ?? '',
    businessImpact: sev.business_impact ?? '',
    affectedServices: sev.affected_services ?? [],
    alertName: sev.alert_name ?? '',
    detection: {
      detectionMethod: sev.detection_method ?? '',
      monitoringTool: sev.monitoring_tool ?? '',
      alertUrl: sev.alert_url ?? '',
      metricLink: sev.metric_link ?? '',
      snapshotUrl: sev.snapshot_url ?? '',
    },
    rightPeoplePresent: sev.right_people_present ?? false,
    rightPeopleNotes: sev.right_people_notes ?? '',
    tags: recordToTagRows(sev.tags),
    sensitive: sev.sensitive ?? false,
    aiDisabled: sev.ai_disabled ?? false,
  }
}

export function DetailsPanel({ sev, canEdit }: { sev: SEVResponse; canEdit: boolean }) {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [form, setForm] = useState<FormState>(() => toFormState(sev))
  const [error, setError] = useState<string | null>(null)

  const mutation = useMutation({
    mutationFn: () =>
      api.sevs.update(sev.id, {
        description: form.description,
        root_cause_category: form.rootCauseCategory,
        root_cause_description: form.rootCauseDescription,
        mitigation: form.mitigation,
        prevention: form.prevention,
        business_impact: form.businessImpact,
        affected_services: form.affectedServices,
        detection_method: form.detection.detectionMethod,
        alert_name: form.alertName,
        monitoring_tool: form.detection.monitoringTool,
        alert_url: form.detection.alertUrl,
        metric_link: form.detection.metricLink,
        snapshot_url: form.detection.snapshotUrl,
        right_people_present: form.rightPeoplePresent,
        right_people_notes: form.rightPeopleNotes,
        tags: tagRowsToRecord(form.tags) ?? {},
        sensitive: form.sensitive,
        ai_disabled: form.aiDisabled,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['sevs', 'detail', sev.id] })
      setEditing(false)
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to save'),
  })

  function startEditing() {
    setForm(toFormState(sev))
    setError(null)
    setEditing(true)
  }

  return (
    <Section
      title="Details"
      action={
        canEdit &&
        !editing && (
          <Button variant="ghost" size="sm" onClick={startEditing}>
            <Pencil className="h-3.5 w-3.5" /> Edit
          </Button>
        )
      }
    >
      {editing ? (
        <div className="flex flex-col gap-4">
          <Field label="Description">
            <Textarea value={form.description} onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))} />
          </Field>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label="Root cause category">
              <Input
                value={form.rootCauseCategory}
                placeholder="deployment, configuration, hardware, dependency…"
                onChange={(e) => setForm((f) => ({ ...f, rootCauseCategory: e.target.value }))}
              />
            </Field>
            <Field label="Business impact">
              <Input value={form.businessImpact} onChange={(e) => setForm((f) => ({ ...f, businessImpact: e.target.value }))} />
            </Field>
          </div>
          <Field label="Root cause description">
            <Textarea
              value={form.rootCauseDescription}
              onChange={(e) => setForm((f) => ({ ...f, rootCauseDescription: e.target.value }))}
            />
          </Field>
          <Field label="Mitigation">
            <Textarea value={form.mitigation} onChange={(e) => setForm((f) => ({ ...f, mitigation: e.target.value }))} />
          </Field>
          <Field label="Prevention / action items">
            <Textarea value={form.prevention} onChange={(e) => setForm((f) => ({ ...f, prevention: e.target.value }))} />
          </Field>
          <Field label="Affected services">
            <ServiceChipEditor
              services={form.affectedServices}
              onChange={(services) => setForm((f) => ({ ...f, affectedServices: services }))}
            />
          </Field>
          <Field label="Alert name">
            <Input value={form.alertName} onChange={(e) => setForm((f) => ({ ...f, alertName: e.target.value }))} />
          </Field>
          <DetectionFields value={form.detection} onChange={(detection) => setForm((f) => ({ ...f, detection }))} />
          <Field label="Tags">
            <TagRowsEditor rows={form.tags} onChange={(tags) => setForm((f) => ({ ...f, tags }))} />
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={form.rightPeoplePresent}
              onChange={(e) => setForm((f) => ({ ...f, rightPeoplePresent: e.target.checked }))}
            />
            Right people were in the room
          </label>
          <Field label="Notes on who responded">
            <Textarea
              value={form.rightPeopleNotes}
              onChange={(e) => setForm((f) => ({ ...f, rightPeopleNotes: e.target.value }))}
            />
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={form.sensitive} onChange={(e) => setForm((f) => ({ ...f, sensitive: e.target.checked }))} />
            Sensitive
          </label>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={form.aiDisabled} onChange={(e) => setForm((f) => ({ ...f, aiDisabled: e.target.checked }))} />
            AI dispatch disabled
          </label>

          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div className="flex gap-2">
            <Button size="sm" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
              {mutation.isPending ? 'Saving…' : 'Save'}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setEditing(false)}>
              Cancel
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4 text-sm">
          <ReadField label="Description" value={sev.description} />
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <ReadField label="Root cause category" value={sev.root_cause_category} />
            <ReadField label="Business impact" value={sev.business_impact} />
          </div>
          <ReadField label="Root cause description" value={sev.root_cause_description} />
          <ReadField label="Mitigation" value={sev.mitigation} />
          <ReadField label="Prevention / action items" value={sev.prevention} />
          <div>
            <div className="mb-1 text-xs text-muted-foreground">Affected services</div>
            <div className="flex flex-wrap gap-1.5">
              {(sev.affected_services ?? []).length > 0 ? (
                sev.affected_services!.map((s) => (
                  <Badge key={s} variant="secondary">
                    {s}
                  </Badge>
                ))
              ) : (
                <span className="text-muted-foreground">—</span>
              )}
            </div>
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <ReadField
              label="Detection method"
              value={sev.detection_method && DETECTION_METHOD_LABELS[sev.detection_method as DetectionMethod]}
            />
            <ReadField label="Alert name" value={sev.alert_name} />
            <ReadField label="Monitoring tool" value={sev.monitoring_tool} />
          </div>
          {(sev.alert_url || sev.metric_link || sev.snapshot_url) && (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
              <ReadLinkField label="Alert link" href={sev.alert_url} />
              <ReadLinkField label="Metric / dashboard link" href={sev.metric_link} />
              <ReadLinkField label="Snapshot link" href={sev.snapshot_url} />
            </div>
          )}
          {sev.snapshot_url && <SnapshotPreview url={sev.snapshot_url} />}
          {(sev.tags && Object.keys(sev.tags).length > 0) && (
            <div>
              <div className="mb-1 text-xs text-muted-foreground">Tags</div>
              <div className="flex flex-wrap gap-1.5">
                {Object.entries(sev.tags).map(([k, v]) => (
                  <Badge key={k} variant="outline">
                    {k}={v}
                  </Badge>
                ))}
              </div>
            </div>
          )}
          <ReadField
            label="Right people in the room?"
            value={sev.right_people_present === undefined ? undefined : sev.right_people_present ? 'Yes' : 'No'}
          />
          <ReadField label="Notes" value={sev.right_people_notes} />
        </div>
      )}
    </Section>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      {children}
    </div>
  )
}

function ReadLinkField({ label, href }: { label: string; href?: string }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      {href ? (
        <a
          href={href}
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-1 truncate text-primary hover:underline"
        >
          <span className="truncate">{href}</span>
          <ExternalLink className="h-3 w-3 shrink-0" aria-hidden />
        </a>
      ) : (
        <span className="text-muted-foreground">—</span>
      )}
    </div>
  )
}

function ReadField({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div>{value || <span className="text-muted-foreground">—</span>}</div>
    </div>
  )
}
