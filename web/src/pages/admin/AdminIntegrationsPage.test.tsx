import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminIntegrationsPage } from '@/pages/admin/AdminIntegrationsPage'
import { renderWithProviders } from '@/test/utils'
import type { GetIntegrationCatalogResponse, IntegrationConfigResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

// Mirrors internal/integrations/catalog.All exactly (Roadmap Phase 9).
const CATALOG: GetIntegrationCatalogResponse = {
  integrations: [
    {
      type: 'pagerduty',
      label: 'PagerDuty',
      credential_fields: [{ key: 'api_key', label: 'API Key', kind: 'secret', required: true }],
    },
    {
      type: 'github',
      label: 'GitHub',
      credential_fields: [{ key: 'token', label: 'Token', kind: 'secret', required: true }],
    },
    {
      type: 'slack',
      label: 'Slack',
      credential_fields: [
        { key: 'bot_token', label: 'Bot Token', kind: 'secret', required: true },
        { key: 'app_token', label: 'App Token', kind: 'secret', required: true },
      ],
      settings_fields: [
        { key: 'default_channel', label: 'Default Channel', kind: 'text' },
        { key: 'channel_naming_convention', label: 'Channel Naming Convention', kind: 'text' },
      ],
    },
    {
      type: 'jira',
      label: 'Jira',
      credential_fields: [{ key: 'api_token', label: 'API Token', kind: 'secret', required: true }],
      settings_fields: [
        { key: 'cloud_id', label: 'Cloud ID', kind: 'text', required: true },
        { key: 'site_url', label: 'Site URL', kind: 'text' },
      ],
    },
    {
      type: 'monitoring',
      label: 'Monitoring',
      settings_fields: [
        { key: 'tool', label: 'Tool', kind: 'select', required: true, options: ['datadog', 'prometheus', 'cloudwatch'] },
        { key: 'base_url', label: 'Base URL', kind: 'text' },
      ],
    },
  ],
}

const PAGERDUTY_CONFIG: IntegrationConfigResponse = {
  integration_type: 'pagerduty',
  credentials_configured: true,
  created_at: '2026-08-23T20:00:00Z',
  updated_at: '2026-08-23T20:00:00Z',
}

function mockFetch(opts?: {
  configs?: IntegrationConfigResponse[]
  upsertHandler?: (integrationType: string, body: unknown) => Response
}) {
  const configs = opts?.configs ?? [PAGERDUTY_CONFIG]
  vi.mocked(fetch).mockImplementation((input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/v1/config/integration-catalog') return Promise.resolve(jsonResponse(CATALOG))
    if (url === '/v1/config/integrations') return Promise.resolve(jsonResponse({ configs }))
    if (url === '/admin/integrations/health')
      return Promise.resolve(jsonResponse({ integrations: [{ integration_type: 'pagerduty', status: 'connected' }] }))
    const match = /^\/v1\/config\/integrations\/([^/]+)$/.exec(url)
    if (match && method === 'PUT') {
      const integrationType = match[1]
      const body = JSON.parse(String(init?.body))
      if (opts?.upsertHandler) return Promise.resolve(opts.upsertHandler(integrationType, body))
      return Promise.resolve(
        jsonResponse({ integration_type: integrationType, credentials_configured: true, settings: body.settings }),
      )
    }
    return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
  })
}

describe('AdminIntegrationsPage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('shows exactly 5 sidebar entries with no "Other…" anywhere', async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)

    for (const label of ['PagerDuty', 'GitHub', 'Slack', 'Jira', 'Monitoring']) {
      expect(await screen.findByRole('button', { name: new RegExp(label) })).toBeInTheDocument()
    }
    expect(screen.queryByText(/other/i)).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: /type/i })).not.toBeInTheDocument()
  })

  it('shows configured/not-set and health badges per sidebar row', async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)

    const pagerdutyRow = await screen.findByRole('button', { name: /PagerDuty/ })
    expect(within(pagerdutyRow).getByText('Configured')).toBeInTheDocument()
    expect(within(pagerdutyRow).getByText('Connected')).toBeInTheDocument()

    const githubRow = screen.getByRole('button', { name: /GitHub/ })
    expect(within(githubRow).getByText('Not set')).toBeInTheDocument()
    expect(within(githubRow).getByText('No health check')).toBeInTheDocument()
  })

  it('renders credential fields with real labels as password inputs, pre-selecting the first row', async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)

    // PagerDuty is the first catalog entry, so its form shows without a click.
    expect(await screen.findByText('PagerDuty', { selector: 'h2' })).toBeInTheDocument()
    const apiKeyInput = screen.getByLabelText('API Key')
    expect(apiKeyInput).toHaveAttribute('type', 'password')
    // The raw storage key must never leak into the UI as a label.
    expect(screen.queryByText('api_key')).not.toBeInTheDocument()
  })

  it("switches the detail form when a different sidebar row is clicked, showing Slack's two credential fields and settings", async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)
    await screen.findByText('PagerDuty', { selector: 'h2' })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /Slack/ }))

    expect(await screen.findByText('Slack', { selector: 'h2' })).toBeInTheDocument()
    expect(screen.getByLabelText('Bot Token')).toHaveAttribute('type', 'password')
    expect(screen.getByLabelText('App Token')).toHaveAttribute('type', 'password')
    expect(screen.getByLabelText(/Default Channel/)).toBeInTheDocument()
    expect(screen.getByLabelText(/Channel Naming Convention/)).toBeInTheDocument()
    expect(screen.getByText(/prefers this bot_token\/app_token pair/)).toBeInTheDocument()
  })

  it('renders Monitoring with no credential fields and a 3-option Tool select', async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)
    await screen.findByText('PagerDuty', { selector: 'h2' })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /Monitoring/ }))

    expect(await screen.findByText('Monitoring', { selector: 'h2' })).toBeInTheDocument()
    expect(screen.getByText('No credentials required.')).toBeInTheDocument()

    const toolSelect = screen.getByLabelText(/Tool/) as HTMLSelectElement
    const options = within(toolSelect).getAllByRole('option')
    expect(options).toHaveLength(3)
    expect(options.map((o) => o.textContent)).toEqual(['Datadog', 'Prometheus', 'Cloudwatch'])
    expect(screen.getByLabelText(/Base URL/)).toBeInTheDocument()
  })

  it('omits a blank credential from the save payload while still sending settings', async () => {
    mockFetch()
    renderWithProviders(<AdminIntegrationsPage />)
    await screen.findByText('PagerDuty', { selector: 'h2' })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /Slack/ }))
    await screen.findByText('Slack', { selector: 'h2' })

    // Only fill in the bot token; app_token and both settings are left blank.
    await user.type(screen.getByLabelText('Bot Token'), 'xoxb-new-secret')
    await user.click(screen.getByRole('button', { name: /save integration/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/integrations/slack' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
    const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/integrations/slack' && init?.method === 'PUT')!
    const body = JSON.parse(String(call[1]!.body))
    expect(body.credentials).toEqual({ bot_token: 'xoxb-new-secret' })
    expect(body.credentials.app_token).toBeUndefined()
    // Settings are always sent (schema-driven — every field is a visible,
    // editable row), even when left at their default blank value.
    expect(body.settings).toEqual({ default_channel: '', channel_naming_convention: '' })
  })

  it('pre-fills a settings field from the existing config and leaves credential fields blank', async () => {
    mockFetch({
      configs: [
        {
          integration_type: 'jira',
          credentials_configured: true,
          settings: { cloud_id: 'existing-cloud-id', site_url: 'https://acme.atlassian.net' },
          created_at: '2026-08-23T20:00:00Z',
          updated_at: '2026-08-23T20:00:00Z',
        },
      ],
    })
    renderWithProviders(<AdminIntegrationsPage />)
    await screen.findByText('PagerDuty', { selector: 'h2' })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /Jira/ }))
    await screen.findByText('Jira', { selector: 'h2' })

    expect(screen.getByLabelText('API Token')).toHaveValue('')
    expect(screen.getByLabelText(/Cloud ID/)).toHaveValue('existing-cloud-id')
    expect(screen.getByLabelText(/Site URL/)).toHaveValue('https://acme.atlassian.net')
    expect(screen.getByLabelText('API Token')).toHaveAttribute('placeholder', 'Leave blank to keep current value')
  })

  it('surfaces a server validation error through the existing error-alert path', async () => {
    mockFetch({
      upsertHandler: () => jsonResponse({ message: 'monitoring: tool must be one of [datadog prometheus cloudwatch]' }, 400),
    })
    renderWithProviders(<AdminIntegrationsPage />)
    await screen.findByText('PagerDuty', { selector: 'h2' })

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /Monitoring/ }))
    await screen.findByText('Monitoring', { selector: 'h2' })
    await user.click(screen.getByRole('button', { name: /save integration/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/tool must be one of/)
  })
})
