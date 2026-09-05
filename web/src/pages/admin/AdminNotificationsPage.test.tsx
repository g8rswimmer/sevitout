import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminNotificationsPage } from '@/pages/admin/AdminNotificationsPage'
import { renderWithProviders } from '@/test/utils'
import type { EscalationConfigResponse, NotificationConfigResponse, TestNotificationConfigResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const IC_RULE: NotificationConfigResponse = {
  id: '1',
  role: 'incident-commander',
  events: ['sev.created'],
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

/** testResponder builds the TestNotificationConfig response for whatever
 * request body the "Send test" button submits; defaults to a success result
 * for every event requested. Override it to simulate a delivery failure. */
function mockFetch(
  rules: NotificationConfigResponse[],
  escalations: EscalationConfigResponse[],
  testResponder: (body: { events: string[] }) => TestNotificationConfigResponse = (body) => ({
    results: body.events.map((event) => ({ event, success: true })),
  }),
) {
  let ruleList = rules
  let nextID = rules.length + 1
  vi.mocked(fetch).mockImplementation((input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/v1/config/notifications' && method === 'GET') {
      return Promise.resolve(jsonResponse({ configs: ruleList }))
    }
    if (url === '/v1/config/notifications' && method === 'POST') {
      const body = JSON.parse(String(init?.body))
      const created = { ...body, id: String(nextID++), created_at: 'now', updated_at: 'now' }
      ruleList = [...ruleList, created]
      return Promise.resolve(jsonResponse(created))
    }
    if (url === '/v1/config/notifications/test' && method === 'POST') {
      const body = JSON.parse(String(init?.body))
      return Promise.resolve(jsonResponse(testResponder(body)))
    }
    const deleteMatch = url.match(/^\/v1\/config\/notifications\/(\d+)$/)
    if (deleteMatch && method === 'DELETE') {
      ruleList = ruleList.filter((r) => r.id !== deleteMatch[1])
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

  it('renders every event covered by a multi-event rule', async () => {
    const slaRule: NotificationConfigResponse = {
      id: '2',
      role: 'admin',
      events: ['sev.sla_at_risk', 'sev.sla_breached'],
      channel_type: 'slack',
      channel_target: '#sla-alerts',
      created_at: '2026-08-23T20:00:00Z',
      updated_at: '2026-08-23T20:00:00Z',
    }
    mockFetch([slaRule], [])
    renderWithProviders(<AdminNotificationsPage />)

    expect(await screen.findByText('SLA at risk')).toBeInTheDocument()
    expect(screen.getByText('SLA breached')).toBeInTheDocument()
  })

  it('adds a new routing rule covering multiple events', async () => {
    mockFetch([], [])
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('No notification rules configured yet.')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /add rule/i }))
    // "SEV opened" is checked by default; also cover SLA breached.
    await user.click(screen.getByLabelText('SLA breached'))
    await user.type(screen.getByLabelText('Channel target'), 'mgmt@example.com')
    await user.selectOptions(screen.getByLabelText('Channel type'), 'email')
    await user.click(screen.getByRole('button', { name: /^add rule$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/notifications' && init?.method === 'POST')
      expect(call).toBeDefined()
    })
    const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/notifications' && init?.method === 'POST')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({
      channel_target: 'mgmt@example.com',
      channel_type: 'email',
      events: ['sev.created', 'sev.sla_breached'],
    })
  })

  it('disables Add rule when every event is deselected', async () => {
    mockFetch([], [])
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('No notification rules configured yet.')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /add rule/i }))
    await user.type(screen.getByLabelText('Channel target'), '#incidents')
    // Deselect the only checked event ("SEV opened", checked by default).
    await user.click(screen.getByLabelText('SEV opened'))

    expect(screen.getByRole('button', { name: /^add rule$/i })).toBeDisabled()
  })

  it('sends a test for an existing rule and shows per-event results', async () => {
    const slaRule: NotificationConfigResponse = {
      id: '2',
      role: 'admin',
      events: ['sev.sla_at_risk', 'sev.sla_breached'],
      channel_type: 'slack',
      channel_target: '#sla-alerts',
      created_at: '2026-08-23T20:00:00Z',
      updated_at: '2026-08-23T20:00:00Z',
    }
    mockFetch([slaRule], [])
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('#sla-alerts')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /send test notifications for admin/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/notifications/test' && init?.method === 'POST')
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/notifications/test' && init?.method === 'POST')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({
      role: 'admin',
      events: ['sev.sla_at_risk', 'sev.sla_breached'],
      channel_type: 'slack',
      channel_target: '#sla-alerts',
    })
    expect(await screen.findByText(/SLA at risk: sent/)).toBeInTheDocument()
    expect(screen.getByText(/SLA breached: sent/)).toBeInTheDocument()
  })

  it('shows the delivery error when a test fails', async () => {
    mockFetch([IC_RULE], [], () => ({
      results: [{ event: 'sev.created', success: false, error: 'slack integration unavailable' }],
    }))
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('#incidents')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /send test notifications for incident commander/i }))

    expect(await screen.findByText(/SEV opened: slack integration unavailable/)).toBeInTheDocument()
  })

  it('sends a test from the draft form before saving', async () => {
    mockFetch([], [])
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('No notification rules configured yet.')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /add rule/i }))
    await user.type(screen.getByLabelText('Channel target'), '#incidents')
    await user.click(screen.getByRole('button', { name: /^send test$/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/notifications/test' && init?.method === 'POST')
      expect(call).toBeDefined()
    })
    expect(await screen.findByText(/SEV opened: sent/)).toBeInTheDocument()
    // The draft was only tested, never saved.
    const createCall = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/notifications' && init?.method === 'POST')
    expect(createCall).toBeUndefined()
  })

  it('disables Send test in the draft form when no events are selected', async () => {
    mockFetch([], [])
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('No notification rules configured yet.')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /add rule/i }))
    await user.type(screen.getByLabelText('Channel target'), '#incidents')
    await user.click(screen.getByLabelText('SEV opened'))

    expect(screen.getByRole('button', { name: /^send test$/i })).toBeDisabled()
  })

  it('deletes a routing rule', async () => {
    mockFetch([IC_RULE], [])
    renderWithProviders(<AdminNotificationsPage />)
    await screen.findByText('#incidents')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /delete rule for incident commander/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/notifications/1' && init?.method === 'DELETE')
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
