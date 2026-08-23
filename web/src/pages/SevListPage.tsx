import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { ArrowDownAZ, ArrowUpAZ, Plus, Search } from 'lucide-react'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import { SeverityBadge, StatusBadge } from '@/components/sev/badges'
import { formatDateTime, formatDurationSeconds } from '@/lib/format'
import {
  QUICK_VIEW_LABELS,
  SEV_STATUS_LABELS,
  type QuickView,
  type SEVSortField,
  type SEVStatus,
} from '@/types/api'

const SEVERITY_LEVELS = [1, 2, 3, 4]
const ALL_STATUSES: SEVStatus[] = [
  'open',
  'investigating',
  'mitigated',
  'resolved',
  'postmortem_in_progress',
  'postmortem_complete',
]
const SORT_FIELDS: { value: SEVSortField; label: string }[] = [
  { value: 'started_at', label: 'Started' },
  { value: 'severity', label: 'Severity' },
  { value: 'mttr', label: 'MTTR' },
  { value: 'updated_at', label: 'Last updated' },
]
const PAGE_SIZE = 20

export function SevListPage() {
  const [queryInput, setQueryInput] = useState('')
  const [query, setQuery] = useState('')
  const [quickView, setQuickView] = useState<QuickView | ''>('')
  const [severityLevels, setSeverityLevels] = useState<number[]>([])
  const [statuses, setStatuses] = useState<SEVStatus[]>([])
  const [sort, setSort] = useState<SEVSortField>('started_at')
  const [sortDesc, setSortDesc] = useState(true)
  const [offset, setOffset] = useState(0)

  const results = useQuery({
    queryKey: ['search', 'sevs', { query, quickView, severityLevels, statuses, sort, sortDesc, offset }],
    queryFn: () =>
      api.search.sevs({
        query: query || undefined,
        quick_view: quickView || undefined,
        severity_levels: severityLevels.length ? severityLevels : undefined,
        statuses: quickView ? undefined : statuses.length ? statuses : undefined,
        sort,
        sort_desc: sortDesc,
        limit: PAGE_SIZE,
        offset,
      }),
  })

  function toggleSeverity(level: number) {
    setOffset(0)
    setSeverityLevels((prev) => (prev.includes(level) ? prev.filter((l) => l !== level) : [...prev, level]))
  }

  function toggleStatus(s: SEVStatus) {
    setOffset(0)
    setStatuses((prev) => (prev.includes(s) ? prev.filter((v) => v !== s) : [...prev, s]))
  }

  function selectQuickView(qv: QuickView | '') {
    setOffset(0)
    setQuickView(qv)
  }

  const sevs = results.data?.sevs ?? []
  const total = results.data?.total ?? 0

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">SEVs</h1>
        <Link to="/sevs/new" className="inline-flex">
          <Button>
            <Plus className="h-4 w-4" aria-hidden />
            New SEV
          </Button>
        </Link>
      </div>

      <div className="flex flex-col gap-6 lg:flex-row">
        <aside className="flex shrink-0 flex-col gap-5 lg:w-56">
          <div>
            <h2 className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Quick views</h2>
            <div className="flex flex-col gap-1" role="tablist" aria-label="Quick views">
              <QuickViewTab label="All" active={quickView === ''} onClick={() => selectQuickView('')} />
              {(Object.keys(QUICK_VIEW_LABELS) as QuickView[]).map((qv) => (
                <QuickViewTab
                  key={qv}
                  label={QUICK_VIEW_LABELS[qv]}
                  active={quickView === qv}
                  onClick={() => selectQuickView(qv)}
                />
              ))}
            </div>
          </div>

          <div>
            <h2 className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Severity</h2>
            <div className="flex flex-col gap-1.5">
              {SEVERITY_LEVELS.map((level) => (
                <label key={level} className="flex items-center gap-2 text-sm">
                  <Checkbox checked={severityLevels.includes(level)} onChange={() => toggleSeverity(level)} />
                  SEV-{level}
                </label>
              ))}
            </div>
          </div>

          <div>
            <h2 className="mb-2 text-xs font-semibold uppercase text-muted-foreground">Status</h2>
            <div className={`flex flex-col gap-1.5 ${quickView ? 'opacity-50' : ''}`}>
              {ALL_STATUSES.map((s) => (
                <label key={s} className="flex items-center gap-2 text-sm">
                  <Checkbox checked={statuses.includes(s)} onChange={() => toggleStatus(s)} disabled={!!quickView} />
                  {SEV_STATUS_LABELS[s]}
                </label>
              ))}
            </div>
            {quickView && (
              <p className="mt-1 text-xs text-muted-foreground">Cleared while a quick view is active.</p>
            )}
          </div>
        </aside>

        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              setOffset(0)
              setQuery(queryInput.trim())
            }}
          >
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
              <Input
                type="search"
                placeholder="Search title, description, root cause, announcements…"
                className="pl-8"
                value={queryInput}
                onChange={(e) => setQueryInput(e.target.value)}
                aria-label="Search SEVs"
              />
            </div>
            <Select
              value={sort}
              onChange={(e) => {
                setOffset(0)
                setSort(e.target.value as SEVSortField)
              }}
              className="w-40"
              aria-label="Sort by"
            >
              {SORT_FIELDS.map((f) => (
                <option key={f.value} value={f.value}>
                  Sort: {f.label}
                </option>
              ))}
            </Select>
            <Button
              type="button"
              variant="outline"
              size="icon"
              onClick={() => {
                setOffset(0)
                setSortDesc((d) => !d)
              }}
              aria-label={sortDesc ? 'Descending' : 'Ascending'}
              title={sortDesc ? 'Descending' : 'Ascending'}
            >
              {sortDesc ? <ArrowDownAZ className="h-4 w-4" /> : <ArrowUpAZ className="h-4 w-4" />}
            </Button>
            <Button type="submit">Search</Button>
          </form>

          {results.isLoading && (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
            </div>
          )}

          {results.isError && (
            <p role="alert" className="text-sm text-destructive">
              Failed to load SEVs: {(results.error as Error).message}
            </p>
          )}

          {results.data && sevs.length === 0 && (
            <Card>
              <CardContent className="py-10 text-center text-sm text-muted-foreground">
                No SEVs match the current filters.
              </CardContent>
            </Card>
          )}

          {sevs.length > 0 && (
            <Card>
              <ul className="divide-y divide-border">
                {sevs.map((sev) => (
                  <li key={sev.id} className="flex flex-col gap-2 p-4 sm:flex-row sm:items-center sm:justify-between">
                    <Link to={`/sevs/${sev.id}`} className="flex min-w-0 items-center gap-3">
                      <SeverityBadge level={sev.severity_level} />
                      <StatusBadge status={sev.status} />
                      <span className="truncate font-medium hover:underline">{sev.title}</span>
                    </Link>
                    <div className="flex shrink-0 items-center gap-4 text-xs text-muted-foreground">
                      <span>{(sev.affected_services ?? []).join(', ') || '—'}</span>
                      <span>Started {formatDateTime(sev.started_at)}</span>
                      <span>MTTR {formatDurationSeconds(sev.mttr_seconds)}</span>
                    </div>
                  </li>
                ))}
              </ul>
            </Card>
          )}

          {total > 0 && (
            <div className="flex items-center justify-between text-sm text-muted-foreground">
              <span>
                {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} of {total}
              </span>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset === 0}
                  onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
                >
                  Previous
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset + PAGE_SIZE >= total}
                  onClick={() => setOffset((o) => o + PAGE_SIZE)}
                >
                  Next
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function QuickViewTab({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={`rounded-md px-3 py-1.5 text-left text-sm font-medium transition-colors ${
        active ? 'bg-accent text-accent-foreground' : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
      }`}
    >
      {label}
    </button>
  )
}
