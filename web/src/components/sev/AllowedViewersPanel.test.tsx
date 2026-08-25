import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AllowedViewersPanel } from '@/components/sev/AllowedViewersPanel'
import { renderWithProviders } from '@/test/utils'
import type { ListAccessResponse, ListUsersResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const SEV_ID = 'SEV-2026-0001'

const GRANTS: ListAccessResponse = {
  access: [{ id: '1', sev_id: SEV_ID, user_id: 'user-alice', created_at: '2026-08-23T00:00:00Z', created_by: 'user-admin' }],
}

const USERS: ListUsersResponse = {
  users: [
    { id: 'user-alice', email: 'alice@example.com', name: 'Alice', org_role: 'viewer', created_at: '2026-08-01T00:00:00Z', updated_at: '2026-08-01T00:00:00Z' },
    { id: 'user-bob', email: 'bob@example.com', name: 'Bob', org_role: 'viewer', created_at: '2026-08-01T00:00:00Z', updated_at: '2026-08-01T00:00:00Z' },
  ],
}

describe('AllowedViewersPanel', () => {
  beforeEach(() => {
    // No stored token, same rationale as ShareLinkControl.test.tsx: sidesteps
    // AuthProvider's own whoAmI() fetch on mount consuming the shared mocked
    // Response body before this component's own calls do.
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('lists granted users read-only when canManage is false, without fetching the user directory', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url === `/v1/sevs/${SEV_ID}/access`) return Promise.resolve(jsonResponse(GRANTS))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    renderWithProviders(<AllowedViewersPanel sevId={SEV_ID} canManage={false} />)

    // The user directory (Admin-gated) is only fetched when canManage, so a
    // non-managing viewer sees the raw user_id rather than a resolved name.
    expect(await screen.findByText('user-alice')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /revoke access/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: /user to grant access/i })).not.toBeInTheDocument()
    expect(vi.mocked(fetch).mock.calls.some(([u]) => String(u) === '/v1/config/users')).toBe(false)
  })

  it('shows a fallback message when no one has been granted access yet', async () => {
    vi.mocked(fetch).mockImplementation(() => Promise.resolve(jsonResponse({})))
    renderWithProviders(<AllowedViewersPanel sevId={SEV_ID} canManage={false} />)

    expect(await screen.findByText(/no one has been explicitly granted access yet/i)).toBeInTheDocument()
  })

  it('grants access to a selected user when canManage is true', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === `/v1/sevs/${SEV_ID}/access` && method === 'GET') return Promise.resolve(jsonResponse({}))
      if (url === '/v1/config/users' && method === 'GET') return Promise.resolve(jsonResponse(USERS))
      if (url === `/v1/sevs/${SEV_ID}/access` && method === 'POST') {
        const body = JSON.parse(String(init?.body))
        expect(body).toEqual({ user_id: 'user-bob' })
        return Promise.resolve(
          jsonResponse({ id: '2', sev_id: SEV_ID, user_id: 'user-bob', created_at: '2026-08-24T00:00:00Z', created_by: 'user-ic' }),
        )
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<AllowedViewersPanel sevId={SEV_ID} canManage />)

    const select = await screen.findByRole('combobox', { name: /user to grant access/i })
    // The user directory loads asynchronously; wait for Bob's <option> to
    // actually be present before selecting it, not just for the <select>
    // element itself (which renders immediately with only the placeholder).
    await screen.findByRole('option', { name: /bob/i })
    const user = userEvent.setup()
    await user.selectOptions(select, 'user-bob')
    await user.click(screen.getByRole('button', { name: /grant access/i }))

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith(
        `/v1/sevs/${SEV_ID}/access`,
        expect.objectContaining({ method: 'POST' }),
      ),
    )
  })

  it('revokes a grant when canManage is true', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === `/v1/sevs/${SEV_ID}/access` && method === 'GET') return Promise.resolve(jsonResponse(GRANTS))
      if (url === '/v1/config/users' && method === 'GET') return Promise.resolve(jsonResponse(USERS))
      if (url === `/v1/sevs/${SEV_ID}/access/1` && method === 'DELETE') return Promise.resolve(new Response(null, { status: 204 }))
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<AllowedViewersPanel sevId={SEV_ID} canManage />)

    const revokeButton = await screen.findByRole('button', { name: /revoke access for alice/i })
    const user = userEvent.setup()
    await user.click(revokeButton)

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith(
        `/v1/sevs/${SEV_ID}/access/1`,
        expect.objectContaining({ method: 'DELETE' }),
      ),
    )
  })

  it('shows the server error message on a failed grant', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === `/v1/sevs/${SEV_ID}/access` && method === 'GET') return Promise.resolve(jsonResponse({}))
      if (url === '/v1/config/users' && method === 'GET') return Promise.resolve(jsonResponse(USERS))
      if (method === 'POST') return Promise.resolve(jsonResponse({ message: 'access already granted' }, 409))
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<AllowedViewersPanel sevId={SEV_ID} canManage />)

    const select = await screen.findByRole('combobox', { name: /user to grant access/i })
    await screen.findByRole('option', { name: /alice/i })
    const user = userEvent.setup()
    await user.selectOptions(select, 'user-alice')
    await user.click(screen.getByRole('button', { name: /grant access/i }))

    expect(await screen.findByText(/access already granted/i)).toBeInTheDocument()
  })
})
