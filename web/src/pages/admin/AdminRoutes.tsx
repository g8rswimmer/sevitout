import { Navigate, Route, Routes } from 'react-router-dom'
import { AdminLayout } from '@/pages/admin/AdminLayout'
import { AdminServicesPage } from '@/pages/admin/AdminServicesPage'
import { AdminUsersPage } from '@/pages/admin/AdminUsersPage'
import { AdminOnCallPage } from '@/pages/admin/AdminOnCallPage'
import { AdminIntegrationsPage } from '@/pages/admin/AdminIntegrationsPage'
import { AdminAIPluginsPage } from '@/pages/admin/AdminAIPluginsPage'
import { AdminRetentionPage } from '@/pages/admin/AdminRetentionPage'

/** The whole /admin/* subtree, mounted as one React.lazy chunk from App.tsx
 * (same "code-split the whole feature behind a Suspense boundary" approach
 * as PostmortemPage/TipTap) — six admin pages is enough weight that everyone
 * who isn't an Admin shouldn't pay for it in the main bundle. */
export function AdminRoutes() {
  return (
    <Routes>
      <Route element={<AdminLayout />}>
        <Route index element={<Navigate to="services" replace />} />
        <Route path="services" element={<AdminServicesPage />} />
        <Route path="users" element={<AdminUsersPage />} />
        <Route path="oncall" element={<AdminOnCallPage />} />
        <Route path="integrations" element={<AdminIntegrationsPage />} />
        <Route path="ai" element={<AdminAIPluginsPage />} />
        <Route path="retention" element={<AdminRetentionPage />} />
      </Route>
    </Routes>
  )
}
