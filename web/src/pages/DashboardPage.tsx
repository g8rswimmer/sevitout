import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { AlertTriangle, ClipboardList } from 'lucide-react'
import { api } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge, severityVariant } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { SeverityBadge, StatusBadge } from '@/components/sev/badges'
import { MTTRTrendChart } from '@/components/reports/MTTRTrendChart'
import { formatDateTime } from '@/lib/format'
import { ACTIVE_SEV_STATUSES } from '@/types/api'

export function DashboardPage() {
  const metrics = useQuery({
    queryKey: ['reports', 'dashboard'],
    queryFn: api.reports.dashboardMetrics,
  })
  const activeSevs = useQuery({
    queryKey: ['sevs', 'active'],
    queryFn: () => api.sevs.list({ statuses: ACTIVE_SEV_STATUSES, limit: 10 }),
  })

  const countByLevel = new Map(metrics.data?.active_by_level?.map((c) => [c.severity_level, c.count]) ?? [])
  const totalActive = [...countByLevel.values()].reduce((a, b) => a + b, 0)
  const activeSevList = activeSevs.data?.sevs ?? []

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold">Dashboard</h1>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader>
            <CardTitle>Active SEVs</CardTitle>
          </CardHeader>
          <CardContent>
            {metrics.isLoading ? (
              <Skeleton className="h-9 w-16" />
            ) : (
              <>
                <div className="text-3xl font-semibold">{totalActive}</div>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {[1, 2, 3, 4].map((level) => (
                    <Badge key={level} variant={severityVariant(level)}>
                      SEV-{level}: {countByLevel.get(level) ?? 0}
                    </Badge>
                  ))}
                </div>
              </>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Overdue Tasks</CardTitle>
          </CardHeader>
          <CardContent>
            {metrics.isLoading ? (
              <Skeleton className="h-9 w-16" />
            ) : (
              <div className="flex items-center gap-2 text-3xl font-semibold">
                {(metrics.data?.overdue_task_count ?? 0) > 0 && (
                  <AlertTriangle className="h-6 w-6 text-destructive" aria-hidden />
                )}
                {metrics.data?.overdue_task_count ?? 0}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Postmortem Completion</CardTitle>
          </CardHeader>
          <CardContent>
            {metrics.isLoading ? (
              <Skeleton className="h-9 w-16" />
            ) : (
              <div className="text-3xl font-semibold">
                {Math.round((metrics.data?.postmortem_completion_rate ?? 0) * 100)}%
              </div>
            )}
          </CardContent>
        </Card>

        <Card className="sm:col-span-2 lg:col-span-1">
          <CardHeader>
            <CardTitle>MTTR Trend</CardTitle>
          </CardHeader>
          <CardContent>
            {metrics.isLoading ? (
              <Skeleton className="h-16 w-full" />
            ) : (
              <MTTRTrendChart trends={metrics.data?.mttr_trends ?? []} />
            )}
          </CardContent>
        </Card>
      </div>

      {metrics.isError && (
        <p role="alert" className="text-sm text-destructive">
          Failed to load dashboard metrics: {(metrics.error as Error).message}
        </p>
      )}

      <Card>
        <CardHeader className="flex-row items-center justify-between space-y-0">
          <CardTitle className="flex items-center gap-1.5 text-base text-foreground">
            <ClipboardList className="h-4 w-4" aria-hidden />
            Active SEVs
          </CardTitle>
        </CardHeader>
        <CardContent>
          {activeSevs.isLoading && (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          )}
          {activeSevs.isError && (
            <p role="alert" className="text-sm text-destructive">
              Failed to load active SEVs: {(activeSevs.error as Error).message}
            </p>
          )}
          {activeSevs.data && activeSevList.length === 0 && (
            <p className="text-sm text-muted-foreground">No active SEVs. All clear.</p>
          )}
          {activeSevs.data && activeSevList.length > 0 && (
            <ul className="divide-y divide-border">
              {activeSevList.map((sev) => (
                <li key={sev.id} className="flex items-center justify-between gap-3 py-2.5">
                  <Link to={`/sevs/${sev.id}`} className="flex min-w-0 items-center gap-3">
                    <SeverityBadge level={sev.severity_level} />
                    <span className="truncate font-medium hover:underline">{sev.title}</span>
                  </Link>
                  <div className="flex shrink-0 items-center gap-3">
                    <StatusBadge status={sev.status} />
                    <span className="text-xs text-muted-foreground">{formatDateTime(sev.started_at)}</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
