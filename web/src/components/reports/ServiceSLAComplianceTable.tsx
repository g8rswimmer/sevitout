import { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronDown } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { formatDurationSeconds } from '@/lib/format'
import type { ServiceResponse } from '@/types/api'

/**
 * Mirrors the planned `ReportService.GetServiceMetrics` RPC (see
 * docs/roadmap.md Phase 13b) field-for-field. This is a local type, not
 * imported from `types/api.ts`, because that RPC doesn't exist yet — 13d
 * adds the real `ServiceLevelMetrics`/`ServiceMetricsResponse` types once
 * this shape is reviewed and locked in; 13e then swaps `FIXTURES_BY_WINDOW`
 * below for a `useQuery` against the real endpoint without touching the
 * columns or rendering logic.
 */
interface ServiceLevelMetrics {
  service_id: string
  severity_level: number
  sev_count: number
  avg_mttd_seconds?: number
  avg_mttm_seconds?: number
  avg_mttr_seconds?: number
  sla_ok_count: number
  sla_at_risk_count: number
  sla_breached_count: number
  sla_not_applicable_count: number
  /** `ok / (ok + at_risk + breached)`, 0 when that denominator is 0 (i.e.
   * every SEV in the group was `not_applicable` — no SLA configured for
   * this service+severity). The 0 in that case is not itself meaningful —
   * see the render logic below, which shows "No SLA configured" instead of
   * "0%" so an unconfigured SLA is never confused with a breached one. */
  compliance_pct: number
}

const WINDOW_OPTIONS = [30, 60, 90, 180] as const
type WindowDays = (typeof WINDOW_OPTIONS)[number]

/** Fixture data standing in for `GetServiceMetrics` until 13b/13d ship (see
 * this file's header comment). Keyed per window so the selector genuinely
 * changes what's on screen — not just a decorative control — which is the
 * point of reviewing this as a clickable mock rather than a static image.
 * 180 days is deliberately empty, to make the table's empty state part of
 * the reviewable flow instead of something described only in prose. */
const FIXTURES_BY_WINDOW: Record<WindowDays, ServiceLevelMetrics[]> = {
  30: [
    {
      service_id: 'checkout-api',
      severity_level: 1,
      sev_count: 4,
      avg_mttd_seconds: 420,
      avg_mttm_seconds: 1800,
      avg_mttr_seconds: 5400,
      sla_ok_count: 3,
      sla_at_risk_count: 0,
      sla_breached_count: 1,
      sla_not_applicable_count: 0,
      compliance_pct: 0.75,
    },
    {
      service_id: 'checkout-api',
      severity_level: 2,
      sev_count: 6,
      avg_mttd_seconds: 300,
      avg_mttm_seconds: 1200,
      avg_mttr_seconds: 3600,
      sla_ok_count: 5,
      sla_at_risk_count: 1,
      sla_breached_count: 0,
      sla_not_applicable_count: 0,
      compliance_pct: 5 / 6,
    },
    {
      service_id: 'payments-api',
      severity_level: 1,
      sev_count: 2,
      avg_mttd_seconds: 600,
      avg_mttm_seconds: 2400,
      avg_mttr_seconds: 7200,
      sla_ok_count: 1,
      sla_at_risk_count: 0,
      sla_breached_count: 1,
      sla_not_applicable_count: 0,
      compliance_pct: 0.5,
    },
    {
      service_id: 'payments-api',
      severity_level: 3,
      sev_count: 5,
      avg_mttd_seconds: 900,
      avg_mttm_seconds: 3000,
      avg_mttr_seconds: 5400,
      sla_ok_count: 5,
      sla_at_risk_count: 0,
      sla_breached_count: 0,
      sla_not_applicable_count: 0,
      compliance_pct: 1,
    },
    {
      service_id: 'auth-service',
      severity_level: 2,
      sev_count: 3,
      avg_mttd_seconds: 240,
      avg_mttm_seconds: 900,
      avg_mttr_seconds: 2700,
      sla_ok_count: 3,
      sla_at_risk_count: 0,
      sla_breached_count: 0,
      sla_not_applicable_count: 0,
      compliance_pct: 1,
    },
    {
      service_id: 'notification-service',
      severity_level: 4,
      sev_count: 7,
      avg_mttd_seconds: 1500,
      avg_mttm_seconds: 4200,
      avg_mttr_seconds: 9000,
      sla_ok_count: 0,
      sla_at_risk_count: 0,
      sla_breached_count: 0,
      sla_not_applicable_count: 7,
      compliance_pct: 0,
    },
  ],
  60: [
    {
      service_id: 'checkout-api',
      severity_level: 1,
      sev_count: 7,
      avg_mttd_seconds: 480,
      avg_mttm_seconds: 2100,
      avg_mttr_seconds: 6000,
      sla_ok_count: 5,
      sla_at_risk_count: 0,
      sla_breached_count: 2,
      sla_not_applicable_count: 0,
      compliance_pct: 5 / 7,
    },
    {
      service_id: 'checkout-api',
      severity_level: 2,
      sev_count: 10,
      avg_mttd_seconds: 330,
      avg_mttm_seconds: 1260,
      avg_mttr_seconds: 3900,
      sla_ok_count: 9,
      sla_at_risk_count: 1,
      sla_breached_count: 0,
      sla_not_applicable_count: 0,
      compliance_pct: 0.9,
    },
    {
      service_id: 'payments-api',
      severity_level: 1,
      sev_count: 3,
      avg_mttd_seconds: 660,
      avg_mttm_seconds: 2520,
      avg_mttr_seconds: 7500,
      sla_ok_count: 2,
      sla_at_risk_count: 0,
      sla_breached_count: 1,
      sla_not_applicable_count: 0,
      compliance_pct: 2 / 3,
    },
    {
      service_id: 'auth-service',
      severity_level: 2,
      sev_count: 5,
      avg_mttd_seconds: 260,
      avg_mttm_seconds: 960,
      avg_mttr_seconds: 2850,
      sla_ok_count: 5,
      sla_at_risk_count: 0,
      sla_breached_count: 0,
      sla_not_applicable_count: 0,
      compliance_pct: 1,
    },
    {
      service_id: 'notification-service',
      severity_level: 4,
      sev_count: 11,
      avg_mttd_seconds: 1620,
      avg_mttm_seconds: 4500,
      avg_mttr_seconds: 9600,
      sla_ok_count: 0,
      sla_at_risk_count: 0,
      sla_breached_count: 0,
      sla_not_applicable_count: 11,
      compliance_pct: 0,
    },
  ],
  90: [
    {
      service_id: 'checkout-api',
      severity_level: 1,
      sev_count: 9,
      avg_mttd_seconds: 510,
      avg_mttm_seconds: 2250,
      avg_mttr_seconds: 6300,
      sla_ok_count: 6,
      sla_at_risk_count: 1,
      sla_breached_count: 2,
      sla_not_applicable_count: 0,
      compliance_pct: 6 / 9,
    },
    {
      service_id: 'payments-api',
      severity_level: 1,
      sev_count: 4,
      avg_mttd_seconds: 690,
      avg_mttm_seconds: 2640,
      avg_mttr_seconds: 7800,
      sla_ok_count: 3,
      sla_at_risk_count: 0,
      sla_breached_count: 1,
      sla_not_applicable_count: 0,
      compliance_pct: 0.75,
    },
    {
      service_id: 'payments-api',
      severity_level: 3,
      sev_count: 8,
      avg_mttd_seconds: 930,
      avg_mttm_seconds: 3120,
      avg_mttr_seconds: 5700,
      sla_ok_count: 8,
      sla_at_risk_count: 0,
      sla_breached_count: 0,
      sla_not_applicable_count: 0,
      compliance_pct: 1,
    },
    {
      service_id: 'auth-service',
      severity_level: 2,
      sev_count: 6,
      avg_mttd_seconds: 270,
      avg_mttm_seconds: 990,
      avg_mttr_seconds: 2970,
      sla_ok_count: 5,
      sla_at_risk_count: 1,
      sla_breached_count: 0,
      sla_not_applicable_count: 0,
      compliance_pct: 5 / 6,
    },
    {
      service_id: 'notification-service',
      severity_level: 4,
      sev_count: 14,
      avg_mttd_seconds: 1680,
      avg_mttm_seconds: 4680,
      avg_mttr_seconds: 9900,
      sla_ok_count: 0,
      sla_at_risk_count: 0,
      sla_breached_count: 0,
      sla_not_applicable_count: 14,
      compliance_pct: 0,
    },
  ],
  180: [],
}

const SEVERITY_LABEL: Record<number, string> = { 1: 'SEV-1', 2: 'SEV-2', 3: 'SEV-3', 4: 'SEV-4' }
const SEVERITY_LEVELS = [1, 2, 3, 4]

/** Compliance % + count breakdown for one (service, severity) group. A row
 * where every SEV was `not_applicable` (no SLA configured) shows that fact
 * in muted text instead of a misleading "0%" — 0% reads as "this service is
 * failing its SLA," which is a different claim than "this service has no
 * SLA to measure against." */
function ComplianceCell({ metrics }: { metrics: ServiceLevelMetrics }) {
  const measured = metrics.sla_ok_count + metrics.sla_at_risk_count + metrics.sla_breached_count
  if (measured === 0) {
    return <span className="text-muted-foreground">No SLA configured</span>
  }
  return <span>{Math.round(metrics.compliance_pct * 100)}%</span>
}

function WindowSelector({ value, onChange }: { value: WindowDays; onChange: (days: WindowDays) => void }) {
  return (
    <div role="group" aria-label="Reporting window" className="flex gap-1">
      {WINDOW_OPTIONS.map((days) => (
        <Button
          key={days}
          type="button"
          size="sm"
          variant={value === days ? 'default' : 'outline'}
          aria-pressed={value === days}
          onClick={() => onChange(days)}
        >
          {days}d
        </Button>
      ))}
    </div>
  )
}

/** A button that opens a checkbox-list popover — same "empty selection = no
 * filter, otherwise narrow to the checked set" convention as
 * SevListPage.tsx's severity/status filters, just collapsed behind a
 * dropdown instead of always-visible, so two multi-value filters fit this
 * card's header row without crowding it. No Radix (see select.tsx/
 * checkbox.tsx/dialog.tsx/tooltip.tsx for the same "plain element over a
 * new dependency" choice) — a plain `<div>` positioned under the trigger
 * button, closed on outside click or Escape.
 *
 * `onChange` replaces the whole selection rather than toggling one value at
 * a time, so "Select all"/"Clear" can reuse it instead of needing separate
 * bulk-action props. */
function MultiSelectDropdown<T extends string | number>({
  label,
  options,
  selected,
  onChange,
}: {
  label: string
  options: { value: T; label: string }[]
  selected: T[]
  onChange: (values: T[]) => void
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function onPointerDown(e: PointerEvent) {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false)
    }
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    window.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown)
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  const summary = selected.length === 0 ? `All ${label.toLowerCase()}` : `${label} (${selected.length})`
  const allSelected = options.length > 0 && selected.length === options.length

  function toggle(value: T) {
    onChange(selected.includes(value) ? selected.filter((v) => v !== value) : [...selected, value])
  }

  return (
    <div ref={rootRef} className="relative">
      <Button
        type="button"
        variant="outline"
        size="sm"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        {summary}
        <ChevronDown className="h-3.5 w-3.5" aria-hidden />
      </Button>
      {open && (
        <div
          role="listbox"
          aria-label={label}
          aria-multiselectable="true"
          className="absolute left-0 top-full z-20 mt-1 w-48 rounded-md border border-border bg-card p-1.5 shadow-md"
        >
          <div className="mb-1 flex items-center justify-between gap-2 border-b border-border pb-1.5">
            <button
              type="button"
              className="text-xs font-medium text-primary hover:underline disabled:pointer-events-none disabled:text-muted-foreground disabled:no-underline"
              disabled={allSelected}
              onClick={() => onChange(options.map((opt) => opt.value))}
            >
              Select all
            </button>
            <button
              type="button"
              className="text-xs font-medium text-primary hover:underline disabled:pointer-events-none disabled:text-muted-foreground disabled:no-underline"
              disabled={selected.length === 0}
              onClick={() => onChange([])}
            >
              Clear
            </button>
          </div>
          <div className="max-h-60 overflow-y-auto">
            {options.map((opt) => (
              <label
                key={opt.value}
                className="flex items-center gap-2 rounded px-2 py-1 text-sm hover:bg-accent"
              >
                <Checkbox checked={selected.includes(opt.value)} onChange={() => toggle(opt.value)} />
                {opt.label}
              </label>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

/** `serviceName` mirrors ServiceHeatmap/RecurringPatternsTable's own prop
 * (ReportsPage.tsx) — resolving a service ID to its display name is the
 * page's job, not this table's, so all three components share one lookup
 * instead of each fetching services independently. `services` is the same
 * registry ReportsPage already fetches (`api.services.list`) for that
 * lookup, reused here as the source of the service filter dropdown's
 * options instead of a second fetch.
 *
 * Both filters are local/client-side in this mock. Per Phase 13b/13d,
 * `GetServiceMetricsRequest` already plans a `service_ids` field and
 * `api.reports.serviceMetrics` already plans a `serviceIds` param — 13e
 * wires the service filter's selection into that request instead of
 * filtering the response, once it exists. Severity has no equivalent
 * request field (nor does it need one): a window's response already
 * contains every severity level in one call, so narrowing to a subset is
 * purely a view concern and stays a client-side filter in 13e too. */
export function ServiceSLAComplianceTable({
  serviceName,
  services,
}: {
  serviceName: (id: string) => string
  services: ServiceResponse[]
}) {
  const [windowDays, setWindowDays] = useState<WindowDays>(30)
  const [selectedServiceIds, setSelectedServiceIds] = useState<string[]>([])
  const [selectedSeverities, setSelectedSeverities] = useState<number[]>([])
  const rows = FIXTURES_BY_WINDOW[windowDays]

  const serviceOptions = useMemo(
    () =>
      [...services]
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((s) => ({ value: s.id, label: s.name })),
    [services],
  )
  const severityOptions = SEVERITY_LEVELS.map((level) => ({ value: level, label: SEVERITY_LABEL[level] }))

  const filteredRows = rows.filter(
    (row) =>
      (selectedServiceIds.length === 0 || selectedServiceIds.includes(row.service_id)) &&
      (selectedSeverities.length === 0 || selectedSeverities.includes(row.severity_level)),
  )

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-wrap gap-2">
          {serviceOptions.length > 0 && (
            <MultiSelectDropdown
              label="Service"
              options={serviceOptions}
              selected={selectedServiceIds}
              onChange={setSelectedServiceIds}
            />
          )}
          <MultiSelectDropdown
            label="Severity"
            options={severityOptions}
            selected={selectedSeverities}
            onChange={setSelectedSeverities}
          />
        </div>
        <WindowSelector value={windowDays} onChange={setWindowDays} />
      </div>

      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">No SEVs opened in the selected window.</p>
      ) : filteredRows.length === 0 ? (
        <p className="text-sm text-muted-foreground">No SEVs match the selected filters.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3">Service</th>
                <th className="py-2 pr-3">Severity</th>
                <th className="px-2 py-2 text-right">SEV Count</th>
                <th className="px-2 py-2 text-right">Avg MTTD</th>
                <th className="px-2 py-2 text-right">Avg MTTM</th>
                <th className="px-2 py-2 text-right">Avg MTTR</th>
                <th className="px-2 py-2 text-right">Compliance</th>
              </tr>
            </thead>
            <tbody>
              {filteredRows.map((row) => (
                <tr key={`${row.service_id}:${row.severity_level}`} className="border-b border-border">
                  <td className="py-2 pr-3 font-medium">{serviceName(row.service_id)}</td>
                  <td className="py-2 pr-3">{SEVERITY_LABEL[row.severity_level] ?? row.severity_level}</td>
                  <td className="px-2 py-2 text-right">{row.sev_count}</td>
                  <td className="px-2 py-2 text-right">{formatDurationSeconds(row.avg_mttd_seconds)}</td>
                  <td className="px-2 py-2 text-right">{formatDurationSeconds(row.avg_mttm_seconds)}</td>
                  <td className="px-2 py-2 text-right">{formatDurationSeconds(row.avg_mttr_seconds)}</td>
                  <td className="px-2 py-2 text-right">
                    <ComplianceCell metrics={row} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
