import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Input } from '@/components/ui/input'
import { SeverityBadge } from '@/components/sev/badges'

/** Debounces a fast-changing value — used here so typing doesn't fire a
 * search request per keystroke. No library: this is the entire
 * implementation TanStack Query's own docs suggest for this case. */
function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs)
    return () => clearTimeout(timer)
  }, [value, delayMs])
  return debounced
}

/** A text input that suggests matching SEVs (by ID or full-text title/
 * description match, via SearchService.SearchSEVs) as the user types,
 * showing each option's title alongside its ID — not just a bare ID field
 * the user has to already know by heart. The input's value stays the
 * literal target_sev_id being submitted; picking a suggestion just fills
 * that in, so typing an exact known ID directly (no suggestion click) still
 * works exactly as before. */
export function SevAutocomplete({
  id,
  ariaLabel,
  value,
  onChange,
  excludeId,
}: {
  id?: string
  ariaLabel?: string
  value: string
  onChange: (id: string) => void
  /** Omitted from suggestions — typically the current SEV, which can't link to itself. */
  excludeId?: string
}) {
  const [open, setOpen] = useState(false)
  const debouncedValue = useDebouncedValue(value, 250)
  const blurTimer = useRef<ReturnType<typeof setTimeout>>(undefined)

  const results = useQuery({
    queryKey: ['search', 'sevs', 'autocomplete', debouncedValue],
    queryFn: () => api.search.sevs({ query: debouncedValue, limit: 6 }),
    enabled: debouncedValue.trim().length >= 2,
  })

  const options = (results.data?.sevs ?? []).filter((s) => s.id !== excludeId)

  return (
    <div className="relative">
      <Input
        id={id}
        aria-label={ariaLabel}
        placeholder="Search by ID or title…"
        value={value}
        onChange={(e) => {
          onChange(e.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => {
          // Delay so a click on a suggestion (which blurs the input first)
          // still registers before the list disappears.
          blurTimer.current = setTimeout(() => setOpen(false), 150)
        }}
        autoComplete="off"
      />
      {open && options.length > 0 && (
        <ul className="absolute z-20 mt-1 max-h-56 w-full overflow-y-auto rounded-md border border-border bg-card shadow-md">
          {options.map((s) => (
            <li key={s.id}>
              <button
                type="button"
                onMouseDown={(e) => {
                  // Prevent the input's onBlur from closing the list before
                  // this click's onClick has a chance to run.
                  e.preventDefault()
                  clearTimeout(blurTimer.current)
                }}
                onClick={() => {
                  onChange(s.id)
                  setOpen(false)
                }}
                className="flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm hover:bg-accent"
              >
                <SeverityBadge level={s.severity_level} />
                <span className="truncate font-mono text-xs text-muted-foreground">{s.id}</span>
                <span className="truncate">{s.title}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
