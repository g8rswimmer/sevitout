import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { PostmortemPage } from '@/pages/PostmortemPage'
import { tokenStorage } from '@/lib/api'
import { renderWithProviders } from '@/test/utils'
import { MockWebSocket } from '@/test/mockWebSocket'
import type { AIOutputResponse, OrgRole, PostmortemResponse, SEVResponse, WhoAmIResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const SEV_ID = 'SEV-2026-0001'

function makeSev(overrides: Partial<SEVResponse> = {}): SEVResponse {
  return {
    id: SEV_ID,
    title: 'Database outage',
    severity_level: 1,
    status: 'postmortem_in_progress',
    created_at: '2026-08-23T20:00:00Z',
    updated_at: '2026-08-23T20:00:00Z',
    ...overrides,
  }
}

function makePostmortem(overrides: Partial<PostmortemResponse> = {}): PostmortemResponse {
  return {
    id: '1',
    sev_id: SEV_ID,
    status: 'draft',
    content: 'Initial content.',
    created_at: '2026-08-23T20:00:00Z',
    updated_at: '2026-08-23T20:00:00Z',
    ...overrides,
  }
}

function me(org_role: OrgRole): WhoAmIResponse {
  return { id: 'u1', email: 'a@b.com', name: 'Ada', org_role }
}

function mockFetchFor(opts: {
  sev: SEVResponse
  pm: PostmortemResponse
  whoAmI: WhoAmIResponse
  aiOutputs?: AIOutputResponse[]
  hasPlugins?: boolean
}) {
  let pm = opts.pm
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/v1/auth/me') return Promise.resolve(jsonResponse(opts.whoAmI))
      if (url === `/v1/sevs/${SEV_ID}` && method === 'GET') return Promise.resolve(jsonResponse(opts.sev))
      if (url === `/v1/sevs/${SEV_ID}/postmortem` && method === 'GET') return Promise.resolve(jsonResponse(pm))
      if (url === `/v1/sevs/${SEV_ID}/postmortem` && method === 'PATCH') {
        const body = JSON.parse(String(init?.body))
        pm = { ...pm, content: body.content }
        return Promise.resolve(jsonResponse(pm))
      }
      if (url === `/v1/sevs/${SEV_ID}/postmortem/transition` && method === 'POST') {
        const body = JSON.parse(String(init?.body))
        pm = { ...pm, status: body.to_status }
        return Promise.resolve(jsonResponse(pm))
      }
      if (url === `/v1/sevs/${SEV_ID}/unlock` && method === 'POST') {
        return Promise.resolve(jsonResponse({ unlock_token: 'unlock-token-abc' }))
      }
      if (url === `/v1/sevs/${SEV_ID}/ai/outputs`) return Promise.resolve(jsonResponse({ outputs: opts.aiOutputs ?? [] }))
      if (url === '/v1/ai/plugins')
        return Promise.resolve(jsonResponse(opts.hasPlugins ? { plugins: [{ id: '1', name: 'p', provider: 'anthropic' }] } : {}))
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    }),
  )
}

function renderPage() {
  return renderWithProviders(
    <Routes>
      <Route path="/sevs/:id/postmortem" element={<PostmortemPage />} />
    </Routes>,
    { route: `/sevs/${SEV_ID}/postmortem` },
  )
}

describe('PostmortemPage', () => {
  beforeEach(() => {
    localStorage.clear()
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders read-only for a Viewer, with no Edit/Unlock/transition controls', async () => {
    tokenStorage.set('tok')
    mockFetchFor({ sev: makeSev(), pm: makePostmortem(), whoAmI: me('viewer') })
    renderPage()

    expect(await screen.findByRole('heading', { name: /postmortem: database outage/i })).toBeInTheDocument()
    expect(screen.getByText('Draft')).toBeInTheDocument()
    expect(screen.getByText('Initial content.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^edit$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /unlock to edit/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/transition to:/i)).not.toBeInTheDocument()
  })

  it('lets a Responder edit and save an unlocked postmortem', async () => {
    tokenStorage.set('tok')
    mockFetchFor({ sev: makeSev(), pm: makePostmortem(), whoAmI: me('responder') })
    renderPage()

    await screen.findByRole('heading', { name: /postmortem/i })
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^edit$/i }))

    await waitFor(() => expect(document.querySelector('.ProseMirror')).toHaveAttribute('contenteditable', 'true'))

    await user.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === `/v1/sevs/${SEV_ID}/postmortem` && init?.method === 'PATCH')
      expect(call).toBeDefined()
    })
  })

  it('seeds a blank postmortem with a template built from the SEV facts on first edit', async () => {
    tokenStorage.set('tok')
    mockFetchFor({
      sev: makeSev({ description: 'Checkout was unavailable.', business_impact: 'Lost revenue.' }),
      pm: makePostmortem({ content: '' }),
      whoAmI: me('responder'),
    })
    renderPage()

    await screen.findByRole('heading', { name: /postmortem/i })
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^edit$/i }))

    await waitFor(() => expect(document.querySelector('.ProseMirror')).toHaveAttribute('contenteditable', 'true'))
    expect(screen.getByText('Checkout was unavailable.')).toBeInTheDocument()
    expect(screen.getByText('Lost revenue.')).toBeInTheDocument()
    expect(screen.getByText('Summary')).toBeInTheDocument()
    expect(screen.getByText('Lifecycle')).toBeInTheDocument()
    expect(screen.getByText('Root Cause')).toBeInTheDocument()
    expect(screen.getByText('Business Impact')).toBeInTheDocument()
    expect(screen.getByText('Services Affected')).toBeInTheDocument()
    expect(screen.getByText('Mitigation')).toBeInTheDocument()
  })

  it('does not overwrite existing postmortem content with the template on edit', async () => {
    tokenStorage.set('tok')
    mockFetchFor({
      sev: makeSev({ description: 'Checkout was unavailable.' }),
      pm: makePostmortem({ content: 'Already written by a human.' }),
      whoAmI: me('responder'),
    })
    renderPage()

    await screen.findByRole('heading', { name: /postmortem/i })
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /^edit$/i }))

    await waitFor(() => expect(document.querySelector('.ProseMirror')).toHaveAttribute('contenteditable', 'true'))
    expect(screen.getByText('Already written by a human.')).toBeInTheDocument()
    expect(screen.queryByText('Checkout was unavailable.')).not.toBeInTheDocument()
  })

  it('does not offer Unlock to a Responder on a locked SEV (Incident-Commander-only)', async () => {
    tokenStorage.set('tok')
    mockFetchFor({ sev: makeSev({ locked: true, status: 'postmortem_complete' }), pm: makePostmortem({ status: 'approved' }), whoAmI: me('responder') })
    renderPage()

    await screen.findByRole('heading', { name: /postmortem/i })
    expect(screen.getByText('SEV Locked')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^edit$/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /unlock to edit/i })).not.toBeInTheDocument()
  })

  it('lets an Incident Commander unlock a locked SEV, edit, and save with the unlock token', async () => {
    tokenStorage.set('tok')
    mockFetchFor({
      sev: makeSev({ locked: true, status: 'postmortem_complete' }),
      pm: makePostmortem({ status: 'approved' }),
      whoAmI: me('incident-commander'),
    })
    renderPage()

    await screen.findByRole('heading', { name: /postmortem/i })
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /unlock to edit/i }))

    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Reason'), 'fixing a typo')
    await user.click(within(dialog).getByRole('button', { name: /^unlock$/i }))

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith(
        `/v1/sevs/${SEV_ID}/unlock`,
        expect.objectContaining({ method: 'POST' }),
      ),
    )
    await waitFor(() => expect(document.querySelector('.ProseMirror')).toHaveAttribute('contenteditable', 'true'))

    await user.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      const call = vi
        .mocked(fetch)
        .mock.calls.find(([url, init]) => String(url) === `/v1/sevs/${SEV_ID}/postmortem` && init?.method === 'PATCH')
      expect(call).toBeDefined()
    })
    const call = vi
      .mocked(fetch)
      .mock.calls.find(([url, init]) => String(url) === `/v1/sevs/${SEV_ID}/postmortem` && init?.method === 'PATCH')!
    expect(JSON.parse(String(call[1]!.body))).toMatchObject({ unlock_token: 'unlock-token-abc' })
  })

  it('shows status transition buttons for an Incident Commander', async () => {
    tokenStorage.set('tok')
    mockFetchFor({ sev: makeSev(), pm: makePostmortem({ status: 'draft' }), whoAmI: me('incident-commander') })
    renderPage()

    await screen.findByRole('heading', { name: /postmortem/i })
    expect(screen.getByText(/transition to:/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'In Review' })).toBeInTheDocument()
  })

  it('shows an existing AI draft and applies it into the editor', async () => {
    tokenStorage.set('tok')
    mockFetchFor({
      sev: makeSev(),
      pm: makePostmortem({ content: 'human-written content' }),
      whoAmI: me('responder'),
      aiOutputs: [
        {
          id: '1',
          sev_id: SEV_ID,
          action: 'AI_ACTION_DRAFT_POSTMORTEM',
          content: 'AI drafted summary text',
          created_at: '2026-08-23T20:05:00Z',
        },
      ],
    })
    renderPage()

    await screen.findByRole('heading', { name: /postmortem/i })
    expect(await screen.findByText(/ai-generated/i)).toBeInTheDocument()
    expect(screen.getByText('AI drafted summary text')).toBeInTheDocument()

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /apply to editor/i }))

    await waitFor(() => expect(screen.getAllByText('AI drafted summary text').length).toBeGreaterThanOrEqual(2))
  })

  it('downloads the current content as Markdown, including unsaved edits while editing', async () => {
    tokenStorage.set('tok')
    mockFetchFor({
      sev: makeSev(),
      pm: makePostmortem({ content: 'Saved content.' }),
      whoAmI: me('responder'),
      aiOutputs: [
        {
          id: '1',
          sev_id: SEV_ID,
          action: 'AI_ACTION_DRAFT_POSTMORTEM',
          content: 'Unsaved draft content.',
          created_at: '2026-08-23T20:05:00Z',
        },
      ],
    })
    renderPage()
    await screen.findByRole('heading', { name: /postmortem/i })

    let anchor: HTMLAnchorElement | undefined
    const createElement = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      const el = createElement(tag)
      if (tag === 'a') anchor = el as HTMLAnchorElement
      return el
    })
    const createObjectURL = vi.fn((_blob: Blob) => 'blob:mock-url')
    vi.stubGlobal('URL', { ...URL, createObjectURL, revokeObjectURL: vi.fn() })
    const originalClick = HTMLAnchorElement.prototype.click
    const click = vi.fn<() => void>()
    HTMLAnchorElement.prototype.click = click

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /download \.md/i }))

    expect(anchor?.download).toBe(`${SEV_ID}-postmortem.md`)
    const savedBlob = createObjectURL.mock.calls[0][0] as Blob
    expect(await savedBlob.text()).toBe('Saved content.')
    expect(click).toHaveBeenCalledTimes(1)

    // Apply the AI draft (enters edit mode with unsaved content — see
    // handleApplyDraft) and download again — should reflect the draft, not
    // the last-saved content. This avoids simulating real keystrokes into
    // the ProseMirror contenteditable, which jsdom can't fully support.
    await screen.findByText(/ai-generated/i)
    await user.click(screen.getByRole('button', { name: /apply to editor/i }))
    await user.click(screen.getByRole('button', { name: /download \.md/i }))

    const draftBlob = createObjectURL.mock.calls[1][0] as Blob
    expect(await draftBlob.text()).toContain('Unsaved draft content.')

    HTMLAnchorElement.prototype.click = originalClick
  })

  it('prints the current document for "Download PDF", suggesting a filename via document.title', async () => {
    tokenStorage.set('tok')
    mockFetchFor({ sev: makeSev(), pm: makePostmortem(), whoAmI: me('responder') })
    renderPage()
    await screen.findByRole('heading', { name: /postmortem/i })

    const originalTitle = document.title
    const print = vi.fn()
    vi.stubGlobal('print', print)

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /download pdf/i }))

    expect(print).toHaveBeenCalledTimes(1)
    expect(document.title).toBe(`${SEV_ID}-postmortem`)

    window.dispatchEvent(new Event('afterprint'))
    expect(document.title).toBe(originalTitle)
  })
})
