import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { useAuth } from '@/auth/useAuth'
import type { OrgRole } from '@/types/api'
import { hasRole } from '@/types/api'

/** Wraps a route element: redirects to /login when unauthenticated, and
 * optionally gates on a minimum org role (mirrors internal/auth/rbac.go's
 * hierarchy — the server is still the source of truth; this only avoids
 * rendering UI the user's role can't act on). */
export function ProtectedRoute({
  children,
  minRole,
}: {
  children: ReactNode
  minRole?: OrgRole
}) {
  const { user, loading } = useAuth()
  const location = useLocation()

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center text-muted-foreground">
        Loading…
      </div>
    )
  }

  if (!user) {
    return <Navigate to="/login" state={{ from: location }} replace />
  }

  if (minRole && !hasRole(user.org_role, minRole)) {
    return (
      <div className="flex h-screen items-center justify-center text-muted-foreground">
        You don't have permission to view this page.
      </div>
    )
  }

  return <>{children}</>
}
