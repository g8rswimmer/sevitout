import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useSevSocket } from '@/lib/ws'
import { MockWebSocket } from '@/test/mockWebSocket'

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useSevSocket', () => {
  // Inline, not a shared helper — see mockWebSocket.ts's doc comment for why.
  beforeEach(() => {
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('does nothing when sevId is undefined', () => {
    const queryClient = new QueryClient()
    renderHook(() => useSevSocket(undefined), { wrapper: wrapper(queryClient) })
    expect(MockWebSocket.instances).toHaveLength(0)
  })

  it('connects to /ws?sev_id=<id> when mounted', () => {
    const queryClient = new QueryClient()
    renderHook(() => useSevSocket('SEV-2026-0001'), { wrapper: wrapper(queryClient) })
    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0].url).toContain('/ws?sev_id=SEV-2026-0001')
  })

  it('invalidates the matching query on a same-SEV event', () => {
    const queryClient = new QueryClient()
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    renderHook(() => useSevSocket('SEV-2026-0001'), { wrapper: wrapper(queryClient) })

    MockWebSocket.instances[0].emitMessage({ type: 'announcement.created', sev_id: 'SEV-2026-0001', payload: {} })

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['sevs', 'SEV-2026-0001', 'announcements'] })
  })

  it('ignores an event for a different SEV id', () => {
    const queryClient = new QueryClient()
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')
    renderHook(() => useSevSocket('SEV-2026-0001'), { wrapper: wrapper(queryClient) })

    MockWebSocket.instances[0].emitMessage({ type: 'sev.updated', sev_id: 'SEV-2026-9999', payload: {} })

    expect(invalidateSpy).not.toHaveBeenCalled()
  })

  it('ignores a malformed (non-JSON) frame without throwing', () => {
    const queryClient = new QueryClient()
    renderHook(() => useSevSocket('SEV-2026-0001'), { wrapper: wrapper(queryClient) })
    expect(() => MockWebSocket.instances[0].emitMessage('not json')).not.toThrow()
  })

  it('reconnects after the socket closes', () => {
    vi.useFakeTimers()
    const queryClient = new QueryClient()
    renderHook(() => useSevSocket('SEV-2026-0001'), { wrapper: wrapper(queryClient) })
    expect(MockWebSocket.instances).toHaveLength(1)

    MockWebSocket.instances[0].emitClose()
    vi.advanceTimersByTime(3000)

    expect(MockWebSocket.instances).toHaveLength(2)
    vi.useRealTimers()
  })

  it('closes the socket and does not reconnect after unmount', () => {
    vi.useFakeTimers()
    const queryClient = new QueryClient()
    const { unmount } = renderHook(() => useSevSocket('SEV-2026-0001'), { wrapper: wrapper(queryClient) })
    const socket = MockWebSocket.instances[0]

    unmount()
    expect(socket.closed).toBe(true)

    vi.advanceTimersByTime(5000)
    expect(MockWebSocket.instances).toHaveLength(1)
    vi.useRealTimers()
  })
})
