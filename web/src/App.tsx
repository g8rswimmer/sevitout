import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { AppLayout } from '@/components/layout/AppLayout'
import { ProtectedRoute } from '@/auth/ProtectedRoute'
import { LoginPage } from '@/pages/LoginPage'
import { RegisterPage } from '@/pages/RegisterPage'
import { DashboardPage } from '@/pages/DashboardPage'
import { SevListPage } from '@/pages/SevListPage'
import { SevCreatePage } from '@/pages/SevCreatePage'
import { SevDetailPage } from '@/pages/SevDetailPage'
import { NotFoundPage } from '@/pages/NotFoundPage'

// Lazy-loaded: TipTap + ProseMirror (the postmortem editor's dependencies)
// are the single heaviest thing in this app by far — code-splitting this
// one route keeps everyone who never opens a postmortem from downloading it.
const PostmortemPage = lazy(() => import('@/pages/PostmortemPage').then((m) => ({ default: m.PostmortemPage })))

// Lazy-loaded: the whole six-page /admin/* subtree (M14d) is only ever
// visited by Admins, so everyone else shouldn't pay for it in the main
// bundle — same reasoning as PostmortemPage above.
const AdminRoutes = lazy(() => import('@/pages/admin/AdminRoutes').then((m) => ({ default: m.AdminRoutes })))

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />

      <Route
        element={
          <ProtectedRoute>
            <AppLayout />
          </ProtectedRoute>
        }
      >
        <Route path="/" element={<DashboardPage />} />
        <Route path="/sevs" element={<SevListPage />} />
        <Route
          path="/sevs/new"
          element={
            <ProtectedRoute minRole="responder">
              <SevCreatePage />
            </ProtectedRoute>
          }
        />
        <Route path="/sevs/:id" element={<SevDetailPage />} />
        <Route
          path="/sevs/:id/postmortem"
          element={
            <Suspense fallback={<div className="p-4 text-sm text-muted-foreground">Loading…</div>}>
              <PostmortemPage />
            </Suspense>
          }
        />

        <Route
          path="/admin/*"
          element={
            <ProtectedRoute minRole="admin">
              <Suspense fallback={<div className="p-4 text-sm text-muted-foreground">Loading…</div>}>
                <AdminRoutes />
              </Suspense>
            </ProtectedRoute>
          }
        />
      </Route>

      <Route path="/404" element={<NotFoundPage />} />
      <Route path="*" element={<Navigate to="/404" replace />} />
    </Routes>
  )
}
