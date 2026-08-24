import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { PublicSharePage } from '@/pages/PublicSharePage'
import { renderWithProviders } from '@/test/utils'
import type { SharedSEVResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function textResponse(body: string, status: number) {
  return new Response(body, { status, headers: { 'Content-Type': 'text/plain' } })
}

const TOKEN = 'abc123'

const SHARED: SharedSEVResponse = {
  id: 'SEV-2026-0001',
  title: 'Checkout outage',
  severity_level: 1,
  status: 'resolved',
  started_at: '2026-08-23T20:00:00Z',
  resolved_at: '2026-08-23T21:00:00Z',
  business_impact: 'Checkout unavailable for all customers.',
  announcements: [{ message: 'We have identified the issue.', created_at: '2026-08-23T20:30:00Z' }],
}

function renderPage() {
  return renderWithProviders(
    <Routes>
      <Route path="/s/:token" element={<PublicSharePage />} />
    </Routes>,
    { route: `/s/${TOKEN}` },
  )
}

describe('PublicSharePage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders the curated public summary — title, severity, status, timestamps, business impact, and external announcements', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse(SHARED))
    renderPage()

    expect(await screen.findByRole('heading', { name: 'Checkout outage' })).toBeInTheDocument()
    expect(screen.getByText('SEV-1')).toBeInTheDocument()
    // "Resolved" appears twice on purpose: the status badge, and the
    // Resolved-timestamp field's own <dt> label.
    expect(screen.getAllByText('Resolved').length).toBe(2)
    expect(screen.getByText('Checkout unavailable for all customers.')).toBeInTheDocument()
    expect(screen.getByText('We have identified the issue.')).toBeInTheDocument()
  })

  it('shows the server\'s exact plain-text error message for a revoked/expired/unknown link', async () => {
    vi.mocked(fetch).mockResolvedValue(textResponse('link has been revoked', 410))
    renderPage()

    expect(await screen.findByText('link has been revoked')).toBeInTheDocument()
  })

  it('does not render a section for fields the public view never includes', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ ...SHARED, business_impact: undefined, announcements: [] }))
    renderPage()

    await screen.findByRole('heading', { name: 'Checkout outage' })
    expect(screen.queryByText('Business impact')).not.toBeInTheDocument()
    expect(screen.queryByText('Updates')).not.toBeInTheDocument()
  })
})
