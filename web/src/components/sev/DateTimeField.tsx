import { useRef } from 'react'
import { CalendarClock } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

/** A datetime-local field with an explicit "open picker" button — clicking
 * the input itself already opens most browsers' native date+time picker,
 * but that's not an obvious affordance on its own, so this adds a calendar
 * icon button that calls the input's own `showPicker()` (a real DOM API,
 * not a custom widget) to make it unambiguous. Falls back silently if the
 * browser doesn't support showPicker (Firefox, at time of writing) — the
 * input itself, and its own native affordance, still work either way. */
export function DateTimeField({
  id,
  label,
  value,
  onChange,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
}) {
  const inputRef = useRef<HTMLInputElement>(null)

  function openPicker() {
    try {
      inputRef.current?.showPicker?.()
    } catch {
      // Unsupported browser, or called on an element in a state that
      // disallows it — the input is still directly clickable/typeable.
    }
  }

  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex gap-1.5">
        <Input
          id={id}
          ref={inputRef}
          type="datetime-local"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="flex-1"
        />
        <Button
          type="button"
          variant="outline"
          size="icon"
          onClick={openPicker}
          aria-label={`Open date/time picker for ${label}`}
          title="Open date/time picker"
        >
          <CalendarClock className="h-4 w-4" />
        </Button>
      </div>
    </div>
  )
}
