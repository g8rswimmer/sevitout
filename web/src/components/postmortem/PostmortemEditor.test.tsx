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
})
