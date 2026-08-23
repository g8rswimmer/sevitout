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
      </Route>

      <Route path="/404" element={<NotFoundPage />} />
      <Route path="*" element={<Navigate to="/404" replace />} />
    </Routes>
  )
}
