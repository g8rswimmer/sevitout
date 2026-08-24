import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AdminAIPluginsPage } from '@/pages/admin/AdminAIPluginsPage'
import { renderWithProviders } from '@/test/utils'
import type { AIPluginResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const CLAUDE: AIPluginResponse = {
  id: '1',
  name: 'Claude',
  handler_type: 'builtin',
  provider: 'anthropic',
  model: 'claude-sonnet-5',
  api_key_configured: true,
  enabled: true,
  created_at: '2026-08-23T20:00:00Z',
  updated_at: '2026-08-23T20:00:00Z',
}

function mockFetch(plugins: AIPluginResponse[] = [CLAUDE]) {
  let list = plugins
  vi.mocked(fetch).mockImplementation((input, init) => {
    const url = String(input)
    const method = init?.method ?? 'GET'
    if (url === '/v1/config/ai-plugins' && method === 'GET') return Promise.resolve(jsonResponse({ plugins: list }))
    if (url === '/v1/config/ai-plugins' && method === 'POST') {
      const body = JSON.parse(String(init?.body))
      const created = { ...body, id: '2', created_at: 'now', updated_at: 'now' }
      list = [...list, created]
      return Promise.resolve(jsonResponse(created))
    }
    if (url === '/v1/config/ai-plugins/1' && method === 'PATCH') {
      const body = JSON.parse(String(init?.body))
      list = list.map((p) => (p.id === '1' ? { ...p, ...body } : p))
      return Promise.resolve(jsonResponse(list[0]))
    }
    if (url === '/v1/config/ai-plugins/1' && method === 'DELETE') {
      list = list.filter((p) => p.id !== '1')
      return Promise.resolve(new Response(null, { status: 204 }))
    }
    return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
  })
}

describe('AdminAIPluginsPage', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('renders registered plugins', async () => {
    mockFetch()
    renderWithProviders(<AdminAIPluginsPage />)

    expect(await screen.findByText('Claude')).toBeInTheDocument()
    expect(screen.getByText('anthropic / claude-sonnet-5')).toBeInTheDocument()
    expect(screen.getByText('Enabled')).toBeInTheDocument()
  })

  it('creates a new plugin, always sending the boolean/rate-limit fields explicitly', async () => {
    mockFetch()
    renderWithProviders(<AdminAIPluginsPage />)
    await screen.findByText('Claude')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /new plugin/i }))
    await user.type(screen.getByLabelText('Name'), 'GPT Plugin')
    await user.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url, init]) => String(url) === '/v1/config/ai-plugins' && init?.method === 'POST')
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/ai-plugins' && init?.method === 'POST')!
    const body = JSON.parse(String(call[1]!.body))
    expect(body).toMatchObject({
      name: 'GPT Plugin',
      handler_type: 'builtin',
      enabled: true,
      trigger_on_open: false,
      rate_limit_per_minute: 0,
    })
  })

  it('shows the HTTP endpoint field only when handler type is http', async () => {
    mockFetch()
    renderWithProviders(<AdminAIPluginsPage />)
    await screen.findByText('Claude')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /new plugin/i }))
    expect(screen.queryByLabelText('HTTP endpoint')).not.toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Handler type'), 'http')
    expect(screen.getByLabelText('HTTP endpoint')).toBeInTheDocument()
  })

  it('edits a plugin and deletes it', async () => {
    mockFetch()
    renderWithProviders(<AdminAIPluginsPage />)
    await screen.findByText('Claude')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /edit claude/i }))
    await user.click(screen.getByLabelText('Enabled'))
    await user.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === '/v1/config/ai-plugins/1' && init?.method === 'PATCH')
      expect(call).toBeDefined()
    })
    const patchCall = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === '/v1/config/ai-plugins/1' && init?.method === 'PATCH')!
    expect(JSON.parse(String(patchCall[1]!.body))).toMatchObject({ enabled: false })

    await user.click(screen.getByRole('button', { name: /delete claude/i }))
    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith('/v1/config/ai-plugins/1', expect.objectContaining({ method: 'DELETE' })),
    )
  })
})
