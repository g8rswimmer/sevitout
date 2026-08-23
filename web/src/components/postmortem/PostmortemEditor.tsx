import { useEffect } from 'react'
import type { Editor } from '@tiptap/react'
import { useEditor, EditorContent } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { Markdown, type MarkdownStorage } from 'tiptap-markdown'
import { cn } from '@/lib/utils'

// tiptap-markdown doesn't ship a @tiptap/core Storage module augmentation,
// so `editor.storage.markdown` isn't visible on the generic Editor type —
// this is the one place that cast lives.
function getMarkdown(editor: Editor): string {
  return (editor.storage as unknown as { markdown: MarkdownStorage }).markdown.getMarkdown()
}

/** A TipTap rich-text editor that reads/writes plain Markdown (the storage
 * format `postmortems.content` uses — docs/architecture.md §12 resolved
 * this specifically so the document stays portable/human-readable outside
 * the UI). `content`/`editable` are treated as external props to sync into
 * the (otherwise TipTap-owned) editor instance, not literal React-controlled
 * values re-rendered on every keystroke — TipTap manages its own DOM. */
export function PostmortemEditor({
  content,
  editable,
  onChange,
  className,
}: {
  content: string
  editable: boolean
  onChange?: (markdown: string) => void
  className?: string
}) {
  const editor = useEditor({
    extensions: [StarterKit, Markdown.configure({ html: false, transformPastedText: true })],
    content,
    editable,
    onUpdate: ({ editor }) => {
      onChange?.(getMarkdown(editor))
    },
  })

  // Push editable changes (e.g. Edit/Unlock → Save/Cancel) into the live
  // editor instance — useEditor only applies its `editable` option once, at
  // creation.
  useEffect(() => {
    editor?.setEditable(editable)
  }, [editor, editable])

  // Push external content changes (loading a different SEV, Cancel
  // reverting to the last-saved value, Apply on an AI draft) in — but only
  // when they didn't originate from this editor's own onUpdate, or every
  // keystroke would round-trip through the parent and reset the cursor.
  useEffect(() => {
    if (!editor) return
    if (content !== getMarkdown(editor)) {
      editor.commands.setContent(content)
    }
  }, [editor, content])

  return (
    <EditorContent
      editor={editor}
      className={cn(
        'tiptap-content rounded-md',
        editable && 'border border-input bg-transparent px-3 py-2 shadow-sm focus-within:ring-1 focus-within:ring-ring',
        className,
      )}
    />
  )
}
