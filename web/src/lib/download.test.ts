import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { downloadTextFile } from '@/lib/download'

describe('downloadTextFile', () => {
  let createObjectURL: ReturnType<typeof vi.fn>
  let revokeObjectURL: ReturnType<typeof vi.fn>
  let click: ReturnType<typeof vi.fn<() => void>>
  const originalClick = HTMLAnchorElement.prototype.click

  beforeEach(() => {
    // jsdom doesn't implement URL.createObjectURL/revokeObjectURL at all —
    // calling the real ones throws "not implemented" — and doesn't actually
    // navigate on an anchor click, but we still want to assert it was
    // invoked, so all three are mocked directly. (vi.spyOn on
    // HTMLAnchorElement.prototype.click hits a TS overload-resolution quirk
    // with jsdom's typings, so this assigns the mock directly instead.)
    createObjectURL = vi.fn(() => 'blob:mock-url')
    revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { ...URL, createObjectURL, revokeObjectURL })
    click = vi.fn<() => void>()
    HTMLAnchorElement.prototype.click = click
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    HTMLAnchorElement.prototype.click = originalClick
  })

  it('creates a Blob with the given content/type, downloads it via a temporary anchor, and revokes the URL', () => {
    let anchor: HTMLAnchorElement | undefined
    const createElement = document.createElement.bind(document)
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      const el = createElement(tag)
      if (tag === 'a') anchor = el as HTMLAnchorElement
      return el
    })

    downloadTextFile('SEV-2026-0001-postmortem.md', '# Summary\n\nSomething broke.', 'text/markdown')

    expect(createObjectURL).toHaveBeenCalledTimes(1)
    const blob = createObjectURL.mock.calls[0][0] as Blob
    expect(blob.type).toBe('text/markdown')

    expect(anchor?.download).toBe('SEV-2026-0001-postmortem.md')
    expect(anchor?.href).toBe('blob:mock-url')
    expect(click).toHaveBeenCalledTimes(1)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock-url')
  })

  it('defaults to text/plain when no mimeType is given', () => {
    downloadTextFile('notes.txt', 'hello')
    const blob = createObjectURL.mock.calls[0][0] as Blob
    expect(blob.type).toBe('text/plain')
  })
})
