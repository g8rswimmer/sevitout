import { useState, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, Pencil } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'
import { Section } from '@/components/sev/Section'
import { ServiceChipEditor } from '@/components/sev/ServiceChipEditor'
import { recordToTagRows, tagRowsToRecord, TagRowsEditor, type TagRow } from '@/components/sev/TagRowsEditor'
import { DetectionFields, SnapshotPreview, type DetectionFieldsValue } from '@/components/sev/DetectionFields'
import {
  DETECTION_METHOD_LABELS,
  ROOT_CAUSE_CATEGORY_LABELS,
  type DetectionMethod,
  type RootCauseCategory,
  type SEVResponse,
} from '@/types/api'

const ROOT_CAUSE_CATEGORIES = Object.keys(ROOT_CAUSE_CATEGORY_LABELS) as RootCauseCategory[]
const ROOT_CAUSE_SELECT_OTHER = 'other'

interface FormState {
  description: string
  rootCauseCategory: string
  rootCauseDescription: string
  mitigation: string
  prevention: string
  businessImpact: string
  affectedServices: string[]
  detection: DetectionFieldsValue
  githubRepo: string
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
    detection: {
      detectionMethod: sev.detection_method ?? '',
      monitoringTool: sev.monitoring_tool ?? '',
      alertName: sev.alert_name ?? '',
      alertUrl: sev.alert_url ?? '',
      metricLink: sev.metric_link ?? '',
      snapshotUrl: sev.snapshot_url ?? '',
    },
    githubRepo: sev.github_repo ?? '',
    rightPeoplePresent: sev.right_people_present ?? false,
    rightPeopleNotes: sev.right_people_notes ?? '',
    tags: recordToTagRows(sev.tags),
    sensitive: sev.sensitive ?? false,
    aiDisabled: sev.ai_disabled ?? false,
  }
}

/** deployment/configuration/hardware/dependency, "" for not-yet-set (shows
 * the neutral "— Select —" option), or "other" for any other non-empty
 * value — mirrors DetectionFields' monitoringSelectValue. */
function rootCauseSelectValue(category: string): string {
  if (category === '') return ''
  if ((ROOT_CAUSE_CATEGORIES as string[]).includes(category)) return category
  return ROOT_CAUSE_SELECT_OTHER
}

export function DetailsPanel({ sev, canEdit }: { sev: SEVResponse; canEdit: boolean }) {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [form, setForm] = useState<FormState>(() => toFormState(sev))
  const [error, setError] = useState<string | null>(null)
  // Tracks an explicit "Other" pick so a freshly-cleared category (for
  // typing a custom one) doesn't fall back to "— Select —" just because the
  // text is momentarily empty — same issue DetectionFields' otherSelected
  // guards against for monitoring tool.
  const [otherCategorySelected, setOtherCategorySelected] = useState(
    () => rootCauseSelectValue(sev.root_cause_category ?? '') === ROOT_CAUSE_SELECT_OTHER,
  )

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
        alert_name: form.detection.alertName,
        monitoring_tool: form.detection.monitoringTool,
        alert_url: form.detection.alertUrl,
        metric_link: form.detection.metricLink,
        snapshot_url: form.detection.snapshotUrl,
        github_repo: form.githubRepo,
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
    setOtherCategorySelected(rootCauseSelectValue(sev.root_cause_category ?? '') === ROOT_CAUSE_SELECT_OTHER)
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
          <Field label="Description" htmlFor="dp-description">
            <Textarea
              id="dp-description"
              value={form.description}
              onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
            />
          </Field>
          <Field label="Root cause category" htmlFor="dp-root-cause-category">
            <Select
              id="dp-root-cause-category"
              value={otherCategorySelected ? ROOT_CAUSE_SELECT_OTHER : rootCauseSelectValue(form.rootCauseCategory)}
              onChange={(e) => {
                const v = e.target.value
                setOtherCategorySelected(v === ROOT_CAUSE_SELECT_OTHER)
                setForm((f) => ({ ...f, rootCauseCategory: v === ROOT_CAUSE_SELECT_OTHER ? '' : v }))
              }}
            >
              <option value="">— Select —</option>
              {ROOT_CAUSE_CATEGORIES.map((c) => (
                <option key={c} value={c}>
                  {ROOT_CAUSE_CATEGORY_LABELS[c]}
                </option>
              ))}
              <option value={ROOT_CAUSE_SELECT_OTHER}>Other…</option>
            </Select>
            {otherCategorySelected && (
              <Input
                aria-label="Custom root cause category"
                placeholder="Name the category"
                value={form.rootCauseCategory}
                onChange={(e) => setForm((f) => ({ ...f, rootCauseCategory: e.target.value }))}
              />
            )}
          </Field>
          <Field label="Business impact" htmlFor="dp-business-impact">
            <Input
              id="dp-business-impact"
              value={form.businessImpact}
              onChange={(e) => setForm((f) => ({ ...f, businessImpact: e.target.value }))}
            />
          </Field>
          <Field label="Root cause description" htmlFor="dp-root-cause-description">
            <Textarea
              id="dp-root-cause-description"
              value={form.rootCauseDescription}
              onChange={(e) => setForm((f) => ({ ...f, rootCauseDescription: e.target.value }))}
            />
          </Field>
          <Field label="Mitigation" htmlFor="dp-mitigation">
            <Textarea
              id="dp-mitigation"
              value={form.mitigation}
              onChange={(e) => setForm((f) => ({ ...f, mitigation: e.target.value }))}
            />
          </Field>
          <Field label="Prevention / action items" htmlFor="dp-prevention">
            <Textarea
              id="dp-prevention"
              value={form.prevention}
              onChange={(e) => setForm((f) => ({ ...f, prevention: e.target.value }))}
            />
          </Field>
          <Field label="Affected services">
            <ServiceChipEditor
              services={form.affectedServices}
              onChange={(services) => setForm((f) => ({ ...f, affectedServices: services }))}
            />
          </Field>
          <DetectionFields value={form.detection} onChange={(detection) => setForm((f) => ({ ...f, detection }))} />
          <Field label="Repository" htmlFor="dp-github-repo">
            <Input
              id="dp-github-repo"
              placeholder="owner/repo"
              value={form.githubRepo}
              onChange={(e) => setForm((f) => ({ ...f, githubRepo: e.target.value }))}
            />
            <p className="text-xs text-muted-foreground">
              Pre-fills owner/repo when creating a GitHub issue from Linked Tasks.
            </p>
          </Field>
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
          <Field label="Notes on who responded" htmlFor="dp-right-people-notes">
            <Textarea
              id="dp-right-people-notes"
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
          <ReadField
            label="Root cause category"
            value={
              sev.root_cause_category &&
              (ROOT_CAUSE_CATEGORY_LABELS[sev.root_cause_category as RootCauseCategory] ?? sev.root_cause_category)
            }
          />
          <ReadField label="Business impact" value={sev.business_impact} />
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
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <ReadField
              label="Detection method"
              value={sev.detection_method && DETECTION_METHOD_LABELS[sev.detection_method as DetectionMethod]}
            />
            <ReadField label="Monitoring tool" value={sev.monitoring_tool} />
          </div>
          <ReadField label="Alert name" value={sev.alert_name} />
          <ReadLinkField label="Alert link" href={sev.alert_url} />
          {(sev.metric_link || sev.snapshot_url) && (
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <ReadLinkField label="Metric / dashboard link" href={sev.metric_link} />
              <ReadLinkField label="Snapshot link" href={sev.snapshot_url} />
            </div>
          )}
          {sev.snapshot_url && <SnapshotPreview url={sev.snapshot_url} />}
          <div>
            <div className="text-xs text-muted-foreground">Repository</div>
            {sev.github_repo ? (
              <a
                href={`https://github.com/${sev.github_repo}`}
                target="_blank"
                rel="noreferrer"
                className="flex items-center gap-1 text-primary hover:underline"
              >
                {sev.github_repo}
                <ExternalLink className="h-3 w-3 shrink-0" aria-hidden />
              </a>
            ) : (
              <span className="text-muted-foreground">—</span>
            )}
          </div>
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

function Field({ label, htmlFor, children }: { label: string; htmlFor?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
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
