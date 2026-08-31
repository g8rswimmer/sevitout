import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AssigneePicker } from '@/components/sev/AssigneePicker'
import { renderWithProviders } from '@/test/utils'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('AssigneePicker', () => {
  beforeEach(() => vi.stubGlobal('fetch', vi.fn()))
  afterEach(() => vi.unstubAllGlobals())

  it('does not search until at least 2 characters are typed', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ users: [{ id: '1', name: 'Alice', email: 'alice@example.com', github_username: 'alice-gh' }] }))
    renderWithProviders(<AssigneePicker field="github_username" value="" onSelect={vi.fn()} onClear={vi.fn()} />)

    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Assignee'), 'a')

    expect(fetch).not.toHaveBeenCalled()
  })

  it('filters out directory matches with no value for the requested field', async () => {
    vi.mocked(fetch).mockResolvedValue(
      jsonResponse({
        users: [
          { id: '1', name: 'Alice', email: 'alice@example.com', github_username: 'alice-gh' },
          { id: '2', name: 'Amy', email: 'amy@example.com' }, // no github_username
        ],
      }),
    )
    renderWithProviders(<AssigneePicker field="github_username" value="" onSelect={vi.fn()} onClear={vi.fn()} />)

    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Assignee'), 'a')
    await user.type(screen.getByLabelText('Assignee'), 'm') // "am" — length 2, triggers the search

    await screen.findByText('Alice')
    expect(screen.queryByText('Amy')).not.toBeInTheDocument()
  })

  it('shows a "no match" message when nothing has the field set', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ users: [{ id: '2', name: 'Amy', email: 'amy@example.com' }] }))
    renderWithProviders(<AssigneePicker field="jira_account_id" value="" onSelect={vi.fn()} onClear={vi.fn()} />)

    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Assignee'), 'amy')

    expect(await screen.findByText(/no matching user has a jira account id/i)).toBeInTheDocument()
  })

  it('calls onSelect with the picked user and clears the search text', async () => {
    vi.mocked(fetch).mockResolvedValue(jsonResponse({ users: [{ id: '1', name: 'Alice', email: 'alice@example.com', github_username: 'alice-gh' }] }))
    const onSelect = vi.fn()
    renderWithProviders(<AssigneePicker field="github_username" value="" onSelect={onSelect} onClear={vi.fn()} />)

    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Assignee'), 'al')
    await user.click(await screen.findByText('Alice'))

    expect(onSelect).toHaveBeenCalledWith(expect.objectContaining({ id: '1', name: 'Alice', github_username: 'alice-gh' }))
  })

  it('shows the selected name (not the raw value) with a clear control once a value is set', async () => {
    const onClear = vi.fn()
    renderWithProviders(<AssigneePicker field="github_username" value="alice-gh" selectedName="Alice" onSelect={vi.fn()} onClear={onClear} />)

    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.queryByText('alice-gh')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Assignee')).not.toBeInTheDocument()

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /clear assignee/i }))
    expect(onClear).toHaveBeenCalled()
  })

  it('falls back to the raw value when no selectedName is known', async () => {
    renderWithProviders(<AssigneePicker field="jira_account_id" value="acc-42" onSelect={vi.fn()} onClear={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('acc-42')).toBeInTheDocument())
  })
})
