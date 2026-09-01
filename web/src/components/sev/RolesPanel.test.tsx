import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RolesPanel } from '@/components/sev/RolesPanel'
import { renderWithProviders } from '@/test/utils'
import type { SEVRoleResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

/** Matches useEnabledIntegrations' request (Roadmap Phase 11b) — every test
 * below must handle this URL, since the hook fires unconditionally on
 * mount regardless of canManage. */
function enabledIntegrationsHandler(types: string[]) {
  return (url: string) => (url === '/v1/config/enabled-integrations' ? jsonResponse({ enabled_types: types }) : null)
}

const SEV_ID = 'SEV-2026-0001'

function role(overrides: Partial<SEVRoleResponse>): SEVRoleResponse {
  return {
    id: '1',
    sev_id: SEV_ID,
    role_type: 'responder',
    display_name: 'Carol',
    created_at: '2026-08-23T00:00:00Z',
    ...overrides,
  }
}

describe('RolesPanel', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('hides management controls when canManage is false', async () => {
    const enabled = enabledIntegrationsHandler(['slack'])
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      if (url === `/v1/sevs/${SEV_ID}/roles`) return Promise.resolve(jsonResponse({ roles: [role({})] }))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    renderWithProviders(<RolesPanel sevId={SEV_ID} canManage={false} />)

    await screen.findByText('Carol')
    expect(screen.queryByRole('button', { name: /assign/i })).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/remove/i)).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/add .* to slack channel/i)).not.toBeInTheDocument()
  })

  it('hides "Add to chat" entirely when the slack integration is not enabled, even with a channel recorded', async () => {
    const enabled = enabledIntegrationsHandler([])
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      if (url === `/v1/sevs/${SEV_ID}/roles`) return Promise.resolve(jsonResponse({ roles: [role({})] }))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    renderWithProviders(<RolesPanel sevId={SEV_ID} canManage slackChannelId="C123" />)

    await screen.findByText('Carol')
    expect(screen.queryByLabelText(/add .* to slack channel/i)).not.toBeInTheDocument()
  })

  it('disables "Add to chat" when the SEV has no Slack channel, enables it when it does', async () => {
    const enabled = enabledIntegrationsHandler(['slack'])
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      if (url === `/v1/sevs/${SEV_ID}/roles`) return Promise.resolve(jsonResponse({ roles: [role({})] }))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    const { rerender } = renderWithProviders(<RolesPanel sevId={SEV_ID} canManage />)

    const button = await screen.findByRole('button', { name: /add .* to slack channel/i })
    expect(button).toBeDisabled()

    rerender(<RolesPanel sevId={SEV_ID} canManage slackChannelId="C123" />)
    await waitFor(() => expect(screen.getByRole('button', { name: /add .* to slack channel/i })).not.toBeDisabled())
  })

  it('calls the invite-to-slack endpoint for the clicked role', async () => {
    const enabled = enabledIntegrationsHandler(['slack'])
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      if (url === `/v1/sevs/${SEV_ID}/roles` && method === 'GET') return Promise.resolve(jsonResponse({ roles: [role({ id: '7' })] }))
      if (url === `/v1/sevs/${SEV_ID}/roles/7/invite-to-slack` && method === 'POST') return Promise.resolve(jsonResponse({}))
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<RolesPanel sevId={SEV_ID} canManage slackChannelId="C123" />)

    await screen.findByText('Carol')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /add .* to slack channel/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === `/v1/sevs/${SEV_ID}/roles/7/invite-to-slack`)
      expect(call).toBeDefined()
    })
  })

  it('shows the server error message when invite-to-slack fails', async () => {
    const enabled = enabledIntegrationsHandler(['slack'])
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      if (url === `/v1/sevs/${SEV_ID}/roles` && method === 'GET') return Promise.resolve(jsonResponse({ roles: [role({ id: '7' })] }))
      if (url === `/v1/sevs/${SEV_ID}/roles/7/invite-to-slack` && method === 'POST') {
        return Promise.resolve(jsonResponse({ message: 'this role holder has no resolvable Slack identity' }, 400))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<RolesPanel sevId={SEV_ID} canManage slackChannelId="C123" />)

    await screen.findByText('Carol')
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /add .* to slack channel/i }))

    expect(await screen.findByText(/no resolvable slack identity/i)).toBeInTheDocument()
  })

  it('picking a directory user sets user_id on the assign request and pre-fills the name', async () => {
    const enabled = enabledIntegrationsHandler([])
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      if (url === `/v1/sevs/${SEV_ID}/roles` && method === 'GET') return Promise.resolve(jsonResponse({ roles: [] }))
      if (url.startsWith('/v1/auth/directory') && method === 'GET') {
        return Promise.resolve(jsonResponse({ users: [{ id: 'user-9', name: 'Dave', email: 'dave@example.com' }] }))
      }
      if (url === `/v1/sevs/${SEV_ID}/roles` && method === 'POST') {
        return Promise.resolve(jsonResponse(role({ id: '2', display_name: 'Dave' })))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<RolesPanel sevId={SEV_ID} canManage />)

    await screen.findByText('No roles assigned yet.')
    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/search users to link/i), 'dav')

    const match = await screen.findByText('Dave')
    await user.click(match)

    // The free-text name field is pre-filled from the picked user.
    expect(screen.getByLabelText(/person's name/i)).toHaveValue('Dave')

    await user.click(screen.getByRole('button', { name: /^assign$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(
        ([url, init]) => String(url) === `/v1/sevs/${SEV_ID}/roles` && (init?.method ?? 'GET') === 'POST',
      )
      expect(call).toBeDefined()
    })
    const [, init] = vi.mocked(fetch).mock.calls.find(
      ([url, i]) => String(url) === `/v1/sevs/${SEV_ID}/roles` && (i?.method ?? 'GET') === 'POST',
    )!
    const body = JSON.parse(String(init!.body))
    expect(body.user_id).toBe('user-9')
  })

  it('free-text assignment still works without picking a directory user', async () => {
    const enabled = enabledIntegrationsHandler([])
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      if (url === `/v1/sevs/${SEV_ID}/roles` && method === 'GET') return Promise.resolve(jsonResponse({ roles: [] }))
      if (url === `/v1/sevs/${SEV_ID}/roles` && method === 'POST') {
        return Promise.resolve(jsonResponse(role({ id: '3', display_name: 'Free Text Name' })))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<RolesPanel sevId={SEV_ID} canManage />)

    await screen.findByText('No roles assigned yet.')
    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/person's name/i), 'Free Text Name')
    await user.click(screen.getByRole('button', { name: /^assign$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(
        ([url, i]) => String(url) === `/v1/sevs/${SEV_ID}/roles` && (i?.method ?? 'GET') === 'POST',
      )
      expect(call).toBeDefined()
    })
    const [, init] = vi.mocked(fetch).mock.calls.find(
      ([url, i]) => String(url) === `/v1/sevs/${SEV_ID}/roles` && (i?.method ?? 'GET') === 'POST',
    )!
    const body = JSON.parse(String(init!.body))
    expect(body.user_id).toBeUndefined()
    expect(body.display_name).toBe('Free Text Name')
  })
})
