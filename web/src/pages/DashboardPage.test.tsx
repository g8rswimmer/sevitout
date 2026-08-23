import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import { DashboardPage } from '@/pages/DashboardPage'
import { renderWithProviders } from '@/test/utils'
import type { DashboardMetricsResponse, ListSEVsResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const METRICS: DashboardMetricsResponse = {
  active_by_level: [
    { severity_level: 1, count: 2 },
    { severity_level: 2, count: 1 },
  ],
  mttr_trends: [
    { window_days: 7, average_mttr_seconds: '3600', sample_size: 4 },
    { window_days: 30, average_mttr_seconds: '7200', sample_size: 10 },
    { window_days: 90, average_mttr_seconds: '10800', sample_size: 25 },
  ],
  frequency_by_service_and_level: [],
  postmortem_completion_rate: 0.75,
  overdue_task_count: 5,
}

const SEVS: ListSEVsResponse = {
  sevs: [
    {
      id: 'sev-1',
      title: 'Database outage',
      description: '',
      severity_level: 1,
      status: 'investigating',
      root_cause_category: '',
      root_cause_description: '',
      mitigation: '',
      prevention: '',
      business_impact: '',
      affected_services: [],
      alert_name: '',
      monitoring_tool: '',
      right_people_notes: '',
      tags: {},
      mttd_seconds: '0',
      mttm_seconds: '0',
      mttr_seconds: '0',
      dttm_seconds: '0',
      locked: false,
      sensitive: false,
      created_at: '2026-08-20T00:00:00Z',
      updated_at: '2026-08-20T00:00:00Z',
      created_by: 'u1',
      ai_disabled: false,
    },
  ],
  total: 1,
}

describe('DashboardPage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders metrics and the active SEV list', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/reports/dashboard')) return Promise.resolve(jsonResponse(METRICS))
      if (url.startsWith('/v1/sevs')) return Promise.resolve(jsonResponse(SEVS))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })

    renderWithProviders(<DashboardPage />)

    await waitFor(() => expect(screen.getByText('5')).toBeInTheDocument()) // overdue task count
    expect(screen.getByText('75%')).toBeInTheDocument()
    expect(screen.getByText('SEV-1: 2')).toBeInTheDocument()
    expect(screen.getByText('SEV-2: 1')).toBeInTheDocument()
    expect(await screen.findByText('Database outage')).toBeInTheDocument()
  })

  it('shows an empty state when there are no active SEVs', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/reports/dashboard')) return Promise.resolve(jsonResponse(METRICS))
      if (url.startsWith('/v1/sevs')) return Promise.resolve(jsonResponse({ sevs: [], total: 0 }))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })

    renderWithProviders(<DashboardPage />)

    expect(await screen.findByText(/no active sevs/i)).toBeInTheDocument()
  })

  it('surfaces a load error for the metrics card', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/reports/dashboard')) return Promise.resolve(new Response('boom', { status: 500 }))
      if (url.startsWith('/v1/sevs')) return Promise.resolve(jsonResponse({ sevs: [], total: 0 }))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })

    renderWithProviders(<DashboardPage />)

    expect(await screen.findByText(/failed to load dashboard metrics/i)).toBeInTheDocument()
  })
})
