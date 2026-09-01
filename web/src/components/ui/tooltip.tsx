import { Info } from 'lucide-react'
import { cn } from '@/lib/utils'

/** A minimal CSS-only tooltip (no Radix — see components/ui/select.tsx and
 * checkbox.tsx for the same "plain element over a new dependency" choice for
 * a single use case). No `title` attribute — that triggers the browser's own
 * native tooltip *in addition to* the styled one below (a second, unstyled
 * "box" appearing after the OS hover delay, on top of ours), and screen
 * readers already get the text via the `sr-only` span, so it added nothing.
 * The styled tooltip shows on hover and on keyboard focus
 * (`group-focus-visible`), so it's reachable without a mouse too; cursor is
 * left at its default rather than the `help` (question-mark) cursor — the
 * info icon itself is the affordance.
 *
 * `side` picks which way the popup opens, default `'top'`. Callers inside a
 * horizontally-scrollable `overflow-x-auto` table wrapper (ServiceSLAEditor.tsx/
 * AdminServicesPage.tsx's SLA-target column headers) must pass `side="bottom"`:
 * verified directly in Chrome, an absolutely positioned descendant of a
 * `<table>` inside such a wrapper gets clipped at the wrapper's top edge even
 * with `overflow-y: visible` explicitly set on the wrapper (a real rendering
 * quirk, not spec behavior a plain non-table element exhibits) — opening
 * downward never needs to escape the wrapper's box, so it isn't affected.
 * `'top'` stays the default since it's the right choice with no such wrapper
 * (e.g. LifecyclePanel.tsx's metric labels, which have their value directly
 * below — opening downward there would cover it while visible). */
export function InfoTooltip({
  text,
  className,
  side = 'top',
}: {
  text: string
  className?: string
  side?: 'top' | 'bottom'
}) {
  return (
    <span tabIndex={0} className={cn('group relative inline-flex align-middle outline-none', className)}>
      <Info className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
      <span className="sr-only">{text}</span>
      <span
        role="tooltip"
        className={cn(
          'pointer-events-none absolute left-1/2 z-20 w-max max-w-56 -translate-x-1/2 rounded-md bg-foreground px-2 py-1 text-xs whitespace-normal text-background opacity-0 shadow-md transition-opacity duration-100 group-hover:opacity-100 group-focus-visible:opacity-100',
          side === 'top' ? 'bottom-full mb-1.5' : 'top-full mt-1.5',
        )}
      >
        {text}
      </span>
    </span>
  )
}
