import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminServicesPage } from '@/pages/admin/AdminServicesPage'
import { renderWithProviders } from '@/test/utils'
import type { ServiceResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const CHECKOUT: ServiceResponse = {
  id: 'checkout',
  name: 'Checkout',
  owning_team: 'Payments',
  active: true,
  tags: { tier: '1' },
  created_at: '2026-08-23T20:00:00Z',
  updated_at: '2026-08-23T20:00:00Z',
}

describe('AdminServicesPage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders the service registry', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ services: [CHECKOUT] }))
    renderWithProviders(<AdminServicesPage />)

    expect(await screen.findByText('Checkout')).toBeInTheDocument()
    expect(screen.getByText('Payments')).toBeInTheDocument()
    expect(screen.getByText('tier=1')).toBeInTheDocument()
  })

  it('creates a new service with the expected request body', async () => {
    let services = [CHECKOUT]
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/config/services' && method === 'GET') return Promise.resolve(jsonResponse({ services }))
      if (url === '/v1/config/services' && method === 'POST') {
        const body = JSON.parse(String(init?.body))
        const created = { ...body, active: true, created_at: 'now', updated_at: 'now' }
        services = [...services, created]
        return Promise.resolve(jsonResponse(created))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })

    renderWithProviders(<AdminServicesPage />)
    await screen.findByText('Checkout')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /new service/i }))
    await user.type(screen.getByLabelText('ID (slug)'), 'payments')
    await user.type(screen.getByLabelText('Name'), 'Payments Service')
    await user.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/services' && init?.method === 'POST')
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/services' && init?.method === 'POST')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({ id: 'payments', name: 'Payments Service' })
  })

  it('edits a service and saves the updated fields', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/config/services' && method === 'GET') return Promise.resolve(jsonResponse({ services: [CHECKOUT] }))
      if (url === '/v1/config/services/checkout' && method === 'PATCH') {
        return Promise.resolve(jsonResponse({ ...CHECKOUT, owning_team: 'Core' }))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })

    renderWithProviders(<AdminServicesPage />)
    await screen.findByText('Checkout')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /edit checkout/i }))
    const teamInput = screen.getByLabelText('Owning team')
    await user.clear(teamInput)
    await user.type(teamInput, 'Core')
    await user.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/services/checkout' && init?.method === 'PATCH')
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/services/checkout' && init?.method === 'PATCH')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({ owning_team: 'Core' })
  })

  it('opens the SLA editor and saves a target in minutes as seconds', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/config/services' && method === 'GET') return Promise.resolve(jsonResponse({ services: [CHECKOUT] }))
      if (url === '/v1/config/services/checkout/sla' && method === 'GET') return Promise.resolve(jsonResponse({ slas: [] }))
      if (url === '/v1/config/services/checkout/sla/1' && method === 'PUT') {
        return Promise.resolve(
          jsonResponse({
            service_id: 'checkout',
            severity_level: 1,
            mttd_target_seconds: '300',
            created_at: 'now',
            updated_at: 'now',
          }),
        )
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })

    renderWithProviders(<AdminServicesPage />)
    await screen.findByText('Checkout')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /manage slas for checkout/i }))
    const mttdInput = await screen.findByLabelText('MTTD target minutes for SEV-1')
    await user.type(mttdInput, '5')
    const saveButtons = screen.getAllByRole('button', { name: /^save$/i })
    await user.click(saveButtons[0])

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/services/checkout/sla/1' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/services/checkout/sla/1' && init?.method === 'PUT')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({ severity_level: 1, mttd_target_seconds: 300 })
  })

  it('deletes a service directly (no confirmation step)', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/config/services' && method === 'GET') return Promise.resolve(jsonResponse({ services: [CHECKOUT] }))
      if (url === '/v1/config/services/checkout' && method === 'DELETE') return Promise.resolve(new Response(null, { status: 204 }))
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })

    renderWithProviders(<AdminServicesPage />)
    await screen.findByText('Checkout')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /delete checkout/i }))

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith('/v1/config/services/checkout', expect.objectContaining({ method: 'DELETE' })),
    )
  })
})
