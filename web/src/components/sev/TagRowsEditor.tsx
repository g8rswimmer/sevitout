import { X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export interface TagRow {
  key: string
  value: string
}

export function recordToTagRows(record?: Record<string, string>): TagRow[] {
  return Object.entries(record ?? {}).map(([key, value]) => ({ key, value }))
}

export function tagRowsToRecord(rows: TagRow[]): Record<string, string> | undefined {
  const record: Record<string, string> = {}
  for (const row of rows) {
    if (row.key.trim()) record[row.key.trim()] = row.value
  }
  return Object.keys(record).length ? record : undefined
}

/** A repeatable key/value row editor for a SEV's free-form tags — a compact
 * stand-in for a full tag-picker UI, given tags are arbitrary org-defined
 * key/values (docs/requirements.md §4.2), not a fixed vocabulary to
 * autocomplete against. */
export function TagRowsEditor({ rows, onChange }: { rows: TagRow[]; onChange: (rows: TagRow[]) => void }) {
  return (
    <div className="flex flex-col gap-2">
      {rows.map((tag, i) => (
        <div key={i} className="flex gap-2">
          <Input
            aria-label="Tag key"
            placeholder="key"
            value={tag.key}
            onChange={(e) => onChange(rows.map((t, idx) => (idx === i ? { ...t, key: e.target.value } : t)))}
            className="w-1/3"
          />
          <Input
            aria-label="Tag value"
            placeholder="value"
            value={tag.value}
            onChange={(e) => onChange(rows.map((t, idx) => (idx === i ? { ...t, value: e.target.value } : t)))}
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label="Remove tag"
            onClick={() => onChange(rows.filter((_, idx) => idx !== i))}
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
      ))}
      <Button type="button" variant="outline" size="sm" className="self-start" onClick={() => onChange([...rows, { key: '', value: '' }])}>
        Add tag
      </Button>
    </div>
  )
}
