import { useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Lock } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { useAuth } from '@/auth/useAuth'
import { useSevSocket } from '@/lib/ws'
import { buildPostmortemTemplate } from '@/lib/postmortemTemplate'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import { PostmortemEditor } from '@/components/postmortem/PostmortemEditor'
import { PostmortemStatusControl } from '@/components/postmortem/PostmortemStatusControl'
import { UnlockDialog } from '@/components/postmortem/UnlockDialog'
import { AIDraftPanel } from '@/components/postmortem/AIDraftPanel'
import { hasRole, POSTMORTEM_STATUS_BADGE_CLASS, POSTMORTEM_STATUS_LABELS } from '@/types/api'

export function PostmortemPage() {
  const { id } = useParams<{ id: string }>()
  const sevId = id!
  const { user } = useAuth()
  const queryClient = useQueryClient()

  useSevSocket(sevId)

  const sev = useQuery({ queryKey: ['sevs', 'detail', sevId], queryFn: () => api.sevs.get(sevId) })
  const pm = useQuery({ queryKey: ['postmortems', sevId], queryFn: () => api.postmortems.get(sevId) })

  const canEdit = hasRole(user?.org_role, 'responder')
  const canTransition = hasRole(user?.org_role, 'incident-commander')
  const canUnlock = hasRole(user?.org_role, 'incident-commander')

  const [editing, setEditing] = useState(false)
  const [draftContent, setDraftContent] = useState('')
  const [unlockToken, setUnlockToken] = useState<string | null>(null)
  const [pendingContent, setPendingContent] = useState<string | null>(null)
  const [unlockDialogOpen, setUnlockDialogOpen] = useState(false)
  const [unlockError, setUnlockError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)

  const unlockMutation = useMutation({
    mutationFn: (reason: string) => api.postmortems.unlockSev(sevId, { reason }),
    onSuccess: (res) => {
      setUnlockToken(res.unlock_token)
      setUnlockDialogOpen(false)
      setUnlockError(null)
      enterEditMode(pendingContent ?? currentOrTemplateContent())
      setPendingContent(null)
    },
    onError: (err) => setUnlockError(err instanceof ApiError ? err.message : 'Failed to unlock'),
  })

  const saveMutation = useMutation({
    mutationFn: () => api.postmortems.update(sevId, { content: draftContent, unlock_token: unlockToken ?? undefined }),
    onSuccess: () => {
      setEditing(false)
      // "Auto-lock on save": the unlock token authorized exactly this one
      // write (docs/architecture.md §4.3) — sev.locked itself never actually
      // flips back to false from an unlock (only a status transition off
      // Postmortem Complete does that), so discarding the token here is
      // what makes the *next* edit need a fresh reason.
      setUnlockToken(null)
      setSaveError(null)
      void queryClient.invalidateQueries({ queryKey: ['postmortems', sevId] })
    },
    onError: (err) => setSaveError(err instanceof ApiError ? err.message : 'Failed to save'),
  })

  function enterEditMode(initialContent: string) {
    setDraftContent(initialContent)
    setSaveError(null)
    setEditing(true)
  }

  // The first time anyone opens this postmortem for editing and it's still
  // blank, seed it with a template built from the SEV's own recorded facts
  // (summary, lifecycle timestamps/deltas, root cause, business impact,
  // services, mitigation) instead of a blank page — see
  // lib/postmortemTemplate.ts. Once real content exists, it's used as-is;
  // this never overwrites anything a human has written.
  function currentOrTemplateContent(): string {
    const content = pm.data?.content
    if (content && content.trim() !== '') return content
    return sev.data ? buildPostmortemTemplate(sev.data) : ''
  }

  function handleEditClick() {
    if (sev.data?.locked) {
      setPendingContent(null)
      setUnlockError(null)
      setUnlockDialogOpen(true)
    } else {
      enterEditMode(currentOrTemplateContent())
    }
  }

  function handleApplyDraft(markdown: string) {
    if (editing) {
      setDraftContent(markdown)
      return
    }
    if (sev.data?.locked) {
      setPendingContent(markdown)
      setUnlockError(null)
      setUnlockDialogOpen(true)
    } else {
      enterEditMode(markdown)
    }
  }

  function handleCancel() {
    setEditing(false)
    setUnlockToken(null)
    setSaveError(null)
  }

  if (sev.isLoading || pm.isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-9 w-96" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }

  if (sev.isError || pm.isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        Failed to load postmortem: {((sev.error ?? pm.error) as Error).message}
      </p>
    )
  }

  const record = sev.data!
  const postmortem = pm.data!
  const showEditButton = !record.locked && canEdit && !editing
  const showUnlockButton = record.locked && canUnlock && !editing

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <Link to={`/sevs/${sevId}`} className="flex w-fit items-center gap-1 text-sm text-muted-foreground hover:underline">
          <ArrowLeft className="h-3.5 w-3.5" aria-hidden />
          Back to {sevId}
        </Link>
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-2xl font-semibold">Postmortem: {record.title}</h1>
          <Badge className={POSTMORTEM_STATUS_BADGE_CLASS[postmortem.status]}>
            {POSTMORTEM_STATUS_LABELS[postmortem.status]}
          </Badge>
          {record.locked && (
            <Badge variant="outline" className="gap-1">
              <Lock className="h-3 w-3" /> SEV Locked
            </Badge>
          )}
        </div>
        <PostmortemStatusControl sevId={sevId} postmortem={postmortem} canTransition={canTransition} />
      </div>

      <AIDraftPanel sevId={sevId} canTrigger={canEdit} onApply={handleApplyDraft} />

      <Section
        title="Document"
        action={
          <div className="flex gap-2">
            {showEditButton && (
              <Button size="sm" variant="ghost" onClick={handleEditClick}>
                Edit
              </Button>
            )}
            {showUnlockButton && (
              <Button size="sm" variant="outline" onClick={handleEditClick}>
                <Lock className="h-3.5 w-3.5" /> Unlock to edit
              </Button>
            )}
            {editing && (
              <>
                <Button size="sm" onClick={() => saveMutation.mutate()} disabled={saveMutation.isPending}>
                  {saveMutation.isPending ? 'Saving…' : 'Save'}
                </Button>
                <Button size="sm" variant="outline" onClick={handleCancel} disabled={saveMutation.isPending}>
                  Cancel
                </Button>
              </>
            )}
          </div>
        }
      >
        {saveError && (
          <p role="alert" className="text-sm text-destructive">
            {saveError}
          </p>
        )}
        <PostmortemEditor
          content={editing ? draftContent : (postmortem.content ?? '')}
          editable={editing}
          onChange={editing ? setDraftContent : undefined}
        />
      </Section>

      <UnlockDialog
        open={unlockDialogOpen}
        onClose={() => {
          setUnlockDialogOpen(false)
          setPendingContent(null)
        }}
        onConfirm={(reason) => unlockMutation.mutate(reason)}
        submitting={unlockMutation.isPending}
        error={unlockError}
      />
    </div>
  )
}
