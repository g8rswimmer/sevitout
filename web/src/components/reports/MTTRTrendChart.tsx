/** A pure-CSS bar chart of MTTR over the 7/30/90-day windows
 * `ReportService.GetDashboardMetrics` always returns — no charting library,
 * same "hand-rolled with plain divs" approach the rest of this app uses for
 * anything visual (the heatmap on ReportsPage.tsx, the lifecycle-stage
 * strip on LifecyclePanel.tsx). Shared between DashboardPage (a compact
 * summary) and ReportsPage (the same chart, just given more room). */
export function MTTRTrendChart({ trends }: { trends: { window_days: number; average_mttr_seconds?: string }[] }) {
  const values = trends.map((t) => Number(t.average_mttr_seconds ?? 0))
  const max = Math.max(1, ...values)
  return (
    <div className="flex items-end gap-3">
      {trends.map((t) => {
        const seconds = Number(t.average_mttr_seconds ?? 0)
        const hours = seconds / 3600
        const heightPct = Math.max(4, (seconds / max) * 100)
        return (
          <div key={t.window_days} className="flex flex-1 flex-col items-center gap-1">
            <div className="flex h-16 w-full items-end">
              <div
                className="w-full rounded-t-sm bg-primary"
                style={{ height: `${heightPct}%` }}
                title={`${hours.toFixed(1)}h avg MTTR`}
              />
            </div>
            <div className="text-xs font-medium">{hours.toFixed(1)}h</div>
            <div className="text-[11px] text-muted-foreground">{t.window_days}d</div>
          </div>
        )
      })}
    </div>
  )
}
