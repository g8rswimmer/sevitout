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
