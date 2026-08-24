import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminRetentionPage } from '@/pages/admin/AdminRetentionPage'
import { renderWithProviders } from '@/test/utils'
import type { RetentionConfigResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const SEV1: RetentionConfigResponse = {
  severity_level: 1,
  retention_days: 365,
  hard_delete: false,
  created_at: '2026-08-23T20:00:00Z',
  updated_at: '2026-08-23T20:00:00Z',
}

describe('AdminRetentionPage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders all four severity levels, filling in unconfigured ones with defaults', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ configs: [SEV1] }))
    renderWithProviders(<AdminRetentionPage />)

    expect(await screen.findByText('SEV-1')).toBeInTheDocument()
    expect(screen.getByText('SEV-2')).toBeInTheDocument()
    expect(screen.getByText('SEV-3')).toBeInTheDocument()
    expect(screen.getByText('SEV-4')).toBeInTheDocument()
    expect(screen.getByLabelText('Retention days for SEV-1')).toHaveValue(365)
    expect(screen.getByLabelText('Retention days for SEV-2')).toHaveValue(0)
    expect(screen.getAllByText('Not set')).toHaveLength(3)
  })

  it('updates a severity level\'s retention policy', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/config/retention' && method === 'GET') return Promise.resolve(jsonResponse({ configs: [SEV1] }))
      if (url === '/v1/config/retention/2' && method === 'PUT') return Promise.resolve(jsonResponse({ severity_level: 2, retention_days: 90, hard_delete: true }))
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<AdminRetentionPage />)
    await screen.findByText('SEV-1')

    const user = userEvent.setup()
    const sev2Input = screen.getByLabelText('Retention days for SEV-2')
    await user.clear(sev2Input)
    await user.type(sev2Input, '90')
    await user.click(screen.getByLabelText('Hard delete for SEV-2'))
    const rows = screen.getAllByRole('button', { name: /^save$/i })
    await user.click(rows[1])

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/retention/2' && init?.method === 'PUT')
      expect(call).toBeDefined()
    })
    const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/retention/2' && init?.method === 'PUT')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({ severity_level: 2, retention_days: 90, hard_delete: true })
  })
})
