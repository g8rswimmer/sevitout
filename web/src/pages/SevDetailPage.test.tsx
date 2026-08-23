import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { SevDetailPage } from '@/pages/SevDetailPage'
import { tokenStorage } from '@/lib/api'
import { renderWithProviders } from '@/test/utils'
import { MockWebSocket } from '@/test/mockWebSocket'
import type { OrgRole, SEVResponse, WhoAmIResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const SEV_ID = 'SEV-2026-0001'

const SEV: SEVResponse = {
  id: SEV_ID,
  title: 'Database outage',
  description: 'primary db down',
  severity_level: 1,
  status: 'investigating',
  affected_services: ['checkout'],
  detection_method: 'monitoring-dashboard',
  monitoring_tool: 'datadog',
  alert_url: 'https://alerts.example.com/1',
  metric_link: 'https://metrics.example.com/q/1',
  snapshot_url: 'https://img.example.com/1.png',
  started_at: '2026-08-23T20:00:00Z',
  created_at: '2026-08-23T20:00:00Z',
  updated_at: '2026-08-23T20:00:00Z',
  created_by: 'u1',
}

function me(org_role: OrgRole): WhoAmIResponse {
  return { id: 'u1', email: 'a@b.com', name: 'Ada', org_role }
}

function mockFetchFor(sev: SEVResponse, whoAmI: WhoAmIResponse) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/auth/me') return Promise.resolve(jsonResponse(whoAmI))
      if (url === `/v1/sevs/${sev.id}`) return Promise.resolve(jsonResponse(sev))
      if (url === `/v1/sevs/${sev.id}/roles` && method === 'GET') return Promise.resolve(jsonResponse({ roles: [] }))
      if (url === `/v1/sevs/${sev.id}/announcements` && method === 'GET')
        return Promise.resolve(jsonResponse({ announcements: [] }))
      if (url === `/v1/sevs/${sev.id}/announcements` && method === 'POST') {
        const body = JSON.parse(String(init?.body))
        return Promise.resolve(
          jsonResponse({ id: '1', sev_id: sev.id, message: body.message, audience: body.audience, created_at: '2026-08-23T21:00:00Z' }),
        )
      }
      if (url === `/v1/sevs/${sev.id}/chat` && method === 'GET') return Promise.resolve(jsonResponse({ entries: [] }))
      if (url === `/v1/sevs/${sev.id}/tasks` && method === 'GET') return Promise.resolve(jsonResponse({ tasks: [] }))
      if (url === `/v1/sevs/${sev.id}/links` && method === 'GET') return Promise.resolve(jsonResponse({ links: [] }))
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    }),
  )
}

function renderDetailPage() {
  return renderWithProviders(
    <Routes>
      <Route path="/sevs/:id" element={<SevDetailPage />} />
    </Routes>,
    { route: `/sevs/${SEV_ID}` },
  )
}

describe('SevDetailPage', () => {
  beforeEach(() => {
    localStorage.clear()
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders the SEV header and sections read-only for a Viewer', async () => {
    tokenStorage.set('tok')
    mockFetchFor(SEV, me('viewer'))
    renderDetailPage()

    expect(await screen.findByRole('heading', { name: 'Database outage' })).toBeInTheDocument()
    expect(screen.getByText('SEV-1')).toBeInTheDocument()
    expect(screen.getByText('Investigating')).toBeInTheDocument()
    expect(screen.getByText('primary db down')).toBeInTheDocument()
    expect(screen.getByText('checkout')).toBeInTheDocument()

    // No edit/transition affordances for a Viewer.
    expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/transition to:/i)).not.toBeInTheDocument()

    // Detection metadata renders as human labels, not raw wire slugs, and
    // the three supporting links render as clickable external links plus a
    // snapshot preview image.
    expect(screen.getByText('Monitoring Dashboard')).toBeInTheDocument()
    expect(screen.getByText('datadog')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /alerts\.example\.com/ })).toHaveAttribute(
      'href',
      'https://alerts.example.com/1',
    )
    expect(screen.getByRole('link', { name: /metrics\.example\.com/ })).toHaveAttribute(
      'href',
      'https://metrics.example.com/q/1',
    )
    expect(screen.getByRole('img', { name: /snapshot preview/i })).toHaveAttribute(
      'src',
      'https://img.example.com/1.png',
    )
  })

  it('lets a Responder post an announcement', async () => {
    tokenStorage.set('tok')
    mockFetchFor(SEV, me('responder'))
    renderDetailPage()

    await screen.findByRole('heading', { name: 'Database outage' })
    const announcementsSection = screen.getByText('Announcements').closest('div')!.parentElement!
    const textarea = within(announcementsSection).getByLabelText(/announcement message/i)

    const user = userEvent.setup()
    await user.type(textarea, 'Investigating the root cause')
    await user.click(within(announcementsSection).getByRole('button', { name: /post/i }))

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith(
        `/v1/sevs/${SEV_ID}/announcements`,
        expect.objectContaining({ method: 'POST' }),
      ),
    )
  })

  it('shows status transition options for an Incident Commander', async () => {
    tokenStorage.set('tok')
    mockFetchFor(SEV, me('incident-commander'))
    renderDetailPage()

    await screen.findByRole('heading', { name: 'Database outage' })
    expect(screen.getByText(/transition to:/i)).toBeInTheDocument()
    // From "investigating", valid next statuses are mitigated and open.
    expect(screen.getByRole('button', { name: 'Mitigated' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open' })).toBeInTheDocument()
  })
})
