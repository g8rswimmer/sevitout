import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Sparkles } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { formatDateTime } from '@/lib/format'

/** Shows the most recent AI-generated postmortem draft (§11.1's "Resolved →
 * auto-draft postmortem skeleton" proactive trigger, or a manually requested
 * one below) and lets a Responder+ generate a fresh one or apply it into the
 * editor. AI output is never applied automatically — the user must review
 * and explicitly Apply it (§11.3: "non-authoritative"), which only loads it
 * into the editor for further editing, it doesn't save by itself. */
export function AIDraftPanel({
  sevId,
  canTrigger,
  onApply,
}: {
  sevId: string
  canTrigger: boolean
  onApply: (markdown: string) => void
}) {
  const queryClient = useQueryClient()
  const [error, setError] = useState<string | null>(null)

  const outputs = useQuery({ queryKey: ['ai', 'outputs', sevId], queryFn: () => api.ai.listOutputs(sevId) })
  const plugins = useQuery({ queryKey: ['ai', 'plugins'], queryFn: api.ai.listPlugins })

  const generate = useMutation({
    mutationFn: () => api.ai.triggerAction(sevId, { action: 'AI_ACTION_DRAFT_POSTMORTEM' }),
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: ['ai', 'outputs', sevId] })
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to generate a draft'),
  })

  // ListOutputs returns oldest-first, so the last DraftPostmortem entry is
  // the most recent one — proactive (on Resolve) or manual alike.
  const drafts = (outputs.data?.outputs ?? []).filter((o) => o.action === 'AI_ACTION_DRAFT_POSTMORTEM')
  const latest = drafts.length > 0 ? drafts[drafts.length - 1] : undefined
  const hasPlugins = (plugins.data?.plugins ?? []).length > 0

  // Nothing worth a Viewer's attention if no draft exists yet and they can't
  // generate one anyway.
  if (!canTrigger && !latest && !outputs.isLoading) return null

  return (
    <Section
      title="AI Draft Suggestion"
      action={
        canTrigger && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => generate.mutate()}
            disabled={generate.isPending || !hasPlugins}
            title={!hasPlugins ? 'No AI plugin is configured' : undefined}
          >
            {generate.isPending ? 'Generating…' : latest ? 'Regenerate' : 'Generate Draft'}
          </Button>
        )
      }
    >
      {outputs.isLoading && <Skeleton className="h-16 w-full" />}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {!latest && !outputs.isLoading && (
        <p className="text-sm text-muted-foreground">
          No AI draft yet.{canTrigger && !hasPlugins ? ' No AI plugin is configured.' : ''}
        </p>
      )}
      {latest && (
        <div className="flex flex-col gap-2 rounded-md border border-dashed border-border p-3">
          <div className="flex items-center justify-between gap-2">
            <span className="flex items-center gap-1.5 text-xs font-medium text-amber-600 dark:text-amber-500">
              <Sparkles className="h-3.5 w-3.5" aria-hidden />
              AI-generated — not authoritative, review before use
            </span>
            <span className="text-xs text-muted-foreground">{formatDateTime(latest.created_at)}</span>
          </div>
          <p className="max-h-48 overflow-y-auto whitespace-pre-wrap text-sm text-muted-foreground">
            {latest.content}
          </p>
          {canTrigger && (
            <Button size="sm" className="self-start" onClick={() => onApply(latest.content ?? '')}>
              Apply to editor
            </Button>
          )}
        </div>
      )}
    </Section>
  )
}
