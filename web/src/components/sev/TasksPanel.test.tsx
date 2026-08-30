import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TasksPanel } from '@/components/sev/TasksPanel'
import { renderWithProviders } from '@/test/utils'
import type { ListTasksResponse, TaskResponse } from '@/types/api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const SEV_ID = 'SEV-2026-0001'

function task(overrides: Partial<TaskResponse>): TaskResponse {
  return {
    id: '1',
    sev_id: SEV_ID,
    external_system: 'generic',
    task_id: 'https://example.com/1',
    url: 'https://example.com/1',
    title: 'A linked task',
    relationship_type: 'action-item',
    priority: 'non-critical',
    created_at: '2026-08-23T00:00:00Z',
    ...overrides,
  }
}

describe('TasksPanel', () => {
  beforeEach(() => {
    // No stored token, same rationale as ShareLinkControl.test.tsx: sidesteps
    // AuthProvider's own whoAmI() fetch on mount consuming the shared mocked
    // Response body before this component's own calls do.
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => vi.unstubAllGlobals())

  it('labels each linked task with its tracker, falling back to the raw value for an unknown one', async () => {
    const list: ListTasksResponse = {
      tasks: [
        task({ id: '1', external_system: 'github', title: 'A GitHub issue' }),
        task({ id: '2', external_system: 'jira', title: 'A Jira issue' }),
        task({ id: '3', external_system: 'generic', title: 'A plain link' }),
        task({ id: '4', external_system: 'linear', title: 'Something else' }),
      ],
    }
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url === `/v1/sevs/${SEV_ID}/tasks`) return Promise.resolve(jsonResponse(list))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    renderWithProviders(<TasksPanel sevId={SEV_ID} canManage={false} />)

    const githubItem = (await screen.findByText('A GitHub issue')).closest('li')!
    expect(within(githubItem).getByText('GitHub')).toBeInTheDocument()

    const jiraItem = screen.getByText('A Jira issue').closest('li')!
    expect(within(jiraItem).getByText('Jira')).toBeInTheDocument()

    const genericItem = screen.getByText('A plain link').closest('li')!
    expect(within(genericItem).getByText('Link')).toBeInTheDocument()

    // No badge/label is known for "linear" — the raw value renders as-is
    // rather than being silently dropped or mislabeled.
    const otherItem = screen.getByText('Something else').closest('li')!
    expect(within(otherItem).getByText('linear')).toBeInTheDocument()
  })

  it('hides the create/link controls when canManage is false', async () => {
    vi.mocked(fetch).mockImplementation((input) => {
      const url = String(input)
      if (url === `/v1/sevs/${SEV_ID}/tasks`) return Promise.resolve(jsonResponse({ tasks: [] }))
      return Promise.reject(new Error(`unexpected fetch: ${url}`))
    })
    renderWithProviders(<TasksPanel sevId={SEV_ID} canManage={false} />)

    await screen.findByText('No tasks linked yet.')
    expect(screen.queryByRole('button', { name: /create jira issue/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /create github issue/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /link existing/i })).not.toBeInTheDocument()
  })

  it('creates a Jira issue with the expected fields and resets the form', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === `/v1/sevs/${SEV_ID}/tasks` && method === 'GET') return Promise.resolve(jsonResponse({ tasks: [] }))
      if (url === `/v1/sevs/${SEV_ID}/jira-issues` && method === 'POST') {
        return Promise.resolve(jsonResponse(task({ id: '9', external_system: 'jira', title: 'Checkout errors' })))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<TasksPanel sevId={SEV_ID} canManage />)

    await screen.findByText('No tasks linked yet.')
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: /create jira issue/i }))

    // Switching mode swaps the owner/repo pair for project-key/issue-type,
    // and the shared title field relabels to "Summary" (Jira's naming).
    expect(screen.queryByLabelText('Owner')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Title')).not.toBeInTheDocument()

    await user.type(screen.getByLabelText('Project key'), 'OPS')
    await user.type(screen.getByLabelText('Issue type'), 'Bug')
    await user.type(screen.getByLabelText('Summary'), 'Checkout errors')
    await user.type(screen.getByLabelText('Description'), 'Elevated 500s on checkout')

    await user.click(screen.getByRole('button', { name: /create issue/i }))

    await waitFor(() => {
      const call = vi.mocked(fetch).mock.calls.find(([url]) => String(url) === `/v1/sevs/${SEV_ID}/jira-issues`)
      expect(call).toBeDefined()
    })
    const [, init] = vi.mocked(fetch).mock.calls.find(
      ([url]) => String(url) === `/v1/sevs/${SEV_ID}/jira-issues`,
    )!
    const body = JSON.parse(String(init!.body))
    expect(body).toMatchObject({
      project_key: 'OPS',
      issue_type: 'Bug',
      summary: 'Checkout errors',
      description: 'Elevated 500s on checkout',
      relationship_type: 'action-item',
      priority: 'non-critical',
    })

    // Form resets after a successful create.
    await waitFor(() => expect(screen.getByLabelText('Project key')).toHaveValue(''))
    expect(screen.getByLabelText('Summary')).toHaveValue('')
  })

  it('shows the server error message on a failed Jira create', async () => {
    vi.mocked(fetch).mockImplementation((input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === `/v1/sevs/${SEV_ID}/tasks` && method === 'GET') return Promise.resolve(jsonResponse({ tasks: [] }))
      if (url === `/v1/sevs/${SEV_ID}/jira-issues` && method === 'POST') {
        return Promise.resolve(jsonResponse({ message: 'Jira integration is not configured' }, 503))
      }
      return Promise.reject(new Error(`unexpected fetch: ${method} ${url}`))
    })
    renderWithProviders(<TasksPanel sevId={SEV_ID} canManage />)

    await screen.findByText('No tasks linked yet.')
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: /create jira issue/i }))
    await user.type(screen.getByLabelText('Project key'), 'OPS')
    await user.type(screen.getByLabelText('Issue type'), 'Bug')
    await user.type(screen.getByLabelText('Summary'), 'Checkout errors')
    await user.click(screen.getByRole('button', { name: /create issue/i }))

    expect(await screen.findByText(/jira integration is not configured/i)).toBeInTheDocument()
  })
})
