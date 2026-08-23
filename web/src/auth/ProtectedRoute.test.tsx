import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { ProtectedRoute } from '@/auth/ProtectedRoute'
import { tokenStorage } from '@/lib/api'
import { renderWithProviders } from '@/test/utils'
import type { WhoAmIResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function TestRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<div>login page</div>} />
      <Route
        path="/"
        element={
          <ProtectedRoute>
            <div>protected content</div>
          </ProtectedRoute>
        }
      />
      <Route
        path="/admin"
        element={
          <ProtectedRoute minRole="admin">
            <div>admin content</div>
          </ProtectedRoute>
        }
      />
    </Routes>
  )
}

describe('ProtectedRoute', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('redirects to /login when there is no session', async () => {
    renderWithProviders(<TestRoutes />, { route: '/' })
    await waitFor(() => expect(screen.getByText('login page')).toBeInTheDocument())
  })

  it('renders the protected content once authenticated', async () => {
    tokenStorage.set('tok')
    vi.mocked(fetch).mockResolvedValueOnce(
      jsonResponse({ id: '1', email: 'a@b.com', name: 'Ada', avatar_url: '', org_role: 'viewer', oauth_provider: '' } satisfies WhoAmIResponse),
    )
    renderWithProviders(<TestRoutes />, { route: '/' })
    await waitFor(() => expect(screen.getByText('protected content')).toBeInTheDocument())
  })

  it('blocks a route gated on a higher role than the user has', async () => {
    tokenStorage.set('tok')
    vi.mocked(fetch).mockResolvedValueOnce(
      jsonResponse({ id: '1', email: 'a@b.com', name: 'Ada', avatar_url: '', org_role: 'viewer', oauth_provider: '' } satisfies WhoAmIResponse),
    )
    renderWithProviders(<TestRoutes />, { route: '/admin' })
    await waitFor(() =>
      expect(screen.getByText(/don't have permission/i)).toBeInTheDocument(),
    )
    expect(screen.queryByText('admin content')).not.toBeInTheDocument()
  })
})
