import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { PostmortemEditor } from '@/components/postmortem/PostmortemEditor'

describe('PostmortemEditor', () => {
  it('renders markdown content as formatted output', async () => {
    render(<PostmortemEditor content={'# Summary\n\nSomething broke.'} editable={false} />)
    expect(await screen.findByRole('heading', { name: 'Summary', level: 1 })).toBeInTheDocument()
    expect(screen.getByText('Something broke.')).toBeInTheDocument()
  })

  it('is not editable when editable=false', async () => {
    const { container } = render(<PostmortemEditor content="plain text" editable={false} />)
    await waitFor(() => expect(container.querySelector('.ProseMirror')).toBeInTheDocument())
    expect(container.querySelector('.ProseMirror')).toHaveAttribute('contenteditable', 'false')
  })

  it('is editable when editable=true', async () => {
    const { container } = render(<PostmortemEditor content="plain text" editable onChange={vi.fn()} />)
    await waitFor(() => expect(container.querySelector('.ProseMirror')).toBeInTheDocument())
    expect(container.querySelector('.ProseMirror')).toHaveAttribute('contenteditable', 'true')
  })

  it('renders a Markdown table (the auto-seeded Lifecycle section) as an actual <table>, not run-together text', async () => {
    const table = [
      '| Stage | Timestamp | Time since previous |',
      '| --- | --- | --- |',
      '| Started | Aug 21, 2026, 5:53 PM | — |',
      '| Detected | Aug 23, 2026, 5:53 PM | 2d 0h |',
    ].join('\n')
    const { container } = render(<PostmortemEditor content={table} editable={false} />)

    await waitFor(() => expect(container.querySelector('table')).toBeInTheDocument())
    const headerCells = container.querySelectorAll('th')
    expect(Array.from(headerCells).map((c) => c.textContent)).toEqual(['Stage', 'Timestamp', 'Time since previous'])
    // No explicit <thead>/<tbody> markup, so the browser wraps every <tr>
    // (header row included) in one implicit <tbody> — hence 3 rows total for
    // 1 header + 2 data rows, not 2.
    const rows = container.querySelectorAll('table tr')
    expect(rows).toHaveLength(3)
    expect(rows[2].querySelectorAll('td')).toHaveLength(3)
    expect(rows[2].textContent).toContain('Detected')
    expect(rows[2].textContent).toContain('2d 0h')
  })
})
