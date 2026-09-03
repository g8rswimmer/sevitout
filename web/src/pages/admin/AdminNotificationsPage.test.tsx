import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminNotificationsPage } from '@/pages/admin/AdminNotificationsPage'
import { renderWithProviders } from '@/test/utils'
import type { EscalationConfigResponse, NotificationConfigResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const IC_RULE: NotificationConfigResponse = {
  role: 'incident-commander',
  event: 'sev.created',
  channel_type: 'slack',
  channel_target: '#incidents',
  created_at: '2026-08-23T20:00:00Z',
  updated_at: '2026-08-23T20:00:00Z',
}

const SEV1_ESCALATION: EscalationConfigResponse = {
  severity_level: 1,
  threshold_minutes: 30,
  enabled: true,
  updated_at: '2026-08-23T20:00:00Z',
}

function mockFetch(rules: NotificationConfigResponse[], escalations: EscalationConfigResponse[]) {
  let ruleList = rules
  vi.mocked(fetch).mockImplementation((input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/v1/config/notifications' && method === 'GET') {
      return Promise.resolve(jsonResponse({ configs: ruleList }))
    }
    if (url === '/v1/config/notifications' && method === 'PUT') {
      const body = JSON.parse(String(init?.body))
      const created = { ...body, created_at: 'now', updated_at: 'now' }
      ruleList = [...ruleList, created]
      return Promise.resolve(jsonResponse(created))
    }
    if (url.startsWith('/v1/config/notifications?') && method === 'DELETE') {
      const params = new URLSearchParams(url.split('?')[1])
      ruleList = ruleList.filter(
        (r) => !(r.role === params.get('role') && r.event === params.get('event') && r.channel_type === params.get('channel_type')),
      )
      return Promise.resolve(new Response(null, { status: 204 }))
    }
    if (url === '/v1/config/escalation' && method === 'GET') {
      return Promise.resolve(jsonResponse({ configs: escalations }))
    }
    if (url === '/v1/config/escalation/2' && method === 'PUT') {
      const body = JSON.parse(String(init?.body))
      return Promise.resolve(jsonResponse({ ...body, updated_at: 'now' }))
    }
    return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
  })
}

describe('AdminNotificationsPage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders existing routing rules', async () => {
    mockFetch([IC_RULE], [SEV1_ESCALATION])
    renderWithProviders(<AdminNotificationsPage />)

    expect(await screen.findByText('Incident Commander')).toBeInTheDocument()
    expect(screen.getByText('SEV opened')).toBeInTheDocument()
    expect(screen.getByText('#incidents')).toBeInTheDocument()
    expect(screen.getByText('Every severity')).toBeInTheDocument()
  })

  it('adds a new routing rule', async () => {
    mockFetch([], [])
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('No notification rules configured yet.')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /add rule/i }))
    await user.type(screen.getByLabelText('Channel target'), 'mgmt@example.com')
    await user.selectOptions(screen.getByLabelText('Channel type'), 'email')
    await user.click(screen.getByRole('button', { name: /^add rule$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/notifications' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
    const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/notifications' && init?.method === 'PUT')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({
      channel_target: 'mgmt@example.com',
      channel_type: 'email',
    })
  })

  it('deletes a routing rule', async () => {
    mockFetch([IC_RULE], [])
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('#incidents')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /delete rule for incident commander/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url).startsWith('/v1/config/notifications?') && init?.method === 'DELETE')
      expect(call).toBeDefined()
    })
  })

  it('saves an escalation threshold', async () => {
    mockFetch([], [SEV1_ESCALATION])
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('SEV-1')

    const user = userEvent.setup()
    const sev2Input = screen.getByLabelText('Escalation threshold minutes for SEV-2')
    await user.clear(sev2Input)
    await user.type(sev2Input, '45')
    await user.click(screen.getByLabelText('Enable escalation for SEV-2'))
    const saveButtons = screen.getAllByRole('button', { name: /^save$/i })
    await user.click(saveButtons[1])

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/escalation/2' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
    const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/escalation/2' && init?.method === 'PUT')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({ severity_level: 2, threshold_minutes: 45, enabled: true })
  })
})
