import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Siren } from 'lucide-react'
import { api } from '@/lib/api'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { SeverityBadge, StatusBadge } from '@/components/sev/badges'
import { formatDateTime } from '@/lib/format'

/** GET /s/:token — the one page in this app that renders outside
 * AppLayout/ProtectedRoute (see App.tsx): a signed link can be opened by
 * anyone, logged in or not, so this can't require or even assume a
 * session. `api.shareView.get` hits a plain net/http handler
 * (internal/api/grpc.ShareViewHandler), not gRPC-gateway, and returns only
 * the curated public fields docs/requirements.md §14.1 lists — there is no
 * root cause, mitigation, chat log, or internal announcement to
 * accidentally render here even by mistake, because the data was never
 * sent in the first place. */
export function PublicSharePage() {
  const { token } = useParams<{ token: string }>()
  const view = useQuery({
    queryKey: ['shareView', token],
    queryFn: () => api.shareView.get(token!),
    // A 404 ("link not found")/410 (revoked/expired) is permanent for this
    // token; retrying just delays showing the (already descriptive) server
    // message.
    retry: false,
  })

  return (
    <div className="min-h-screen bg-background px-4 py-10">
      <div className="mx-auto flex max-w-2xl flex-col gap-4">
        <div className="flex items-center gap-2 text-lg font-semibold">
          <Siren className="h-5 w-5 text-primary" aria-hidden />
          Sevitout
        </div>

        {view.isLoading && (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-8 w-2/3" />
            <Skeleton className="h-40 w-full" />
          </div>
        )}

        {view.isError && (
          <Card>
            <CardContent className="py-8 text-center text-sm text-muted-foreground">
              {(view.error as Error).message || 'This link is invalid.'}
            </CardContent>
          </Card>
        )}

        {view.data && (
          <Card>
            <CardHeader>
              <CardTitle className="text-xl text-foreground">{view.data.title}</CardTitle>
              <div className="flex flex-wrap items-center gap-2 pt-1">
                <SeverityBadge level={view.data.severity_level} />
                <StatusBadge status={view.data.status} />
              </div>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              {view.data.business_impact && (
                <div>
                  <div className="text-xs text-muted-foreground">Business impact</div>
                  <div className="text-sm">{view.data.business_impact}</div>
                </div>
              )}

              <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                <TimestampField label="Started" value={view.data.started_at} />
                <TimestampField label="Detected" value={view.data.detected_at} />
                <TimestampField label="Mitigated" value={view.data.mitigated_at} />
                <TimestampField label="Resolved" value={view.data.resolved_at} />
                <TimestampField label="Postmortem complete" value={view.data.postmortem_completed_at} />
              </dl>

              {view.data.announcements.length > 0 && (
                <div>
                  <div className="mb-1.5 text-xs text-muted-foreground">Updates</div>
                  <ul className="flex flex-col gap-2">
                    {view.data.announcements.map((a, i) => (
                      <li key={i} className="rounded-md border border-border p-2.5 text-sm">
                        <p>{a.message}</p>
                        <p className="mt-1 text-xs text-muted-foreground">{formatDateTime(a.created_at)}</p>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}

function TimestampField({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm">{formatDateTime(value)}</dd>
    </div>
  )
}
