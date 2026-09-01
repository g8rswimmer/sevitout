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
 * info icon itself is the affordance. */
export function InfoTooltip({ text, className }: { text: string; className?: string }) {
  return (
    <span tabIndex={0} className={cn('group relative inline-flex align-middle outline-none', className)}>
      <Info className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
      <span className="sr-only">{text}</span>
      <span
        role="tooltip"
        className="pointer-events-none absolute bottom-full left-1/2 z-20 mb-1.5 w-max max-w-56 -translate-x-1/2 rounded-md bg-foreground px-2 py-1 text-xs whitespace-normal text-background opacity-0 shadow-md transition-opacity duration-100 group-hover:opacity-100 group-focus-visible:opacity-100"
      >
        {text}
      </span>
    </span>
  )
}
