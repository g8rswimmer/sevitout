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

  it('submits detection method, a custom monitoring tool, and the link fields', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url.startsWith('/v1/config/services')) return Promise.resolve(jsonResponse({}))
      return Promise.resolve(jsonResponse({ id: 'SEV-2026-0042' }))
    })

    renderWithProviders(<SevCreatePage />)
    const user = userEvent.setup()

    await user.type(screen.getByLabelText('Title'), 'Checkout errors')
    await user.selectOptions(screen.getByLabelText('Detection method'), 'Monitoring Dashboard')
    await user.selectOptions(screen.getByLabelText('Monitoring tool'), 'Other…')
    await user.type(screen.getByLabelText(/custom monitoring tool name/i), 'In-house dashboard')
    await user.type(screen.getByLabelText('Alert link'), 'https://alerts.example.com/1')
    await user.type(screen.getByLabelText(/metric \/ dashboard link/i), 'https://metrics.example.com/q/1')
    await user.type(screen.getByLabelText(/snapshot image link/i), 'https://img.example.com/1.png')

    await user.click(screen.getByRole('button', { name: /create sev/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === '/v1/sevs')
      expect(call).toBeDefined()
    })
    const [, init] = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === '/v1/sevs')!
    const body = JSON.parse(String(init!.body))
    expect(body).toMatchObject({
      detection_method: 'monitoring-dashboard',
      monitoring_tool: 'In-house dashboard',
      alert_url: 'https://alerts.example.com/1',
      metric_link: 'https://metrics.example.com/q/1',
      snapshot_url: 'https://img.example.com/1.png',
    })
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
