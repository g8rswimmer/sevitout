import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown } from 'lucide-react'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import { formatDurationSeconds } from '@/lib/format'
import type { ServiceLevelMetrics, ServiceResponse } from '@/types/api'

const WINDOW_OPTIONS = [30, 60, 90, 180] as const
type WindowDays = (typeof WINDOW_OPTIONS)[number]

const SEVERITY_LABEL: Record<number, string> = { 1: 'SEV-1', 2: 'SEV-2', 3: 'SEV-3', 4: 'SEV-4' }
const SEVERITY_LEVELS = [1, 2, 3, 4]

/** Compliance % + count breakdown for one (service, severity) group. A row
 * where every SEV was `not_applicable` (no SLA configured) shows that fact
 * in muted text instead of a misleading "0%" — 0% reads as "this service is
 * failing its SLA," which is a different claim than "this service has no
 * SLA to measure against." Every count/`compliance_pct` is coalesced with
 * `?? 0` — see ServiceLevelMetrics' doc comment (types/api.ts): protojson
 * omits a zero-valued field from the JSON body entirely, so e.g. a group
 * with zero breached SEVs has no "sla_breached_count" key at all. */
function ComplianceCell({ metrics }: { metrics: ServiceLevelMetrics }) {
  const measured = (metrics.sla_ok_count ?? 0) + (metrics.sla_at_risk_count ?? 0) + (metrics.sla_breached_count ?? 0)
  if (measured === 0) {
    return <span className="text-muted-foreground">No SLA configured</span>
  }
  return <span>{Math.round((metrics.compliance_pct ?? 0) * 100)}%</span>
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
 * The Service filter drives the real `GetServiceMetrics` request's
 * `service_ids` field — checking a service narrows what's fetched, not just
 * what's displayed, so it's part of the query key below. Severity stays a
 * client-side filter over the fetched rows: a window's response already
 * contains every severity level in one call, and the set is a fixed four,
 * so there's no round-trip to save by adding a server-side filter for it. */
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

  const metrics = useQuery({
    queryKey: ['reports', 'service-metrics', windowDays, selectedServiceIds],
    queryFn: () => api.reports.serviceMetrics(windowDays, selectedServiceIds.length ? selectedServiceIds : undefined),
  })
  const rows = metrics.data?.service_level_metrics ?? []

  const serviceOptions = useMemo(
    () =>
      [...services]
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((s) => ({ value: s.id, label: s.name })),
    [services],
  )
  const severityOptions = SEVERITY_LEVELS.map((level) => ({ value: level, label: SEVERITY_LABEL[level] }))

  const filteredRows = rows.filter(
    (row) => selectedSeverities.length === 0 || selectedSeverities.includes(row.severity_level),
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

      {metrics.isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : metrics.isError ? (
        <p role="alert" className="text-sm text-destructive">
          Failed to load service metrics: {(metrics.error as Error).message}
        </p>
      ) : rows.length === 0 ? (
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
