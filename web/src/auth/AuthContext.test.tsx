import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { AuthProvider } from '@/auth/AuthContext'
import { useAuth } from '@/auth/useAuth'
import { tokenStorage } from '@/lib/api'
import type { WhoAmIResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const ME: WhoAmIResponse = {
  id: 'u1',
  email: 'a@b.com',
  name: 'Ada',
  avatar_url: '',
  org_role: 'admin',
  oauth_provider: '',
}

describe('AuthProvider', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('starts logged out with no stored token, loading settles to false', async () => {
    const { result } = renderHook(() => useAuth(), { wrapper: AuthProvider })
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.user).toBeNull()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('hydrates the user from a stored token via WhoAmI', async () => {
    tokenStorage.set('existing-token')
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse(ME))
    const { result } = renderHook(() => useAuth(), { wrapper: AuthProvider })
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.user).toEqual(ME)
  })

  it('clears a stored token that WhoAmI rejects', async () => {
    tokenStorage.set('stale-token')
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse({ message: 'expired' }, 401))
    const { result } = renderHook(() => useAuth(), { wrapper: AuthProvider })
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.user).toBeNull()
    expect(tokenStorage.get()).toBeNull()
  })

  it('login stores the token and populates the user', async () => {
    vi.mocked(fetch)
      .mockResolvedValueOnce(jsonResponse({ token: 'new-token', user: { id: 'u1', email: 'a@b.com', name: 'Ada', org_role: 'admin' } }))
      .mockResolvedValueOnce(jsonResponse(ME))
    const { result } = renderHook(() => useAuth(), { wrapper: AuthProvider })
    await waitFor(() => expect(result.current.loading).toBe(false))

    await act(async () => {
      await result.current.login('a@b.com', 'password123')
    })

    expect(tokenStorage.get()).toBe('new-token')
    expect(result.current.user).toEqual(ME)
  })

  it('logout clears the token and user', async () => {
    tokenStorage.set('existing-token')
    vi.mocked(fetch).mockResolvedValueOnce(jsonResponse(ME))
    const { result } = renderHook(() => useAuth(), { wrapper: AuthProvider })
    await waitFor(() => expect(result.current.user).toEqual(ME))

    act(() => result.current.logout())

    expect(result.current.user).toBeNull()
    expect(tokenStorage.get()).toBeNull()
  })
})
