import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { X } from 'lucide-react'
import { api } from '@/lib/api'
import { Input } from '@/components/ui/input'
import type { DirectoryUser } from '@/types/api'

/** Which stored integration identity a candidate must have set to be
 * offered by the picker, and the field's tracker-facing label. */
const FIELD_LABELS = {
  github_username: 'GitHub username',
  jira_account_id: 'Jira account ID',
} as const

type IdentityField = keyof typeof FIELD_LABELS

/** A searchable dropdown of directory users who have `field` set on their
 * own profile — used by TasksPanel's Create-GitHub/Jira-issue forms so a
 * Responder picks a person by name instead of typing a raw GitHub login or
 * opaque Jira account ID (Roadmap Phase 10f UX follow-up). Once selected,
 * the picked user's name is shown in place of the tracker-native value the
 * form actually submits.
 *
 * Controlled by the parent: `value` is the tracker-native identifier
 * currently chosen (or "" for none); `selectedName`, when known, is shown
 * instead of `value` itself. onSelect/onClear report the user's choice —
 * this component holds no assignee state of its own beyond the in-progress
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

  // Only searches once at least 2 characters are typed, so every keystroke
  // on a short/empty query doesn't fire a request — same threshold as
  // RolesPanel's user picker.
  const trimmed = query.trim()
  const directory = useQuery({
    queryKey: ['directory', 'assignee', field, trimmed],
    queryFn: () => api.auth.directory({ query: trimmed }),
    enabled: trimmed.length >= 2,
  })

  // Only candidates with the relevant identity actually configured are
  // offered — picking anyone else would produce an issue assigned to
  // nobody (GitHub) or a rejected request (Jira), so they're filtered out
  // rather than shown disabled.
  const matches = trimmed.length >= 2 ? (directory.data?.users ?? []).filter((u) => !!u[field]) : []

  if (value) {
    return (
      <div className="flex w-fit items-center gap-1.5 rounded-md border border-border bg-muted/50 px-2 py-1 text-xs">
        Assignee: <span className="font-medium">{selectedName || value}</span>
        <button
          type="button"
          aria-label="Clear assignee"
          onClick={onClear}
          className="text-muted-foreground hover:text-foreground"
        >
          <X className="h-3 w-3" />
        </button>
      </div>
    )
  }

  return (
    <div className="relative w-64">
      <Input
        aria-label="Assignee"
        placeholder="Search assignee by name or email (optional)…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
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
      {trimmed.length >= 2 && !directory.isLoading && matches.length === 0 && (
        <p className="mt-1 text-xs text-muted-foreground">No matching user has a {FIELD_LABELS[field]} on file.</p>
      )}
    </div>
  )
}
