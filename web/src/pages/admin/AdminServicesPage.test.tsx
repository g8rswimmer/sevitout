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

  it('creates a new service with an SLA target set at creation time', async () => {
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
      if (url === '/v1/config/services/payments/sla/1' && method === 'PUT') {
        return Promise.resolve(
          jsonResponse({ service_id: 'payments', severity_level: 1, mttd_target_seconds: '18000', created_at: 'now', updated_at: 'now' }),
        )
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })

    renderWithProviders(<AdminServicesPage />)
    await screen.findByText('Checkout')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /new service/i }))
    await user.type(screen.getByLabelText('ID (slug)'), 'payments')
    await user.type(screen.getByLabelText('Name'), 'Payments Service')
    await user.type(screen.getByLabelText('New service MTTD target hours for SEV-1'), '5')
    // A blank row (SEV-2) must not trigger an UpsertServiceSLA call at all.
    await user.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/services/payments/sla/1' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
    const slaCall = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/services/payments/sla/1' && init?.method === 'PUT')!
    expect(JSON.parse(String(slaCall[1]!.body))).toMatchObject({ severity_level: 1, mttd_target_seconds: 18000 })

    const sla2Call = vi
      .mocked(fetch)
      .mock.calls.find(([url]) => String(url) === '/v1/config/services/payments/sla/2')
    expect(sla2Call).toBeUndefined()
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

  it('opens the SLA editor and saves a target in hours as seconds', async () => {
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
            mttd_target_seconds: '18000',
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
    const mttdInput = await screen.findByLabelText('MTTD target hours for SEV-1')
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
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({ severity_level: 1, mttd_target_seconds: 18000 })
  })

  it('shows metric definitions on hover and saves an RTPC target', async () => {
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
            rtpc_target_seconds: '86400',
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

    // Every target column header carries an info icon; the definition text
    // is present twice per icon (sr-only text + the styled hover/focus
    // tooltip), even before any hover interaction.
    expect((await screen.findAllByText(/Mean Time to Detect/i)).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/Resolution to Postmortem Complete/i).length).toBeGreaterThan(0)

    const rtpcInput = screen.getByLabelText('RTPC target hours for SEV-1')
    await user.type(rtpcInput, '24')
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
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({ severity_level: 1, rtpc_target_seconds: 86400 })
  })

  it('opens the leveling criteria editor and saves guidance text', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/config/services' && method === 'GET') return Promise.resolve(jsonResponse({ services: [CHECKOUT] }))
      if (url === '/v1/config/services/checkout/leveling-criteria' && method === 'GET')
        return Promise.resolve(jsonResponse({ criteria: [] }))
      if (url === '/v1/config/services/checkout/leveling-criteria/1' && method === 'PUT') {
        return Promise.resolve(
          jsonResponse({
            service_id: 'checkout',
            severity_level: 1,
            criteria: '>50% of checkout traffic failing',
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
    await user.click(screen.getByRole('button', { name: /leveling criteria for checkout/i }))
    const textarea = await screen.findByLabelText('Leveling criteria for SEV-1')
    await user.type(textarea, '>50% of checkout traffic failing')
    const saveButtons = screen.getAllByRole('button', { name: /^save$/i })
    await user.click(saveButtons[0])

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(
          ([url, init]) => String(url) === '/v1/config/services/checkout/leveling-criteria/1' && init?.method === 'PUT',
        )
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(
        ([url, init]) => String(url) === '/v1/config/services/checkout/leveling-criteria/1' && init?.method === 'PUT',
      )!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({
      severity_level: 1,
      criteria: '>50% of checkout traffic failing',
    })
  })

  it('shows a Clear button and clears saved leveling criteria', async () => {
    let criteria = [
      { service_id: 'checkout', severity_level: 1, criteria: 'existing guidance', created_at: 'now', updated_at: 'now' },
    ]
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/config/services' && method === 'GET') return Promise.resolve(jsonResponse({ services: [CHECKOUT] }))
      if (url === '/v1/config/services/checkout/leveling-criteria' && method === 'GET')
        return Promise.resolve(jsonResponse({ criteria }))
      if (url === '/v1/config/services/checkout/leveling-criteria/1' && method === 'DELETE') {
        criteria = []
        return Promise.resolve(new Response(null, { status: 204 }))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })

    renderWithProviders(<AdminServicesPage />)
    await screen.findByText('Checkout')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /leveling criteria for checkout/i }))
    await screen.findByDisplayValue('existing guidance')
    await user.click(screen.getByRole('button', { name: /^clear$/i }))

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith(
        '/v1/config/services/checkout/leveling-criteria/1',
        expect.objectContaining({ method: 'DELETE' }),
      ),
    )
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
