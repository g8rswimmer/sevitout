import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { AppLayout } from '@/components/layout/AppLayout'
import { tokenStorage } from '@/lib/api'
import { renderWithProviders } from '@/test/utils'
import type { WhoAmIResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function renderLayout() {
  return renderWithProviders(
    <Routes>
      <Route element={<AppLayout />}>
        <Route path="/" element={<div>home</div>} />
      </Route>
    </Routes>,
    { route: '/' },
  )
}

describe('AppLayout', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('does not show the Admin nav link to a non-admin user', async () => {
    tokenStorage.set('tok')
    vi.mocked(fetch).mockResolvedValueOnce(
      jsonResponse({ id: '1', email: 'a@b.com', name: 'Ada', org_role: 'responder' } satisfies WhoAmIResponse),
    )
    renderLayout()

    await waitFor(() => expect(screen.getByText('Ada')).toBeInTheDocument())
    expect(screen.queryByRole('link', { name: /admin/i })).not.toBeInTheDocument()
  })

  it('shows the Admin nav link to an admin user', async () => {
    tokenStorage.set('tok')
    vi.mocked(fetch).mockResolvedValueOnce(
      jsonResponse({ id: '1', email: 'a@b.com', name: 'Ada', org_role: 'admin' } satisfies WhoAmIResponse),
    )
    renderLayout()

    expect(await screen.findByRole('link', { name: /admin/i })).toHaveAttribute('href', '/admin')
  })
})
