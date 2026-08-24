import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Share2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatDateTime } from '@/lib/format'
import type { ShareLinkResponse } from '@/types/api'

/** A "Share" button + dialog for ShareService.CreateShareLink/RevokeShareLink
 * (§14.1) on the SEV detail page. There's no ListShareLinks RPC — only
 * Create/Revoke — so the most recently created link only exists in this
 * component's own state for as long as the page stays open; reloading the
 * page loses track of it (the link itself keeps working until it expires or
 * is explicitly revoked, this is just the UI forgetting about it). State is
 * kept here rather than inside the Dialog's children because Dialog
 * unmounts its children whenever it's closed (see dialog.tsx) — closing and
 * reopening the dialog must not lose a link that was already created. */
export function ShareLinkControl({ sevId, canShare }: { sevId: string; canShare: boolean }) {
  const [open, setOpen] = useState(false)
  const [expiresInDays, setExpiresInDays] = useState('30')
  const [link, setLink] = useState<ShareLinkResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const createMutation = useMutation({
    mutationFn: () => api.shares.create(sevId, { expires_in_days: Number(expiresInDays) || undefined }),
    onSuccess: (res) => {
      setLink(res)
      setError(null)
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to create share link'),
  })

  const revokeMutation = useMutation({
    mutationFn: () => api.shares.revoke(sevId, link!.token),
    onSuccess: () => {
      setLink(null)
      setCopied(false)
      setError(null)
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to revoke share link'),
  })

  if (!canShare) return null

  const publicUrl = link ? `${window.location.origin}${link.path}` : ''

  function handleClose() {
    setError(null)
    setOpen(false)
  }

  async function copyLink() {
    try {
      await navigator.clipboard.writeText(publicUrl)
      setCopied(true)
    } catch {
      // Clipboard API unavailable/denied — the URL is still selectable text
      // in the input field, so this isn't a dead end, just a smaller
      // convenience lost.
    }
  }

  return (
    <>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
        <Share2 className="h-3.5 w-3.5" /> Share
      </Button>
      <Dialog open={open} onClose={handleClose} title="Public share link">
        {link ? (
          <div className="flex flex-col gap-3">
            <p className="text-sm text-muted-foreground">
              Anyone with this link can view a read-only summary of this SEV — no login required.
            </p>
            <div className="flex gap-2">
              <Input readOnly value={publicUrl} aria-label="Public share URL" onFocus={(e) => e.target.select()} />
              <Button size="sm" variant="outline" onClick={copyLink}>
                {copied ? 'Copied' : 'Copy'}
              </Button>
            </div>
            {link.expires_at && (
              <p className="text-xs text-muted-foreground">Expires {formatDateTime(link.expires_at)}</p>
            )}
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="outline" onClick={handleClose}>
                Close
              </Button>
              <Button
                size="sm"
                variant="destructive"
                className="h-7 px-2 text-xs"
                onClick={() => revokeMutation.mutate()}
                disabled={revokeMutation.isPending}
              >
                {revokeMutation.isPending ? 'Revoking…' : 'Revoke link'}
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="share-expires">Expires in (days)</Label>
              <Input
                id="share-expires"
                type="number"
                min={1}
                value={expiresInDays}
                onChange={(e) => setExpiresInDays(e.target.value)}
              />
            </div>
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="outline" onClick={handleClose}>
                Cancel
              </Button>
              <Button size="sm" onClick={() => createMutation.mutate()} disabled={createMutation.isPending}>
                {createMutation.isPending ? 'Creating…' : 'Create link'}
              </Button>
            </div>
          </div>
        )}
      </Dialog>
    </>
  )
}
