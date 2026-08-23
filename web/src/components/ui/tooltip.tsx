import { Info } from 'lucide-react'
import { cn } from '@/lib/utils'

/** A minimal CSS-only tooltip (no Radix — see components/ui/select.tsx and
 * checkbox.tsx for the same "plain element over a new dependency" choice for
 * a single use case). `title` gives every browser a free native fallback;
 * the visible styled tooltip shows on hover and on keyboard focus
 * (`group-focus-visible`), so it's reachable without a mouse too. */
export function InfoTooltip({ text, className }: { text: string; className?: string }) {
  return (
    <span
      tabIndex={0}
      title={text}
      className={cn('group relative inline-flex cursor-help align-middle outline-none', className)}
    >
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
