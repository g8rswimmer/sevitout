import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'

/** Roadmap Phase 11b's shared "which integrations are configured" query —
 * backs TasksPanel's GitHub/Jira create-issue gating and the Slack
 * "Add to chat" / "Join Slack channel" actions. A single query key means
 * every consumer on a page shares one request/cache entry (React Query's
 * default staleTime, set in queryClient.ts, already covers the common case
 * of several panels mounting at once).
 *
 * Known limitation, inherited from the backend RPC: an integration active
 * only via its static env-var fallback (no store row) reports as not
 * enabled here — see ListEnabledIntegrationsResponse's doc comment.
 */
export function useEnabledIntegrations() {
  const query = useQuery({
    queryKey: ['config', 'enabled-integrations'],
    queryFn: () => api.config.integrations.enabled(),
  })
  const enabled = new Set(query.data?.enabled_types ?? [])
  return { ...query, isEnabled: (type: string) => enabled.has(type) }
}
