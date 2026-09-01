import { createContext, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, setUnauthorizedHandler, tokenStorage } from '@/lib/api'
import type { WhoAmIResponse } from '@/types/api'

interface AuthContextValue {
  user: WhoAmIResponse | null
  /** True while the initial WhoAmI hydration (on load, or after a stored
   * token is found) is in flight. Callers should not redirect to /login
   * until this settles, or a refreshed page would bounce a logged-in user. */
  loading: boolean
  login: (email: string, password: string) => Promise<void>
  register: (email: string, name: string, password: string) => Promise<void>
  logout: () => void
  /** Re-fetches WhoAmI and updates the cached user — called after
   * UpdateMyIntegrationIdentities (ProfilePage) so components reading
   * useAuth().user (e.g. TasksPanel's assignee pre-fill) see the change
   * immediately instead of only after a reload. */
  refreshUser: () => Promise<void>
}

export const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<WhoAmIResponse | null>(null)
  const [loading, setLoading] = useState(true)

  const logout = useCallback(() => {
    tokenStorage.clear()
    setUser(null)
  }, [])

  // Hydrate from a stored token on first load by re-validating it against
  // WhoAmI, rather than trusting a decoded-but-possibly-stale/expired token.
  useEffect(() => {
    let cancelled = false
    async function hydrate() {
      if (!tokenStorage.get()) {
        setLoading(false)
        return
      }
      try {
        const me = await api.auth.whoAmI()
        if (!cancelled) setUser(me)
      } catch {
        if (!cancelled) tokenStorage.clear()
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void hydrate()
    return () => {
      cancelled = true
    }
  }, [])

  // Any request that comes back 401 (expired/invalid token) drops the
  // session — wired once, independent of React re-renders.
  useEffect(() => {
    setUnauthorizedHandler(logout)
    return () => setUnauthorizedHandler(null)
  }, [logout])

  const login = useCallback(async (email: string, password: string) => {
    const res = await api.auth.login(email, password)
    tokenStorage.set(res.token)
    const me = await api.auth.whoAmI()
    setUser(me)
  }, [])

  const register = useCallback(async (email: string, name: string, password: string) => {
    const res = await api.auth.register(email, name, password)
    tokenStorage.set(res.token)
    const me = await api.auth.whoAmI()
    setUser(me)
  }, [])

  const refreshUser = useCallback(async () => {
    if (!tokenStorage.get()) return
    const me = await api.auth.whoAmI()
    setUser(me)
  }, [])

  const value = useMemo(
    () => ({ user, loading, login, register, logout, refreshUser }),
    [user, loading, login, register, logout, refreshUser],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
