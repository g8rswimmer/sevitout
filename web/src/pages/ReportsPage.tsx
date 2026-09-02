import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '@/lib/api'
import { Section } from '@/components/sev/Section'
import { Skeleton } from '@/components/ui/skeleton'
import { MTTRTrendChart } from '@/components/reports/MTTRTrendChart'
import { ServiceSLAComplianceTable } from '@/components/reports/ServiceSLAComplianceTable'
import { ROOT_CAUSE_CATEGORY_LABELS, type RecurringPattern, type RootCauseCategory, type ServiceLevelFrequency } from '@/types/api'

const SEVERITY_LEVELS = [1, 2, 3, 4]

export function ReportsPage() {
  const metrics = useQuery({ queryKey: ['reports', 'dashboard'], queryFn: api.reports.dashboardMetrics })
  const trends = useQuery({ queryKey: ['reports', 'trends'], queryFn: api.reports.trends })
  const services = useQuery({ queryKey: ['services'], queryFn: api.services.list })

  function serviceName(id: string): string {
    return services.data?.services?.find((s) => s.id === id)?.name ?? id
  }

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold">Reports</h1>

      {metrics.isError && (
        <p role="alert" className="text-sm text-destructive">
          Failed to load report data: {(metrics.error as Error).message}
        </p>
      )}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Section title="MTTR Trend">
          {metrics.isLoading ? (
            <Skeleton className="h-20 w-full" />
          ) : (
            <MTTRTrendChart trends={metrics.data?.mttr_trends ?? []} />
          )}
        </Section>

        <Section title="Postmortem Completion Rate">
          {metrics.isLoading ? (
            <Skeleton className="h-9 w-16" />
          ) : (
            <div className="text-4xl font-semibold">
              {Math.round((metrics.data?.postmortem_completion_rate ?? 0) * 100)}%
            </div>
          )}
        </Section>
      </div>

      <Section title="Service × Severity Heatmap">
        {metrics.isLoading ? (
          <Skeleton className="h-32 w-full" />
        ) : (
          <ServiceHeatmap frequencies={metrics.data?.frequency_by_service_and_level ?? []} serviceName={serviceName} />
        )}
      </Section>

      <Section title="Recurring Incident Patterns">
        {trends.isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : (
          <RecurringPatternsTable patterns={trends.data?.recurring_patterns ?? []} serviceName={serviceName} />
        )}
      </Section>

      {/* UX mock (Phase 13a, docs/roadmap.md) — fixture-driven, no backend
       * call yet. 13e swaps this for a `useQuery` against
       * `GetServiceMetrics` once 13b/13d ship; this Section wrapper and the
       * table's column set are meant to carry over unchanged. */}
      <Section title="SLA Compliance by Service">
        <ServiceSLAComplianceTable serviceName={serviceName} services={services.data?.services ?? []} />
      </Section>
    </div>
  )
}

/** Rows are every service that appears in `frequencies` (there's no
 * standalone "count of SEVs per service" endpoint — this is derived
 * entirely from the same service+severity breakdown DashboardPage's card
 * summarizes); columns are the four fixed severity levels. Cell shading is
 * a few discrete steps of `bg-primary`'s opacity relative to the highest
 * count in the whole grid — no charting library, no hardcoded color (the
 * `--primary` CSS variable is already theme-aware), just Tailwind's opacity
 * modifier. */
function ServiceHeatmap({
  frequencies,
  serviceName,
}: {
  frequencies: ServiceLevelFrequency[]
  serviceName: (id: string) => string
}) {
  if (frequencies.length === 0) {
    return <p className="text-sm text-muted-foreground">No SEVs with affected services recorded yet.</p>
  }

  const serviceIds = [...new Set(frequencies.map((f) => f.service_id))].sort((a, b) =>
    serviceName(a).localeCompare(serviceName(b)),
  )
  const countByKey = new Map(frequencies.map((f) => [`${f.service_id}:${f.severity_level}`, f.count]))
  const max = Math.max(1, ...frequencies.map((f) => f.count))

  function cellClass(count: number): string {
    if (count === 0) return ''
    const ratio = count / max
    if (ratio >= 0.75) return 'bg-primary/80 text-primary-foreground font-semibold'
    if (ratio >= 0.5) return 'bg-primary/50 font-semibold'
    if (ratio >= 0.25) return 'bg-primary/25'
    return 'bg-primary/10'
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border text-left text-xs text-muted-foreground">
            <th className="py-2 pr-3">Service</th>
            {SEVERITY_LEVELS.map((level) => (
              <th key={level} className="px-2 py-2 text-center">
                SEV-{level}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {serviceIds.map((id) => (
            <tr key={id} className="border-b border-border">
              <td className="py-2 pr-3 font-medium">{serviceName(id)}</td>
              {SEVERITY_LEVELS.map((level) => {
                const count = countByKey.get(`${id}:${level}`) ?? 0
                return (
                  <td key={level} className={`px-2 py-2 text-center ${cellClass(count)}`}>
                    {count || <span className="text-muted-foreground">—</span>}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function RecurringPatternsTable({
  patterns,
  serviceName,
}: {
  patterns: RecurringPattern[]
  serviceName: (id: string) => string
}) {
  if (patterns.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No recurring incident patterns yet — a pattern appears once 2+ SEVs share both an affected service and a
        root cause category.
      </p>
    )
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border text-left text-xs text-muted-foreground">
            <th className="py-2 pr-3">Service</th>
            <th className="py-2 pr-3">Root cause category</th>
            <th className="py-2 pr-3">Count</th>
            <th className="py-2 pr-3">SEVs (most recent first)</th>
          </tr>
        </thead>
        <tbody>
          {patterns.map((p, i) => (
            <tr key={`${p.service_id}:${p.root_cause_category}:${i}`} className="border-b border-border align-top">
              <td className="py-2 pr-3 font-medium">{serviceName(p.service_id)}</td>
              <td className="py-2 pr-3">
                {ROOT_CAUSE_CATEGORY_LABELS[p.root_cause_category as RootCauseCategory] ?? p.root_cause_category}
              </td>
              <td className="py-2 pr-3">{p.count}</td>
              <td className="py-2 pr-3">
                <div className="flex flex-wrap gap-2">
                  {(p.sev_ids ?? []).map((id) => (
                    <Link key={id} to={`/sevs/${id}`} className="text-primary hover:underline">
                      {id}
                    </Link>
                  ))}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
