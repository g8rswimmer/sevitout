import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminIntegrationsPage } from '@/pages/admin/AdminIntegrationsPage'
import { renderWithProviders } from '@/test/utils'
import type { IntegrationConfigResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const PAGERDUTY: IntegrationConfigResponse = {
  integration_type: 'pagerduty',
  credentials_configured: true,
  created_at: '2026-08-23T20:00:00Z',
  updated_at: '2026-08-23T20:00:00Z',
}

function mockFetch(configs: IntegrationConfigResponse[] = [PAGERDUTY]) {
  vi.mocked(fetch).mockImplementation((input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/v1/config/integrations') return Promise.resolve(jsonResponse({ configs }))
    if (url === '/admin/integrations/health')
      return Promise.resolve(jsonResponse({ integrations: [{ integration_type: 'pagerduty', status: 'connected' }] }))
    if (url === '/v1/config/integrations/pagerduty' && method === 'PUT') {
      return Promise.resolve(jsonResponse({ ...PAGERDUTY }))
    }
    if (url === '/v1/config/integrations/jira' && method === 'PUT') {
      const body = JSON.parse(String(init?.body))
      return Promise.resolve(
        jsonResponse({ integration_type: 'jira', credentials_configured: true, settings: body.settings }),
      )
    }
    if (url === '/v1/config/integrations/slack' && method === 'PUT') {
      const body = JSON.parse(String(init?.body))
      return Promise.resolve(
        jsonResponse({ integration_type: 'slack', credentials_configured: true, settings: body.settings }),
      )
    }
    if (url === '/v1/config/integrations/datadog' && method === 'PUT') {
      const body = JSON.parse(String(init?.body))
      return Promise.resolve(jsonResponse({ integration_type: 'datadog', credentials_configured: true, settings: body.settings }))
    }
    return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
  })
}

describe('AdminIntegrationsPage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders configured integrations with health status', async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)

    expect(await screen.findByText('PagerDuty')).toBeInTheDocument()
    expect(await screen.findByText('Connected')).toBeInTheDocument()
    expect(screen.getByText('Configured')).toBeInTheDocument()
  })

  it('saves a known integration type with its well-known credential key pre-filled', async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)
    await screen.findByText('PagerDuty')

    expect(screen.getByDisplayValue('api_key')).toBeInTheDocument()
    // PagerDuty has no gap between "credential saved here" and "credential
    // actually used" the way Slack does, so it must not show Slack's note.
    expect(screen.queryByText(/periodically pulls bot_token\/app_token from here/)).not.toBeInTheDocument()

    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Tag value'), 'secret-key')
    await user.click(screen.getByRole('button', { name: /save integration/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/integrations/pagerduty' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/integrations/pagerduty' && init?.method === 'PUT')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({
      integration_type: 'pagerduty',
      credentials: { api_key: 'secret-key' },
    })
  })

  it('includes Jira in the type dropdown with its well-known credential and settings keys pre-filled', async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)
    await screen.findByText('PagerDuty')

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Type'), 'Jira')

    expect(screen.getByDisplayValue('api_token')).toBeInTheDocument()
    expect(screen.getByDisplayValue('cloud_id')).toBeInTheDocument()
    // site_url is optional (cosmetic browse-link generation only) but still
    // shown as its own editable row, not just mentioned in the hint text.
    expect(screen.getByText(/"site_url" \(optional\)/)).toBeInTheDocument()
    expect(screen.getByDisplayValue('site_url')).toBeInTheDocument()

    // The credentials section (1 row: api_token) and the settings section
    // (2 rows: cloud_id, site_url) each render "Tag value" inputs once Jira
    // is selected — the first belongs to the credentials row.
    const [credentialValueInput] = screen.getAllByLabelText('Tag value')
    await user.type(credentialValueInput, 'jira-secret-token')
    await user.click(screen.getByRole('button', { name: /save integration/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/integrations/jira' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/integrations/jira' && init?.method === 'PUT')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({
      integration_type: 'jira',
      credentials: { api_token: 'jira-secret-token' },
    })
  })

  it("includes Slack's optional settings keys as editable rows, not just credentials", async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)
    await screen.findByText('PagerDuty')

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Type'), 'Slack')

    expect(screen.getByDisplayValue('bot_token')).toBeInTheDocument()
    // app_token is Slack's second well-known credential key (Phase 8), also
    // pre-filled as its own row, not just mentioned in the hint text.
    expect(screen.getByDisplayValue('app_token')).toBeInTheDocument()
    expect(screen.getByDisplayValue('default_channel')).toBeInTheDocument()
    expect(screen.getByDisplayValue('channel_naming_convention')).toBeInTheDocument()
    expect(screen.getByText(/"default_channel" \(optional\)/)).toBeInTheDocument()
    expect(screen.getByText(/"channel_naming_convention" \(optional\)/)).toBeInTheDocument()
    // Since Phase 8, this credential does reach the running bot's REST
    // client periodically — the form must say so, and must still flag that
    // Socket Mode (slash commands/@mentions) needs a restart.
    expect(screen.getByText(/periodically pulls bot_token\/app_token from here/)).toBeInTheDocument()
    expect(screen.getByText(/Socket Mode .* requires a restart/)).toBeInTheDocument()

    // Credentials section (2 rows: bot_token, app_token) + settings section
    // (2 rows: default_channel, channel_naming_convention) — the first two
    // "Tag value" inputs belong to the credentials rows.
    const [botTokenInput, appTokenInput] = screen.getAllByLabelText('Tag value')
    await user.type(botTokenInput, 'xoxb-slack-secret')
    await user.type(appTokenInput, 'xapp-slack-secret')
    await user.click(screen.getByRole('button', { name: /save integration/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/integrations/slack' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/integrations/slack' && init?.method === 'PUT')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({
      integration_type: 'slack',
      credentials: { bot_token: 'xoxb-slack-secret', app_token: 'xapp-slack-secret' },
    })
  })

  it('supports a custom "Other" integration type', async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)
    await screen.findByText('PagerDuty')

    const user = userEvent.setup()
    await user.selectOptions(screen.getByLabelText('Type'), 'Other…')
    await user.type(screen.getByLabelText('Custom type name'), 'datadog')
    await user.click(screen.getByRole('button', { name: /save integration/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/integrations/datadog' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
  })
})
