import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ShareLinkControl } from '@/components/sev/ShareLinkControl'
import { renderWithProviders } from '@/test/utils'
import type { ShareLinkResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const SEV_ID = 'SEV-2026-0001'

const LINK: ShareLinkResponse = {
  id: '1',
  sev_id: SEV_ID,
  token: 'abc123',
  path: '/s/abc123',
  expires_at: '2026-09-23T00:00:00Z',
  created_at: '2026-08-23T00:00:00Z',
}

describe('ShareLinkControl', () => {
  beforeEach(() => {
    // No stored token: ShareLinkControl needs none itself (api.shares.*
    // just omits the Authorization header when absent), and it sidesteps
    // AuthProvider's own whoAmI() fetch firing on mount and consuming the
    // shared mocked Response body before this component's own calls do —
    // Fetch API response bodies can only be read once.
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders nothing when canShare is false', () => {
    renderWithProviders(<ShareLinkControl sevId={SEV_ID} canShare={false} />)
    expect(screen.queryByRole('button', { name: /share/i })).not.toBeInTheDocument()
  })

  it('creates a link and shows the copyable public URL and expiry', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === `/v1/sevs/${SEV_ID}/share` && method === 'POST') {
        const body = JSON.parse(String(init?.body))
        expect(body).toMatchObject({ expires_in_days: 30 })
        return Promise.resolve(jsonResponse(LINK))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<ShareLinkControl sevId={SEV_ID} canShare />)

    const user = userEvent.setup()
    // userEvent.setup() unconditionally installs its own navigator.clipboard
    // stub (for its keyboard copy/paste emulation) — redefining it here,
    // after setup() and before the click that reads it, is what makes it
    // ours instead of a getter-only property in jsdom.
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })

    await user.click(screen.getByRole('button', { name: /^share$/i }))
    await user.click(screen.getByRole('button', { name: /create link/i }))

    const urlField = await screen.findByLabelText('Public share URL')
    expect(urlField).toHaveValue(`${window.location.origin}/s/abc123`)

    await user.click(screen.getByRole('button', { name: /^copy$/i }))
    // copyLink is async (awaits navigator.clipboard.writeText before
    // setCopied(true)) — wait for its visible effect first so the mock
    // assertion below isn't racing a microtask that hasn't run yet.
    expect(await screen.findByText('Copied')).toBeInTheDocument()
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/s/abc123`)
  })

  it('closes via an explicit Close button, without revoking the link', async () => {
    vi.mocked(fetch).mockImplementation(() => Promise.resolve(jsonResponse(LINK)))
    renderWithProviders(<ShareLinkControl sevId={SEV_ID} canShare />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^share$/i }))
    await user.click(screen.getByRole('button', { name: /create link/i }))
    await screen.findByLabelText('Public share URL')

    await user.click(screen.getByRole('button', { name: /^close$/i }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(
      vi.mocked(fetch).mock.calls.some(([, init]) => init?.method === 'DELETE'),
    ).toBe(false)

    // Reopening still shows the same link — Close only dismisses the dialog.
    await user.click(screen.getByRole('button', { name: /^share$/i }))
    expect(screen.getByLabelText('Public share URL')).toHaveValue(`${window.location.origin}/s/abc123`)
  })

  it('closes the create form via Cancel without creating a link', async () => {
    renderWithProviders(<ShareLinkControl sevId={SEV_ID} canShare />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^share$/i }))
    await user.click(screen.getByRole('button', { name: /^cancel$/i }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('keeps the created link visible after closing and reopening the dialog', async () => {
    vi.mocked(fetch).mockImplementation(() => Promise.resolve(jsonResponse(LINK)))
    renderWithProviders(<ShareLinkControl sevId={SEV_ID} canShare />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^share$/i }))
    await user.click(screen.getByRole('button', { name: /create link/i }))
    await screen.findByLabelText('Public share URL')

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /^share$/i }))
    expect(screen.getByLabelText('Public share URL')).toHaveValue(`${window.location.origin}/s/abc123`)
  })

  it('revokes the link and returns to the create form', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === `/v1/sevs/${SEV_ID}/share` && method === 'POST') return Promise.resolve(jsonResponse(LINK))
      if (url === `/v1/sevs/${SEV_ID}/share/abc123` && method === 'DELETE')
        return Promise.resolve(new Response(null, { status: 204 }))
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<ShareLinkControl sevId={SEV_ID} canShare />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^share$/i }))
    await user.click(screen.getByRole('button', { name: /create link/i }))
    await screen.findByLabelText('Public share URL')

    await user.click(screen.getByRole('button', { name: /revoke link/i }))

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith(
        `/v1/sevs/${SEV_ID}/share/abc123`,
        expect.objectContaining({ method: 'DELETE' }),
      ),
    )
    expect(await screen.findByLabelText('Expires in (days)')).toBeInTheDocument()
  })

  it('shows the server error message on a failed create (e.g. sensitive SEV)', async () => {
    vi.mocked(fetch).mockImplementation(() =>
      Promise.resolve(jsonResponse({ message: 'sensitive SEVs cannot have shareable links generated' }, 400)),
    )
    renderWithProviders(<ShareLinkControl sevId={SEV_ID} canShare />)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^share$/i }))
    await user.click(screen.getByRole('button', { name: /create link/i }))

    expect(await screen.findByText(/sensitive sevs cannot have shareable links/i)).toBeInTheDocument()
  })
})
