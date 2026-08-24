import { ROOT_CAUSE_CATEGORY_LABELS, type RootCauseCategory, type SEVResponse } from '@/types/api'
import { formatDateTime, formatDurationSeconds } from '@/lib/format'

interface LifecycleRow {
  label: string
  iso?: string
}

/**
 * Builds a starter Markdown postmortem document from the SEV's own recorded
 * facts — summary, lifecycle timestamps (with deltas), root cause, business
 * impact, affected services, and mitigation — so a postmortem author isn't
 * starting from a blank page: the mechanical facts that are already on the
 * SEV record are filled in, leaving just the narrative/analysis to write.
 *
 * Only ever used to *seed* the editor the first time it's opened with no
 * content yet (see PostmortemPage.tsx) — it never overwrites anything a
 * human has already written, and saving is still a separate, explicit step.
 */
export function buildPostmortemTemplate(sev: SEVResponse): string {
  const allRows: LifecycleRow[] = [
    { label: 'Started', iso: sev.started_at },
    { label: 'Detected', iso: sev.detected_at },
    { label: 'Mitigated', iso: sev.mitigated_at },
    { label: 'Resolved', iso: sev.resolved_at },
    { label: 'Postmortem complete', iso: sev.postmortem_completed_at },
  ]
  const rows = allRows.filter((row): row is Required<LifecycleRow> => Boolean(row.iso))

  let lifecycle: string
  if (rows.length === 0) {
    lifecycle = '_No lifecycle timestamps recorded yet._'
  } else {
    const lines = ['| Stage | Timestamp | Time since previous |', '| --- | --- | --- |']
    let prev: string | undefined
    for (const row of rows) {
      const delta = prev
        ? formatDurationSeconds((new Date(row.iso).getTime() - new Date(prev).getTime()) / 1000)
        : '—'
      lines.push(`| ${row.label} | ${formatDateTime(row.iso)} | ${delta} |`)
      prev = row.iso
    }
    lifecycle = lines.join('\n')
  }

  const rootCauseCategory = sev.root_cause_category
    ? (ROOT_CAUSE_CATEGORY_LABELS[sev.root_cause_category as RootCauseCategory] ?? sev.root_cause_category)
    : '_Not yet determined._'

  const services = sev.affected_services?.length
    ? sev.affected_services.map((service) => `- ${service}`).join('\n')
    : '_No services recorded._'

  const rootCauseReference = sev.root_cause_reference_url?.trim()
    ? `\n\n**Reference:** ${sev.root_cause_reference_url.trim()}`
    : ''

  return `## Summary

${sev.description?.trim() || '_No description provided._'}

## Lifecycle

${lifecycle}

## Root Cause

**Category:** ${rootCauseCategory}

${sev.root_cause_description?.trim() || '_Not yet determined._'}${rootCauseReference}

## Business Impact

${sev.business_impact?.trim() || '_Not yet documented._'}

## Services Affected

${services}

## Mitigation

${sev.mitigation?.trim() || '_Not yet documented._'}
`
}
