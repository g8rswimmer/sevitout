import { Link, Outlet, useLocation } from 'react-router-dom'
import { LayoutDashboard, ListChecks, LogOut, Siren } from 'lucide-react'
import { useAuth } from '@/auth/useAuth'
import { Button } from '@/components/ui/button'
import { ORG_ROLE_LABELS } from '@/types/api'

const NAV_ITEMS = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/sevs', label: 'SEVs', icon: ListChecks },
]

function isActive(pathname: string, to: string): boolean {
  return pathname === to || (to !== '/' && pathname.startsWith(`${to}/`))
}

/** Shared shell: top nav, breadcrumb slot, user menu. Every authenticated
 * route renders inside this via <Outlet/> (see App.tsx's route tree). */
export function AppLayout() {
  const { user, logout } = useAuth()
  const location = useLocation()

  return (
    <div className="flex min-h-screen flex-col">
      <header className="border-b border-border bg-card">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between px-4">
          <div className="flex items-center gap-6">
            <Link to="/" className="flex items-center gap-2 font-semibold">
              <Siren className="h-5 w-5 text-primary" aria-hidden />
              Sevitout
            </Link>
            <nav className="flex items-center gap-1" aria-label="Primary">
              {NAV_ITEMS.map(({ to, label, icon: Icon }) => {
                const active = isActive(location.pathname, to)
                return (
                  <Link
                    key={to}
                    to={to}
                    aria-current={active ? 'page' : undefined}
                    className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                      active
                        ? 'bg-accent text-accent-foreground'
                        : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                    }`}
                  >
                    <Icon className="h-4 w-4" aria-hidden />
                    {label}
                  </Link>
                )
              })}
            </nav>
          </div>

          {user && (
            <div className="flex items-center gap-3">
              <div className="text-right text-sm leading-tight">
                <div className="font-medium">{user.name || user.email}</div>
                <div className="text-xs text-muted-foreground">{ORG_ROLE_LABELS[user.org_role]}</div>
              </div>
              <Button variant="ghost" size="icon" onClick={logout} aria-label="Log out" title="Log out">
                <LogOut className="h-4 w-4" />
              </Button>
            </div>
          )}
        </div>
      </header>

      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
        <Outlet />
      </main>
    </div>
  )
}
