import { useState } from 'react'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

/** The "written reason" prompt docs/requirements.md §10.1 requires before a
 * Postmortem-Complete SEV can be edited — the reason is written to the audit
 * log server-side (PostmortemService.UnlockSEV) along with the caller and
 * timestamp. */
export function UnlockDialog({
  open,
  onClose,
  onConfirm,
  submitting,
  error,
}: {
  open: boolean
  onClose: () => void
  onConfirm: (reason: string) => void
  submitting: boolean
  error: string | null
}) {
  const [reason, setReason] = useState('')

  function handleClose() {
    setReason('')
    onClose()
  }

  return (
    <Dialog open={open} onClose={handleClose} title="Unlock this SEV to edit">
      <p className="text-sm text-muted-foreground">
        This SEV is locked (postmortem complete). Provide a reason — it's recorded in the
        audit log — to unlock it for this edit.
      </p>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="unlock-reason">Reason</Label>
        <Textarea
          id="unlock-reason"
          autoFocus
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder="Why does this need to change after approval?"
        />
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={handleClose} disabled={submitting}>
          Cancel
        </Button>
        <Button
          type="button"
          onClick={() => onConfirm(reason)}
          disabled={submitting || !reason.trim()}
        >
          {submitting ? 'Unlocking…' : 'Unlock'}
        </Button>
      </div>
    </Dialog>
  )
}
