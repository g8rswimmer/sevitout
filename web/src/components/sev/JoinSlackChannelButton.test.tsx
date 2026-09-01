import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { JoinSlackChannelButton } from '@/components/sev/JoinSlackChannelButton'
import { renderWithProviders } from '@/test/utils'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

/** Matches useEnabledIntegrations' request (Roadmap Phase 11b) — every test
 * below must handle this URL, since the hook fires unconditionally on
 * mount. */
function enabledIntegrationsHandler(types: string[]) {
  return (url: string) => (url === '/v1/config/enabled-integrations' ? jsonResponse({ enabled_types: types }) : null)
}

const SEV_ID = 'SEV-2026-0001'

describe('JoinSlackChannelButton', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders nothing when the slack integration is not enabled, even with a channel recorded', async () => {
    const enabled = enabledIntegrationsHandler([])
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    renderWithProviders(<JoinSlackChannelButton sevId={SEV_ID} slackChannelId="C123" />)

    // Wait for the enabled-integrations fetch to actually resolve, then
    // confirm the button never appears.
    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === '/v1/config/enabled-integrations')
      expect(call).toBeDefined()
    })
    await waitFor(() => expect(screen.queryByRole('button')).not.toBeInTheDocument())
  })

  it('renders disabled (not hidden) on an SEV with no recorded channel, so older SEVs still show the action', async () => {
    const enabled = enabledIntegrationsHandler(['slack'])
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    renderWithProviders(<JoinSlackChannelButton sevId={SEV_ID} />)

    const button = await screen.findByRole('button', { name: /join slack channel/i })
    expect(button).toBeDisabled()
  })

  it('is enabled when the SEV has a recorded channel, and calls the join-slack-channel endpoint', async () => {
    const enabled = enabledIntegrationsHandler(['slack'])
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      if (url === `/v1/sevs/${SEV_ID}/join-slack-channel` && method === 'POST') return Promise.resolve(jsonResponse({}))
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<JoinSlackChannelButton sevId={SEV_ID} slackChannelId="C123" />)

    const button = await screen.findByRole('button', { name: /join slack channel/i })
    expect(button).not.toBeDisabled()

    const user = userEvent.setup()
    await user.click(button)

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === `/v1/sevs/${SEV_ID}/join-slack-channel`)
      expect(call).toBeDefined()
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /joined/i })).toBeInTheDocument()
  })

  it('shows the server error message when joining fails', async () => {
    const enabled = enabledIntegrationsHandler(['slack'])
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      const enabledResp = enabled(url)
      if (enabledResp) return Promise.resolve(enabledResp)
      if (url === `/v1/sevs/${SEV_ID}/join-slack-channel` && method === 'POST') {
        return Promise.resolve(jsonResponse({ message: 'no Slack identity on file — set one in your profile' }, 400))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<JoinSlackChannelButton sevId={SEV_ID} slackChannelId="C123" />)

    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /join slack channel/i }))

    expect(await screen.findByText(/no slack identity on file/i)).toBeInTheDocument()
  })
})
