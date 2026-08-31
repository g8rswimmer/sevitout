import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, X } from 'lucide-react'
import { api } from '@/lib/api'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { DirectoryUser } from '@/types/api'

/** Which stored integration identity a candidate must have set to be
 * offered by the picker, and the field's tracker-facing label. */
const FIELD_LABELS = {
  github_username: 'GitHub username',
  jira_account_id: 'Jira account ID',
} as const

type IdentityField = keyof typeof FIELD_LABELS

/** A single searchable-dropdown control, styled and labeled like any other
 * field in this form (a visible "Assignee" label above one bordered input,
 * not a plain `<Input>` that swaps for an unrelated chip once something's
 * picked) — used by TasksPanel's Create-GitHub/Jira-issue forms so a
 * Responder picks a person by name instead of typing a raw GitHub login or
 * opaque Jira account ID (Roadmap Phase 10f UX follow-up). Offers only
 * directory users who have `field` set on their own profile — picking
 * anyone else would produce an issue assigned to nobody (GitHub) or a
 * rejected request (Jira).
 *
 * Controlled by the parent: `value` is the tracker-native identifier
 * currently chosen (or "" for none); `selectedName`, when known, is shown
 * in the same input in `value`'s place. While a value is set the input is
 * read-only (a dropdown-style "one thing is chosen" state, not an editable
 * text field) — the trailing clear button returns it to a live search. This
 * component holds no assignee state of its own beyond the in-progress
 * search text. */
export function AssigneePicker({
  field,
  value,
  selectedName,
  onSelect,
  onClear,
}: {
  field: IdentityField
  value: string
  selectedName?: string
  onSelect: (user: DirectoryUser) => void
  onClear: () => void
}) {
  const [query, setQuery] = useState('')
  const inputId = `assignee-picker-${field}`

  // Only searches once at least 2 characters are typed, so every keystroke
  // on a short/empty query doesn't fire a request — same threshold as
  // RolesPanel's user picker. Suppressed entirely once a value is chosen,
  // since the input goes read-only and query is stale leftover text.
  const trimmed = query.trim()
  const searching = trimmed.length >= 2 && !value
  const directory = useQuery({
    queryKey: ['directory', 'assignee', field, trimmed],
    queryFn: () => api.auth.directory({ query: trimmed }),
    enabled: searching,
  })

  const matches = searching ? (directory.data?.users ?? []).filter((u) => !!u[field]) : []

  return (
    <div className="flex flex-col gap-1">
      <Label htmlFor={inputId} className="text-xs font-normal text-muted-foreground">
        Assignee
      </Label>
      <div className="relative w-64">
        <Input
          id={inputId}
          aria-label="Assignee"
          placeholder="Search by name or email…"
          value={value ? selectedName || value : query}
          readOnly={!!value}
          onChange={(e) => setQuery(e.target.value)}
          className={value ? 'cursor-default bg-muted/40 pr-8' : 'pr-8'}
        />
        {value ? (
          <button
            type="button"
            aria-label="Clear assignee"
            onClick={onClear}
            className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        ) : (
          <ChevronDown
            className="pointer-events-none absolute right-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground"
            aria-hidden
          />
        )}

        {matches.length > 0 && (
          <ul className="absolute z-10 mt-1 w-full rounded-md border border-border bg-popover shadow-md">
            {matches.slice(0, 8).map((u) => (
              <li key={u.id}>
                <button
                  type="button"
                  onClick={() => {
                    onSelect(u)
                    setQuery('')
                  }}
                  className="flex w-full flex-col items-start px-2 py-1.5 text-left text-xs hover:bg-accent"
                >
                  <span className="font-medium">{u.name}</span>
                  <span className="text-muted-foreground">{u.email}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
        {searching && !directory.isLoading && matches.length === 0 && (
          <p className="mt-1 text-xs text-muted-foreground">No matching user has a {FIELD_LABELS[field]} on file.</p>
        )}
      </div>
    </div>
  )
}
