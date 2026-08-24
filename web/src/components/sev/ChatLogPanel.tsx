import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { formatDateTime } from '@/lib/format'

export function ChatLogPanel({ sevId, canAdd }: { sevId: string; canAdd: boolean }) {
  const queryClient = useQueryClient()
  const entries = useQuery({ queryKey: ['sevs', sevId, 'chat'], queryFn: () => api.chat.list(sevId) })

  const [source, setSource] = useState('slack')
  const [author, setAuthor] = useState('')
  const [content, setContent] = useState('')
  const [error, setError] = useState<string | null>(null)

  const add = useMutation({
    mutationFn: () =>
      api.chat.add(sevId, { occurred_at: new Date().toISOString(), source, author, content }),
    onSuccess: () => {
      setContent('')
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['sevs', sevId, 'chat'] })
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to add chat entry'),
  })

  const list = [...(entries.data?.entries ?? [])].sort(
    (a, b) => new Date(a.occurred_at).getTime() - new Date(b.occurred_at).getTime(),
  )

  return (
    <Section title="Chat log">
      {entries.isLoading && <Skeleton className="h-8 w-full" />}
      {entries.isError && (
        <p role="alert" className="text-sm text-destructive">
          Failed to load chat log: {(entries.error as Error).message}
        </p>
      )}
      {entries.data && list.length === 0 && <p className="text-sm text-muted-foreground">No chat entries captured yet.</p>}
      {list.length > 0 && (
        <ul className="flex max-h-80 flex-col gap-2 overflow-y-auto">
          {list.map((entry) => (
            <li key={entry.id} className="text-sm">
              <span className="text-xs text-muted-foreground">
                [{entry.source}] {formatDateTime(entry.occurred_at)}
              </span>{' '}
              <span className="font-medium">{entry.author}:</span> {entry.content}
            </li>
          ))}
        </ul>
      )}

      {canAdd && (
        <form
          className="flex flex-col gap-2 border-t border-border pt-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (author.trim() && content.trim()) add.mutate()
          }}
        >
          <div className="flex gap-2">
            <Input
              aria-label="Source"
              placeholder="Source (slack, email…)"
              value={source}
              onChange={(e) => setSource(e.target.value)}
              className="w-40"
            />
            <Input
              aria-label="Author"
              placeholder="Author"
              value={author}
              onChange={(e) => setAuthor(e.target.value)}
              className="w-40"
            />
          </div>
          <Textarea
            aria-label="Message content"
            placeholder="Paste the message content…"
            value={content}
            onChange={(e) => setContent(e.target.value)}
          />
          <Button type="submit" size="sm" className="self-start" disabled={add.isPending || !author.trim() || !content.trim()}>
            {add.isPending ? 'Adding…' : 'Add entry'}
          </Button>
        </form>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </Section>
  )
}
