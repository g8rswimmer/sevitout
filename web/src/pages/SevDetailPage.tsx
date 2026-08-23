import { useParams } from 'react-router-dom'

/** Placeholder — the full SEV detail view (roles, announcements, chat log,
 * linked tasks/SEVs, SLIs, edit-in-place) lands in M14b. This stub exists
 * so the dashboard's active-SEV links resolve to something today instead of
 * a 404, and keeps the /sevs/:id route stable across sub-milestones. */
export function SevDetailPage() {
  const { id } = useParams<{ id: string }>()
  return (
    <div className="flex flex-col gap-2">
      <h1 className="text-2xl font-semibold">SEV {id}</h1>
      <p className="text-muted-foreground">
        The full SEV detail view is coming in M14b. This page currently only confirms routing works.
      </p>
    </div>
  )
}
