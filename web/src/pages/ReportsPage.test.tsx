import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { ReportsPage } from '@/pages/ReportsPage'
import { renderWithProviders } from '@/test/utils'
import type { DashboardMetricsResponse, SEVTrendsResponse, ServiceResponse } from '@/types/api'

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

function mockFetch() {
  vi.mocked(fetch).mockImplementation((input) => {
    const url = String(input)
    if (url === '/v1/reports/dashboard') return Promise.resolve(jsonResponse(METRICS))
    if (url === '/v1/reports/trends') return Promise.resolve(jsonResponse(TRENDS))
    if (url === '/v1/config/services') return Promise.resolve(jsonResponse(SERVICES))
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
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    renderPage()

    expect(await screen.findByText(/no recurring incident patterns yet/i)).toBeInTheDocument()
  })
})
