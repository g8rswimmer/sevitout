import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminUsersPage } from '@/pages/admin/AdminUsersPage'
import { renderWithProviders } from '@/test/utils'
import type { UserResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const ADA: UserResponse = {
  id: 'u1',
  email: 'ada@example.com',
  name: 'Ada Lovelace',
  org_role: 'responder',
  active: true,
  created_at: '2026-08-20T00:00:00Z',
  updated_at: '2026-08-20T00:00:00Z',
}

describe('AdminUsersPage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders the user directory', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ users: [ADA] }))
    renderWithProviders(<AdminUsersPage />)

    expect(await screen.findByText('Ada Lovelace')).toBeInTheDocument()
    expect(screen.getByText('ada@example.com')).toBeInTheDocument()
    expect(screen.getByText('Active')).toBeInTheDocument()
  })

  it('searches by query on submit', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.includes('query=ada')) return Promise.resolve(jsonResponse({ users: [ADA] }))
      return Promise.resolve(jsonResponse({ users: [] }))
    })
    renderWithProviders(<AdminUsersPage />)
    await waitFor(() => expect(fetch).toHaveBeenCalled())

    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Search users'), 'ada')
    await user.click(screen.getByRole('button', { name: /search/i }))

    await waitFor(() =>
      expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes('query=ada'))).toBe(true),
    )
  })

  it('changes a user role via the select', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url.startsWith('/v1/config/users') && method === 'GET') return Promise.resolve(jsonResponse({ users: [ADA] }))
      if (url === '/v1/config/users/u1/role' && method === 'PATCH') {
        return Promise.resolve(jsonResponse({ ...ADA, org_role: 'incident-commander' }))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })

    renderWithProviders(<AdminUsersPage />)
    await screen.findByText('Ada Lovelace')

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Role for Ada Lovelace'), 'incident-commander')

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith(
        '/v1/config/users/u1/role',
        expect.objectContaining({ method: 'PATCH', body: JSON.stringify({ org_role: 'incident-commander' }) }),
      ),
    )
  })

  it('deactivates and reactivates a user', async () => {
    let active = true
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url.startsWith('/v1/config/users') && method === 'GET')
        return Promise.resolve(jsonResponse({ users: [{ ...ADA, active }] }))
      if (url === '/v1/config/users/u1/deactivate' && method === 'POST') {
        active = false
        return Promise.resolve(jsonResponse({ ...ADA, active }))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })

    renderWithProviders(<AdminUsersPage />)
    await screen.findByText('Ada Lovelace')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^deactivate$/i }))

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith('/v1/config/users/u1/deactivate', expect.objectContaining({ method: 'POST' })),
    )
    expect(await screen.findByRole('button', { name: /^reactivate$/i })).toBeInTheDocument()
  })
})
