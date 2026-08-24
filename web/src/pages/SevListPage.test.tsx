import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SevListPage } from '@/pages/SevListPage'
import { renderWithProviders } from '@/test/utils'
import type { SearchSEVsResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const RESULTS: SearchSEVsResponse = {
  sevs: [
    {
      id: 'SEV-2026-0001',
      title: 'Database outage',
      severity_level: 1,
      status: 'open',
      affected_services: ['checkout'],
      started_at: '2026-08-23T20:00:00Z',
      created_at: '2026-08-23T20:00:00Z',
      updated_at: '2026-08-23T20:00:00Z',
    },
  ],
  total: 1,
}

describe('SevListPage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders search results', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      return Promise.resolve(jsonResponse(RESULTS))
    })

    renderWithProviders(<SevListPage />)

    expect(await screen.findByText('Database outage')).toBeInTheDocument()
    expect(screen.getByText('1–1 of 1')).toBeInTheDocument()
  })

  it('requests the matching quick_view when a tab is clicked', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      return Promise.resolve(jsonResponse({ sevs: [], total: 0 }))
    })

    renderWithProviders(<SevListPage />)
    await waitFor(() => expect(fetch).toHaveBeenCalled())

    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'My SEVs' }))

    await waitFor(() => {
      const called = vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes('quick_view=my_sevs'))
      expect(called).toBe(true)
    })
  })

  it('requests the typed query on search submit', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      return Promise.resolve(jsonResponse({ sevs: [], total: 0 }))
    })

    renderWithProviders(<SevListPage />)
    await waitFor(() => expect(fetch).toHaveBeenCalled())

    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/search sevs/i), 'database')
    await user.click(screen.getByRole('button', { name: 'Search' }))

    await waitFor(() => {
      const called = vi.mocked(fetch).mock.calls.some(([url]) => String(url).includes('query=database'))
      expect(called).toBe(true)
    })
  })

  it('exports the current severity/status filters as a downloaded CSV', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      if (url.startsWith('/v1/sevs/export.csv')) {
        expect(url).toContain('severity_levels=1')
        return Promise.resolve(new Response('id,title\nSEV-2026-0001,Database outage', { status: 200 }))
      }
      return Promise.resolve(jsonResponse(RESULTS))
    })

    renderWithProviders(<SevListPage />)
    await waitFor(() => expect(fetch).toHaveBeenCalled())

    let anchor: HTMLAnchorElement | undefined
    const createElement = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      const el = createElement(tag)
      if (tag === 'a') anchor = el as HTMLAnchorElement
      return el
    })
    vi.stubGlobal('URL', { ...URL, createObjectURL: vi.fn(() => 'blob:mock-url'), revokeObjectURL: vi.fn() })
    const originalClick = HTMLAnchorElement.prototype.click
    HTMLAnchorElement.prototype.click = vi.fn<() => void>()

    const user = userEvent.setup()
    await user.click(screen.getByRole('checkbox', { name: 'SEV-1' }))
    await user.click(screen.getByRole('button', { name: /export csv/i }))

    await waitFor(() => expect(anchor?.download).toBe('sevs-export.csv'))

    HTMLAnchorElement.prototype.click = originalClick
  })
})
