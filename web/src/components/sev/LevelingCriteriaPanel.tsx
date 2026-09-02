import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Skeleton } from '@/components/ui/skeleton'
import type { ServiceResponse } from '@/types/api'

/** Read-only reference panel showing the configured SEV leveling criteria
 * (Roadmap Phase 14) for the given services at the given severity level —
 * advisory guidance only, never enforced or validated against. Shared by
 * SevCreatePage.tsx (to help pick the right level while filling out the
 * form) and PostmortemPage.tsx (to help confirm the level chosen was
 * correct during writeup). `services` is passed in rather than fetched here
 * — the page owns the service-registry fetch and threads it down, same
 * convention ServiceSLAComplianceTable.tsx's `services` prop follows. */
export function LevelingCriteriaPanel({
  severityLevel,
  serviceIds,
  services,
}: {
  severityLevel: number
  serviceIds: string[]
  services: ServiceResponse[]
}) {
  const criteria = useQuery({
    queryKey: ['levelingCriteria', serviceIds, severityLevel],
    queryFn: () => api.config.levelingCriteria.listForServices(serviceIds, severityLevel),
    enabled: serviceIds.length > 0,
  })

  if (serviceIds.length === 0) {
    return null
  }

  if (criteria.isLoading) {
    return <Skeleton className="h-16 w-full" />
  }

  if (criteria.isError) {
    // Non-fatal: this is reference material, not a required field — fail
    // quietly rather than blocking the form/page it's embedded in.
    return null
  }

  const nameFor = (serviceId: string) => services.find((svc) => svc.id === serviceId)?.name ?? serviceId

  const rows = criteria.data?.criteria ?? []

  return (
    <div className="flex flex-col gap-2 rounded-md border border-dashed border-border p-3 text-sm">
      <p className="text-xs font-medium text-muted-foreground">
        Leveling criteria for SEV-{severityLevel} (guidance only, not enforced)
      </p>
      {rows.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          No leveling criteria configured for the selected service(s) at SEV-{severityLevel}.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {rows.map((row) => (
            <li key={row.service_id}>
              <span className="font-medium">{nameFor(row.service_id)}: </span>
              <span className="text-muted-foreground">{row.criteria}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
