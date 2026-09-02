import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SevCreatePage } from '@/pages/SevCreatePage'
import { renderWithProviders } from '@/test/utils'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('SevCreatePage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('submits the form with the expected fields', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      return Promise.resolve(jsonResponse({ id: 'SEV-2026-0042' }))
    })

    renderWithProviders(<SevCreatePage />)
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('Title'), 'Payments API errors')
    await user.selectOptions(screen.getByLabelText('Severity level'), '2')
    await user.type(screen.getByLabelText('Description'), 'Elevated 500s')

    const serviceInput = screen.getByPlaceholderText(/type a service name/i)
    await user.type(serviceInput, 'payments{enter}')
    expect(screen.getByText('payments')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /create sev/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === '/v1/sevs')
      expect(call).toBeDefined()
    })
    const [, init] = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === '/v1/sevs')!
    const body = JSON.parse(String(init!.body))
    expect(body).toMatchObject({
      title: 'Payments API errors',
      severity_level: 2,
      description: 'Elevated 500s',
      affected_services: ['payments'],
    })
  })

  it('submits detection method, monitoring tool, and the link/query fields', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      return Promise.resolve(jsonResponse({ id: 'SEV-2026-0042' }))
    })

    renderWithProviders(<SevCreatePage />)
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('Title'), 'Checkout errors')
    await user.selectOptions(screen.getByLabelText('Detection method'), 'Monitoring Dashboard')
    await user.selectOptions(screen.getByLabelText('Monitoring tool'), 'Datadog')
    await user.type(screen.getByLabelText('Alert name'), 'checkout-p99-latency')
    await user.type(screen.getByLabelText('Alert link'), 'https://alerts.example.com/1')
    await user.type(screen.getByLabelText(/dashboard link/i), 'https://metrics.example.com/q/1')
    await user.type(screen.getByLabelText(/snapshot image link/i), 'https://img.example.com/1.png')
    await user.type(screen.getByLabelText('Saved query'), 'up{{job="checkout"} == 0')

    // Alert name reads directly above its link, not separated by the
    // detection method/monitoring tool dropdowns.
    const alertName = screen.getByLabelText('Alert name')
    const alertLink = screen.getByLabelText('Alert link')
    expect(alertName.compareDocumentPosition(alertLink) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    await user.click(screen.getByRole('button', { name: /create sev/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === '/v1/sevs')
      expect(call).toBeDefined()
    })
    const [, init] = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === '/v1/sevs')!
    const body = JSON.parse(String(init!.body))
    expect(body).toMatchObject({
      detection_method: 'monitoring-dashboard',
      monitoring_tool: 'datadog',
      alert_name: 'checkout-p99-latency',
      alert_url: 'https://alerts.example.com/1',
      dashboard_url: 'https://metrics.example.com/q/1',
      query: 'up{job="checkout"} == 0',
      snapshot_url: 'https://img.example.com/1.png',
    })
    // started_at/detected_at were never touched — the caller must set them
    // explicitly; the API shouldn't be asked to default them to "now".
    expect(body.started_at).toBeUndefined()
    expect(body.detected_at).toBeUndefined()
  })

  it('opens the native date/time picker when the calendar button is clicked', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      return Promise.resolve(jsonResponse({ id: 'SEV-2026-0042' }))
    })

    renderWithProviders(<SevCreatePage />)
    const showPicker = vi.fn()
    // jsdom doesn't implement showPicker(); stub it to confirm the button
    // actually invokes the input's own picker rather than being decorative.
    HTMLInputElement.prototype.showPicker = showPicker

    try {
      const user = userEvent.setup()
      await user.click(screen.getByRole('button', { name: /open date\/time picker for started at/i }))

      expect(showPicker).toHaveBeenCalledOnce()
    } finally {
      delete (HTMLInputElement.prototype as { showPicker?: unknown }).showPicker
    }
  })

  it('refetches leveling criteria when severity or affected services change', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      if (url.startsWith('/v1/config/leveling-criteria')) return Promise.resolve(jsonResponse({ criteria: [] }))
      return Promise.resolve(jsonResponse({ id: 'SEV-2026-0042' }))
    })

    renderWithProviders(<SevCreatePage />)
    const user = userEvent.setup()

    // No services selected yet — the panel makes no request at all.
    expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url).startsWith('/v1/config/leveling-criteria'))).toBe(false)

    const serviceInput = screen.getByPlaceholderText(/type a service name/i)
    await user.type(serviceInput, 'checkout{enter}')

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url).startsWith('/v1/config/leveling-criteria'))
      expect(call).toBeDefined()
    })
    let call = vi.mocked(fetch).mock.calls.find(([url]) => String(url).startsWith('/v1/config/leveling-criteria'))!
    let requestedUrl = new URL(String(call[0]), 'http://localhost')
    expect(requestedUrl.searchParams.getAll('service_ids')).toEqual(['checkout'])
    expect(requestedUrl.searchParams.get('severity_level')).toBe('3') // default severity

    await user.selectOptions(screen.getByLabelText('Severity level'), '1')

    await waitFor(() => {
      const calls = vi.mocked(fetch).mock.calls.filter(([url]) => String(url).startsWith('/v1/config/leveling-criteria'))
      expect(calls.some(([url]) => new URL(String(url), 'http://localhost').searchParams.get('severity_level') === '1')).toBe(
        true,
      )
    })
    call = vi
      .mocked(fetch)
      .mock.calls.filter(([url]) => String(url).startsWith('/v1/config/leveling-criteria'))
      .find(([url]) => new URL(String(url), 'http://localhost').searchParams.get('severity_level') === '1')!
    requestedUrl = new URL(String(call[0]), 'http://localhost')
    expect(requestedUrl.searchParams.getAll('service_ids')).toEqual(['checkout'])
  })

  it('shows the server error message when creation fails', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      return Promise.resolve(jsonResponse({ message: 'title is required' }, 400))
    })

    renderWithProviders(<SevCreatePage />)
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Title'), 'x')
    await user.click(screen.getByRole('button', { name: /create sev/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent('title is required')
  })
})
