import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import { LevelingCriteriaPanel } from '@/components/sev/LevelingCriteriaPanel'
import { renderWithProviders } from '@/test/utils'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('LevelingCriteriaPanel', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders nothing when no services are selected', () => {
    const { container } = renderWithProviders(<LevelingCriteriaPanel severityLevel={1} serviceIds={[]} />)
    expect(container).toBeEmptyDOMElement()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('shows guidance text per service when populated', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url === '/v1/config/services') {
        return Promise.resolve(
          jsonResponse({ services: [{ id: 'checkout', name: 'Checkout', active: true, created_at: 'now', updated_at: 'now' }] }),
        )
      }
      if (url.startsWith('/v1/config/leveling-criteria')) {
        return Promise.resolve(
          jsonResponse({
            criteria: [
              {
                service_id: 'checkout',
                severity_level: 1,
                criteria: '>50% of checkout traffic failing',
                created_at: 'now',
                updated_at: 'now',
              },
            ],
          }),
        )
      }
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })

    renderWithProviders(<LevelingCriteriaPanel severityLevel={1} serviceIds={['checkout']} />)

    expect(await screen.findByText(/>50% of checkout traffic failing/)).toBeInTheDocument()
    expect(screen.getByText(/Checkout:/)).toBeInTheDocument()

    const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url).startsWith('/v1/config/leveling-criteria'))!
    const requestedUrl = new URL(String(call[0]), 'http://localhost')
    expect(requestedUrl.searchParams.getAll('service_ids')).toEqual(['checkout'])
    expect(requestedUrl.searchParams.get('severity_level')).toBe('1')
  })

  it('shows a quiet empty-state note when nothing is configured', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url === '/v1/config/services') return Promise.resolve(jsonResponse({ services: [] }))
      if (url.startsWith('/v1/config/leveling-criteria')) return Promise.resolve(jsonResponse({ criteria: [] }))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })

    renderWithProviders(<LevelingCriteriaPanel severityLevel={2} serviceIds={['checkout']} />)

    expect(await screen.findByText(/No leveling criteria configured/)).toBeInTheDocument()
  })
})
