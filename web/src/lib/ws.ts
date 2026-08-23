import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import type { WSEvent } from '@/types/api'

const RECONNECT_DELAY_MS = 3000

function wsURL(sevId: string): string {
  // Same-origin, like every other request lib/api.ts makes (see its header
  // comment) — resolved by vite.config.ts's dev proxy or nginx.conf in
  // production, both of which forward /ws to the api service.
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}/ws?sev_id=${encodeURIComponent(sevId)}`
}

/** Maps a WS event's type prefix (internal/api/ws/hub.go's Event.Type, e.g.
 * "sev.updated", "announcement.created" — see docs/architecture.md §3.2 for
 * the full list) to the query key(s) it should invalidate, rather than
 * trying to merge the event's payload into the cache directly — every
 * mutation this app makes already refetches its own query on success, so
 * this only needs to catch changes made by another client (a different tab,
 * another user, the Slack bot). A redundant invalidate right after your own
 * successful mutation is harmless. */
function queryKeysForEvent(sevId: string, eventType: string): unknown[][] {
  if (eventType.startsWith('sev.')) {
    return [
      ['sevs', 'detail', sevId],
      ['sevs', 'active'],
      ['search', 'sevs'],
    ]
  }
  if (eventType.startsWith('announcement.')) return [['sevs', sevId, 'announcements']]
  if (eventType.startsWith('chat.')) return [['sevs', sevId, 'chat']]
  if (eventType.startsWith('role.')) return [['sevs', sevId, 'roles']]
  if (eventType.startsWith('task.')) return [['sevs', sevId, 'tasks']]
  if (eventType.startsWith('postmortem.')) return [['postmortems', sevId]]
  if (eventType.startsWith('ai.')) return [['ai', 'outputs', sevId]]
  return [['sevs', 'detail', sevId]]
}

function invalidateForEvent(queryClient: QueryClient, sevId: string, raw: string) {
  let evt: WSEvent
  try {
    evt = JSON.parse(raw)
  } catch {
    return // malformed frame — ignore, matches the server's own tolerance
  }
  if (evt.sev_id !== sevId) return
  for (const queryKey of queryKeysForEvent(sevId, evt.type)) {
    void queryClient.invalidateQueries({ queryKey })
  }
}

/** Subscribes to `sevId`'s room over the shared /ws endpoint for as long as
 * the calling component is mounted, invalidating the affected TanStack Query
 * caches whenever an event for this SEV arrives — the same "WS event →
 * cache invalidation" pattern docs/architecture.md §3.2 describes, rather
 * than hand-patching the cache from each event's payload. Reconnects on any
 * close (server restart, transient network blip, the server's own 60s
 * idle-ping timeout if this tab was suspended) after a fixed delay; there is
 * no backoff ceiling because a SEV detail page is normally open for minutes
 * at most, not long enough for a fixed 3s retry to matter. */
export function useSevSocket(sevId: string | undefined) {
  const queryClient = useQueryClient()

  useEffect(() => {
    if (!sevId) return

    let cancelled = false
    let socket: WebSocket | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined

    function connect() {
      if (cancelled) return
      socket = new WebSocket(wsURL(sevId!))
      socket.onmessage = (evt) => invalidateForEvent(queryClient, sevId!, evt.data)
      socket.onclose = () => {
        if (!cancelled) reconnectTimer = setTimeout(connect, RECONNECT_DELAY_MS)
      }
    }
    connect()

    return () => {
      cancelled = true
      clearTimeout(reconnectTimer)
      socket?.close()
    }
  }, [sevId, queryClient])
}
