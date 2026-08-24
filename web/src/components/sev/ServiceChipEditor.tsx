import { useState, type KeyboardEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { X } from 'lucide-react'
import { api } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'

/** Chip-list editor for a SEV's affected_services. Suggests quick-add
 * buttons from the service registry (ConfigService.ListServices, Viewer+)
 * but also accepts free text — docs/requirements.md §4.2 allows affected
 * services to reference the registry "or free-form". Full registry CRUD is
 * M14d; this only reads it. */
export function ServiceChipEditor({
  services,
  onChange,
}: {
  services: string[]
  onChange: (services: string[]) => void
}) {
  const [input, setInput] = useState('')
  const registry = useQuery({ queryKey: ['services'], queryFn: api.services.list })

  function add(name: string) {
    const trimmed = name.trim()
    if (trimmed && !services.includes(trimmed)) onChange([...services, trimmed])
    setInput('')
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      add(input)
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-1.5">
        {services.map((s) => (
          <Badge key={s} variant="secondary" className="gap-1">
            {s}
            <button
              type="button"
              onClick={() => onChange(services.filter((v) => v !== s))}
              aria-label={`Remove ${s}`}
              className="rounded-full hover:bg-black/10"
            >
              <X className="h-3 w-3" />
            </button>
          </Badge>
        ))}
      </div>
      <Input
        placeholder="Type a service name and press Enter"
        value={input}
        onChange={(e) => setInput(e.target.value)}
        onKeyDown={handleKeyDown}
        onBlur={() => add(input)}
      />
      {registry.data?.services && registry.data.services.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {registry.data.services
            .filter((svc) => !services.includes(svc.id))
            .map((svc) => (
              <button
                key={svc.id}
                type="button"
                onClick={() => add(svc.id)}
                className="rounded-md border border-dashed border-border px-2 py-0.5 text-xs text-muted-foreground hover:bg-accent"
              >
                + {svc.name}
              </button>
            ))}
        </div>
      )}
    </div>
  )
}
