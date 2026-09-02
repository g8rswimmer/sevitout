/** The 4 severity levels a service can have per-level config for — used by
 * ServiceSLAEditor.tsx (editing an existing service's SLAs) and
 * AdminServicesPage.tsx's "New service" form (setting them at creation
 * time), so the two stay in sync rather than drifting apart. Despite living
 * in this SLA-specific file, the list itself is generic (just [1, 2, 3, 4]),
 * so LevelingCriteriaEditor.tsx (Roadmap Phase 14) reuses it too rather than
 * duplicating it — keep that in mind before folding this into anything
 * SLA-specific. */
export const SEVERITY_LEVELS = [1, 2, 3, 4]

export interface SLARowForm {
  mttd: string // hours, empty string = unset
  mttm: string
  mttr: string
  rtpc: string
}

export function emptySLARowForm(): SLARowForm {
  return { mttd: '', mttm: '', mttr: '', rtpc: '' }
}

/** hours (a form field's string value) → seconds, or undefined when the
 * field is blank (clears that metric's target — UpsertServiceSLA is a
 * full-replace, like UpdateRetentionConfigRequest, not a sparse patch).
 * Hour granularity, not minutes: a target like "48 hours" is far easier to
 * reason about than "2880 minutes," and no SLA in practice needs finer
 * precision than an hour. */
export function hoursToSeconds(v: string): number | undefined {
  const n = Number(v)
  return v.trim() !== '' && n > 0 ? Math.round(n * 3600) : undefined
}

/** True if at least one metric field in form has a value — used to decide
 * whether a severity level needs an UpsertServiceSLA call at all. */
export function slaRowFormHasAnyValue(form: SLARowForm): boolean {
  return (
    hoursToSeconds(form.mttd) !== undefined ||
    hoursToSeconds(form.mttm) !== undefined ||
    hoursToSeconds(form.mttr) !== undefined ||
    hoursToSeconds(form.rtpc) !== undefined
  )
}
