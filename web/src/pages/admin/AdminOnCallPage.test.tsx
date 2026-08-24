import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminOnCallPage } from '@/pages/admin/AdminOnCallPage'
import { renderWithProviders } from '@/test/utils'
import type { OnCallRotationResponse, ServiceResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const CHECKOUT: ServiceResponse = {
  id: 'checkout',
  name: 'Checkout',
  created_at: '2026-08-23T20:00:00Z',
  updated_at: '2026-08-23T20:00:00Z',
}

const PRIMARY: OnCallRotationResponse = {
  id: '1',
  name: 'Primary',
  service_id: 'checkout',
  pagerduty_schedule_id: 'PSCHED1',
  created_at: '2026-08-23T20:00:00Z',
  updated_at: '2026-08-23T20:00:00Z',
}

function mockFetch(rotations: OnCallRotationResponse[]) {
  let list = rotations
  vi.mocked(fetch).mockImplementation((input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/v1/config/services') return Promise.resolve(jsonResponse({ services: [CHECKOUT] }))
    if (url === '/v1/config/oncall' && method === 'GET') return Promise.resolve(jsonResponse({ rotations: list }))
    if (url === '/v1/config/oncall' && method === 'POST') {
      const body = JSON.parse(String(init?.body))
      const created = { ...body, id: '2', created_at: 'now', updated_at: 'now' }
      list = [...list, created]
      return Promise.resolve(jsonResponse(created))
    }
    if (url === '/v1/config/oncall/1' && method === 'PATCH') {
      const body = JSON.parse(String(init?.body))
      list = list.map((r) => (r.id === '1' ? { ...r, ...body } : r))
      return Promise.resolve(jsonResponse(list[0]))
    }
    if (url === '/v1/config/oncall/1' && method === 'DELETE') {
      list = list.filter((r) => r.id !== '1')
      return Promise.resolve(new Response(null, { status: 204 }))
    }
    return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
  })
}

describe('AdminOnCallPage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders existing rotations with their resolved service name', async () => {
    mockFetch([PRIMARY])
    renderWithProviders(<AdminOnCallPage />)

    expect(await screen.findByText('Primary')).toBeInTheDocument()
    expect(await screen.findByText('Checkout')).toBeInTheDocument()
    expect(screen.getByText('PSCHED1')).toBeInTheDocument()
  })

  it('creates a new rotation', async () => {
    mockFetch([PRIMARY])
    renderWithProviders(<AdminOnCallPage />)
    await screen.findByText('Primary')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /new rotation/i }))
    await user.type(screen.getByLabelText('Name'), 'Secondary')
    await user.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/oncall' && init?.method === 'POST')
      expect(call).toBeDefined()
    })
    const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/oncall' && init?.method === 'POST')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({ name: 'Secondary' })
  })

  it('edits a rotation', async () => {
    mockFetch([PRIMARY])
    renderWithProviders(<AdminOnCallPage />)
    await screen.findByText('Primary')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /edit primary/i }))
    const nameInput = screen.getByLabelText('Name')
    await user.clear(nameInput)
    await user.type(nameInput, 'Primary (updated)')
    await user.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/oncall/1' && init?.method === 'PATCH')
      expect(call).toBeDefined()
    })
  })

  it('deletes a rotation', async () => {
    mockFetch([PRIMARY])
    renderWithProviders(<AdminOnCallPage />)
    await screen.findByText('Primary')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /delete primary/i }))

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith('/v1/config/oncall/1', expect.objectContaining({ method: 'DELETE' })),
    )
  })
})
