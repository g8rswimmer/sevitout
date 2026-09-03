import { NavLink, Outlet } from 'react-router-dom'
import { cn } from '@/lib/utils'

const ADMIN_TABS = [
  { to: '/admin/services', label: 'Services' },
  { to: '/admin/users', label: 'Users' },
  { to: '/admin/oncall', label: 'On-Call' },
  { to: '/admin/integrations', label: 'Integrations' },
  { to: '/admin/ai', label: 'AI Plugins' },
  { to: '/admin/retention', label: 'Retention' },
  { to: '/admin/notifications', label: 'Notifications' },
]

/** Shared shell for every /admin/* page (docs/project-plan.md M14d) — a tab
 * strip over an <Outlet/>, the same "layout route wraps children" pattern
 * AppLayout already uses for the whole app. The whole /admin subtree is
 * already gated to the Admin org role by App.tsx's ProtectedRoute, so
 * nothing here re-checks it. */
export function AdminLayout() {
  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold">Admin</h1>
        <p className="text-sm text-muted-foreground">
          Service registry, users, on-call, integrations, AI, data retention, and notifications.
        </p>
      </div>
      <nav className="flex flex-wrap gap-1 border-b border-border" aria-label="Admin sections">
        {ADMIN_TABS.map(({ to, label }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              cn(
                'rounded-t-md px-3 py-2 text-sm font-medium transition-colors',
                isActive
                  ? 'border-b-2 border-primary text-foreground'
                  : 'text-muted-foreground hover:text-foreground',
              )
            }
          >
            {label}
          </NavLink>
        ))}
      </nav>
      <Outlet />
    </div>
  )
}
