import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { ReportsPage } from '@/pages/ReportsPage'
import { renderWithProviders } from '@/test/utils'
import type { DashboardMetricsResponse, SEVTrendsResponse, ServiceMetricsResponse, ServiceResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const METRICS: DashboardMetricsResponse = {
  mttr_trends: [
    { window_days: 7, average_mttr_seconds: '3600', sample_size: 2 },
    { window_days: 30, average_mttr_seconds: '7200', sample_size: 5 },
    { window_days: 90, average_mttr_seconds: '5400', sample_size: 10 },
  ],
  frequency_by_service_and_level: [
    { service_id: 'checkout', severity_level: 1, count: 3 },
    { service_id: 'checkout', severity_level: 2, count: 1 },
    { service_id: 'payments', severity_level: 1, count: 1 },
  ],
  postmortem_completion_rate: 0.75,
}

const TRENDS: SEVTrendsResponse = {
  recurring_patterns: [
    { service_id: 'checkout', root_cause_category: 'deployment', count: 2, sev_ids: ['SEV-2026-0002', 'SEV-2026-0001'] },
  ],
}

const SERVICES: { services: ServiceResponse[] } = {
  services: [
    { id: 'checkout', name: 'Checkout', created_at: 'now', updated_at: 'now' },
    { id: 'payments', name: 'Payments', created_at: 'now', updated_at: 'now' },
  ],
}

// service_id 'auth' deliberately doesn't match anything in SERVICES above
// (falls back to the raw id, per ReportsPage's serviceName()) — keeps this
// fixture's text out of the "Checkout"/"Payments" occurrence counts the
// first test below asserts exactly, since those are unrelated to this
// section's own tests further down.
const SERVICE_METRICS: ServiceMetricsResponse = {
  service_level_metrics: [
    {
      service_id: 'auth',
      severity_level: 2,
      sev_count: 3,
      avg_mttd_seconds: '240',
      avg_mttm_seconds: '900',
      avg_mttr_seconds: '2700',
      sla_ok_count: 3,
      sla_at_risk_count: 0,
      sla_breached_count: 0,
      sla_not_applicable_count: 0,
      compliance_pct: 1,
    },
  ],
  window_days: 30,
}

function mockFetch() {
  vi.mocked(fetch).mockImplementation((input) => {
    const url = String(input)
    if (url === '/v1/reports/dashboard') return Promise.resolve(jsonResponse(METRICS))
    if (url === '/v1/reports/trends') return Promise.resolve(jsonResponse(TRENDS))
    if (url === '/v1/config/services') return Promise.resolve(jsonResponse(SERVICES))
    if (url.startsWith('/v1/reports/service-metrics')) return Promise.resolve(jsonResponse(SERVICE_METRICS))
    return Promise.reject(new Error(`unexpected fetch: ${url}`))
  })
}

function renderPage() {
  return renderWithProviders(
    <Routes>
      <Route path="/reports" element={<ReportsPage />} />
      <Route path="/sevs/:id" element={<div>sev detail</div>} />
    </Routes>,
    { route: '/reports' },
  )
}

describe('ReportsPage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders the MTTR trend, postmortem completion rate, service heatmap (resolved to names), and recurring patterns', async () => {
    mockFetch()
    renderPage()

    expect(await screen.findByText('75%')).toBeInTheDocument()
    // "Checkout"/"Payments" each appear twice — once resolved in the
    // heatmap, once resolved in the recurring-patterns table. The SLA
    // compliance table's service filter (Phase 13a) also resolves these
    // names, but only inside its closed-by-default dropdown popover.
    expect((await screen.findAllByText('Checkout')).length).toBe(2)
    expect(screen.getAllByText('Payments').length).toBe(1)
    // The heatmap cell for checkout/SEV-1 shows the count.
    const heatmap = screen.getByText('Service × Severity Heatmap').closest('div')!.parentElement!
    expect(heatmap.textContent).toContain('3')

    expect(await screen.findByText('Deployment')).toBeInTheDocument()
    const link = screen.getByRole('link', { name: 'SEV-2026-0002' })
    expect(link).toHaveAttribute('href', '/sevs/SEV-2026-0002')
  })

  it('shows an explanatory message when there are no recurring patterns', async () => {
    mockFetch()
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url === '/v1/reports/dashboard') return Promise.resolve(jsonResponse(METRICS))
      if (url === '/v1/reports/trends') return Promise.resolve(jsonResponse({ recurring_patterns: [] }))
      if (url === '/v1/config/services') return Promise.resolve(jsonResponse(SERVICES))
      if (url.startsWith('/v1/reports/service-metrics')) return Promise.resolve(jsonResponse(SERVICE_METRICS))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    renderPage()

    expect(await screen.findByText(/no recurring incident patterns yet/i)).toBeInTheDocument()
  })

  describe('SLA Compliance by Service', () => {
    it('renders the fetched rows, resolving the service name and formatting compliance', async () => {
      mockFetch()
      renderPage()

      expect(await screen.findByText('auth')).toBeInTheDocument()
      // Scoped to this section — "SEV-2" also appears as a heatmap column
      // header elsewhere on the page.
      const section = screen.getByText('SLA Compliance by Service').closest('div')!.parentElement!
      expect(within(section).getByText('SEV-2')).toBeInTheDocument()
      expect(within(section).getByText('100%')).toBeInTheDocument()
    })

    it('treats an omitted (zero-valued) compliance breakdown as "no SLA configured", not NaN%', async () => {
      vi.mocked(fetch).mockImplementation((input) => {
        const url = String(input)
        if (url === '/v1/reports/dashboard') return Promise.resolve(jsonResponse(METRICS))
        if (url === '/v1/reports/trends') return Promise.resolve(jsonResponse(TRENDS))
        if (url === '/v1/config/services') return Promise.resolve(jsonResponse(SERVICES))
        // Mirrors real protojson output (main.go's marshaler has no
        // EmitUnpopulated, live-verified against a running server): a group
        // whose compliance counts are all zero has no sla_*_count/
        // compliance_pct keys in the JSON body at all, not zeros.
        if (url.startsWith('/v1/reports/service-metrics')) {
          return Promise.resolve(
            jsonResponse({
              service_level_metrics: [{ service_id: 'auth', severity_level: 2, sev_count: 1 }],
              window_days: 30,
            }),
          )
        }
        return Promise.reject(new Error(`unexpected fetch: ${url}`))
      })
      renderPage()

      expect(await screen.findByText('auth')).toBeInTheDocument()
      const section = screen.getByText('SLA Compliance by Service').closest('div')!.parentElement!
      expect(within(section).getByText('No SLA configured')).toBeInTheDocument()
      expect(within(section).queryByText(/NaN/)).not.toBeInTheDocument()
    })

    it('switching the reporting window refetches with the new window_days', async () => {
      mockFetch()
      renderPage()
      await screen.findByText('auth')

      const user = userEvent.setup()
      await user.click(screen.getByRole('button', { name: '60d' }))

      await waitFor(() => {
        const called = vi
          .mocked(fetch)
          .mock.calls.some(([url]) => String(url).startsWith('/v1/reports/service-metrics') && String(url).includes('window_days=60'))
        expect(called).toBe(true)
      })
    })

    it('selecting a service in the filter refetches with service_ids set', async () => {
      mockFetch()
      renderPage()
      await screen.findByText('auth')

      const user = userEvent.setup()
      await user.click(screen.getByRole('button', { name: 'All service' }))
      await user.click(screen.getByRole('checkbox', { name: 'Checkout' }))

      await waitFor(() => {
        const called = vi
          .mocked(fetch)
          .mock.calls.some(([url]) => String(url).startsWith('/v1/reports/service-metrics') && String(url).includes('service_ids=checkout'))
        expect(called).toBe(true)
      })
    })

    it('selecting a severity narrows the rendered rows without refetching', async () => {
      mockFetch()
      renderPage()
      await screen.findByText('auth')
      const fetchCallsBefore = vi.mocked(fetch).mock.calls.length

      const user = userEvent.setup()
      await user.click(screen.getByRole('button', { name: 'All severity' }))
      await user.click(screen.getByRole('checkbox', { name: 'SEV-3' }))

      // The one fixture row is SEV-2, so narrowing to SEV-3 empties the table.
      expect(await screen.findByText('No SEVs match the selected filters.')).toBeInTheDocument()
      expect(vi.mocked(fetch).mock.calls.length).toBe(fetchCallsBefore)
    })
  })
})
