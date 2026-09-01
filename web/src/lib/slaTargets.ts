/** Shared shape for one severity level's SLA-target form row — used by both
 * ServiceSLAEditor.tsx (editing an existing service's SLAs) and
 * AdminServicesPage.tsx's "New service" form (setting them at creation
 * time), so the two stay in sync rather than drifting apart. */
export const SEVERITY_LEVELS = [1, 2, 3, 4]

export interface SLARowForm {
  mttd: string // minutes, empty string = unset
  mttm: string
  mttr: string
  rtpc: string
}

export function emptySLARowForm(): SLARowForm {
  return { mttd: '', mttm: '', mttr: '', rtpc: '' }
}

/** minutes (a form field's string value) → seconds, or undefined when the
 * field is blank (clears that metric's target — UpsertServiceSLA is a
 * full-replace, like UpdateRetentionConfigRequest, not a sparse patch). */
export function minutesToSeconds(v: string): number | undefined {
  const n = Number(v)
  return v.trim() !== '' && n > 0 ? Math.round(n * 60) : undefined
}

/** True if at least one metric field in form has a value — used to decide
 * whether a severity level needs an UpsertServiceSLA call at all. */
export function slaRowFormHasAnyValue(form: SLARowForm): boolean {
  return (
    minutesToSeconds(form.mttd) !== undefined ||
    minutesToSeconds(form.mttm) !== undefined ||
    minutesToSeconds(form.mttr) !== undefined ||
    minutesToSeconds(form.rtpc) !== undefined
  )
}
