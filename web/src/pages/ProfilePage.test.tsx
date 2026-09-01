import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ProfilePage } from '@/pages/ProfilePage'
import { tokenStorage } from '@/lib/api'
import { renderWithProviders } from '@/test/utils'
import type { WhoAmIResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function me(overrides: Partial<WhoAmIResponse> = {}): WhoAmIResponse {
  return { id: 'u1', email: 'alice@example.com', name: 'Alice', org_role: 'responder', ...overrides }
}

function mockFetchFor(whoAmI: WhoAmIResponse, onPatch?: (body: unknown) => WhoAmIResponse) {
  let current = whoAmI
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/auth/me' && method === 'GET') return Promise.resolve(jsonResponse(current))
      if (url === '/v1/auth/me' && method === 'PATCH') {
        const body = JSON.parse(String(init?.body))
        current = onPatch ? onPatch(body) : { ...current, ...body }
        return Promise.resolve(jsonResponse(current))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    }),
  )
}

describe('ProfilePage', () => {
  beforeEach(() => {
    localStorage.clear()
    tokenStorage.set('tok')
  })
  afterEach(() => vi.unstubAllGlobals())

  it('pre-fills the form from WhoAmI and shows read-only name/email', async () => {
    mockFetchFor(me({ slack_user_id: 'U123', github_username: 'alice-gh', jira_account_id: 'acc-1' }))
    renderWithProviders(<ProfilePage />)

    await waitFor(() => expect(screen.getByLabelText(/slack user id/i)).toHaveValue('U123'))
    expect(screen.getByLabelText(/github username/i)).toHaveValue('alice-gh')
    expect(screen.getByLabelText(/jira account id/i)).toHaveValue('acc-1')
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('alice@example.com')).toBeInTheDocument()
  })

  it('saves all three fields as a full-replace request', async () => {
    mockFetchFor(me({}))
    renderWithProviders(<ProfilePage />)

    await waitFor(() => expect(screen.getByLabelText(/slack user id/i)).toBeInTheDocument())
    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/slack user id/i), 'U999')
    await user.type(screen.getByLabelText(/github username/i), 'bob-gh')
    await user.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/auth/me' && init?.method === 'PATCH')
      expect(call).toBeDefined()
    })
    const [, init] = vi.mocked(fetch).mock.calls.find(([url, i]) => String(url) === '/v1/auth/me' && i?.method === 'PATCH')!
    const body = JSON.parse(String(init!.body))
    expect(body).toEqual({ slack_user_id: 'U999', github_username: 'bob-gh', jira_account_id: '' })
    expect(await screen.findByText('Saved.')).toBeInTheDocument()
  })

  it('clearing a field and saving sends an empty string, not the old value', async () => {
    mockFetchFor(me({ slack_user_id: 'U123' }))
    renderWithProviders(<ProfilePage />)

    await waitFor(() => expect(screen.getByLabelText(/slack user id/i)).toHaveValue('U123'))
    const user = userEvent.setup()
    await user.clear(screen.getByLabelText(/slack user id/i))
    await user.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/auth/me' && init?.method === 'PATCH')
      expect(call).toBeDefined()
    })
    const [, init] = vi.mocked(fetch).mock.calls.find(([url, i]) => String(url) === '/v1/auth/me' && i?.method === 'PATCH')!
    const body = JSON.parse(String(init!.body))
    expect(body.slack_user_id).toBe('')
  })
})
