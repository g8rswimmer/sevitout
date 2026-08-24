import { useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Section } from '@/components/sev/Section'
import type { AIHandlerType, AIPluginResponse } from '@/types/api'

interface PluginForm {
  name: string
  version: string
  description: string
  handlerType: AIHandlerType
  httpEndpoint: string
  provider: string
  model: string
  apiKey: string
  enabled: boolean
  triggerOnOpen: boolean
  triggerOnMitigated: boolean
  triggerOnResolved: boolean
  triggerOnPostmortemReview: boolean
  rateLimitPerMinute: string
}

function toForm(p?: AIPluginResponse): PluginForm {
  return {
    name: p?.name ?? '',
    version: p?.version ?? '',
    description: p?.description ?? '',
    handlerType: p?.handler_type ?? 'builtin',
    httpEndpoint: p?.http_endpoint ?? '',
    provider: p?.provider ?? '',
    model: p?.model ?? '',
    apiKey: '',
    enabled: p?.enabled ?? true,
    triggerOnOpen: p?.trigger_on_open ?? false,
    triggerOnMitigated: p?.trigger_on_mitigated ?? false,
    triggerOnResolved: p?.trigger_on_resolved ?? false,
    triggerOnPostmortemReview: p?.trigger_on_postmortem_review ?? false,
    rateLimitPerMinute: p?.rate_limit_per_minute != null ? String(p.rate_limit_per_minute) : '0',
  }
}

// Shared by both Create and Update: every boolean/rate-limit field is always
// included (never conditionally omitted), because UpdateAIPluginRequest's
// wrapper-typed fields treat an explicit false/0 as meaningful — see
// types/api.ts's comment on UpdateAIPluginRequest.
function formToRequest(f: PluginForm) {
  return {
    name: f.name,
    version: f.version || undefined,
    description: f.description || undefined,
    handler_type: f.handlerType,
    http_endpoint: f.httpEndpoint || undefined,
    provider: f.provider || undefined,
    model: f.model || undefined,
    api_key: f.apiKey || undefined,
    enabled: f.enabled,
    trigger_on_open: f.triggerOnOpen,
    trigger_on_mitigated: f.triggerOnMitigated,
    trigger_on_resolved: f.triggerOnResolved,
    trigger_on_postmortem_review: f.triggerOnPostmortemReview,
    rate_limit_per_minute: Number(f.rateLimitPerMinute) || 0,
  }
}

export function AdminAIPluginsPage() {
  const queryClient = useQueryClient()
  const plugins = useQuery({ queryKey: ['admin', 'ai-plugins'], queryFn: api.config.aiPlugins.list })

  const [creating, setCreating] = useState(false)
  const [createForm, setCreateForm] = useState<PluginForm>(() => toForm())
  const [createError, setCreateError] = useState<string | null>(null)

  const [editingId, setEditingId] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<PluginForm>(() => toForm())
  const [editError, setEditError] = useState<string | null>(null)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'ai-plugins'] })

  const createMutation = useMutation({
    mutationFn: () => api.config.aiPlugins.create(formToRequest(createForm)),
    onSuccess: () => {
      invalidate()
      setCreating(false)
      setCreateForm(toForm())
      setCreateError(null)
    },
    onError: (err) => setCreateError(err instanceof ApiError ? err.message : 'Failed to create plugin'),
  })

  const updateMutation = useMutation({
    mutationFn: (id: string) => api.config.aiPlugins.update(id, formToRequest(editForm)),
    onSuccess: () => {
      invalidate()
      setEditingId(null)
      setEditError(null)
    },
    onError: (err) => setEditError(err instanceof ApiError ? err.message : 'Failed to save plugin'),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.config.aiPlugins.delete(id),
    onSuccess: invalidate,
  })

  function startEdit(p: AIPluginResponse) {
    setEditingId(p.id)
    setEditForm(toForm(p))
    setEditError(null)
  }

  function renderForm(form: PluginForm, setForm: (f: PluginForm) => void, idPrefix: string) {
    return (
      <div className="flex flex-col gap-3">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Name" htmlFor={`${idPrefix}-name`}>
            <Input id={`${idPrefix}-name`} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </Field>
          <Field label="Version" htmlFor={`${idPrefix}-version`}>
            <Input
              id={`${idPrefix}-version`}
              value={form.version}
              onChange={(e) => setForm({ ...form, version: e.target.value })}
            />
          </Field>
        </div>
        <Field label="Description" htmlFor={`${idPrefix}-description`}>
          <Input
            id={`${idPrefix}-description`}
            value={form.description}
            onChange={(e) => setForm({ ...form, description: e.target.value })}
          />
        </Field>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Field label="Handler type" htmlFor={`${idPrefix}-handler`}>
            <Select
              id={`${idPrefix}-handler`}
              value={form.handlerType}
              onChange={(e) => setForm({ ...form, handlerType: e.target.value as AIHandlerType })}
            >
              <option value="builtin">Built-in</option>
              <option value="http">HTTP endpoint</option>
            </Select>
          </Field>
          {form.handlerType === 'http' && (
            <Field label="HTTP endpoint" htmlFor={`${idPrefix}-endpoint`}>
              <Input
                id={`${idPrefix}-endpoint`}
                placeholder="https://example.com/plugin"
                value={form.httpEndpoint}
                onChange={(e) => setForm({ ...form, httpEndpoint: e.target.value })}
              />
            </Field>
          )}
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Field label="Provider" htmlFor={`${idPrefix}-provider`}>
            <Input
              id={`${idPrefix}-provider`}
              placeholder="anthropic"
              value={form.provider}
              onChange={(e) => setForm({ ...form, provider: e.target.value })}
            />
          </Field>
          <Field label="Model" htmlFor={`${idPrefix}-model`}>
            <Input
              id={`${idPrefix}-model`}
              placeholder="claude-sonnet-5"
              value={form.model}
              onChange={(e) => setForm({ ...form, model: e.target.value })}
            />
          </Field>
          <Field label="Rate limit / min (0 = unlimited)" htmlFor={`${idPrefix}-rate-limit`}>
            <Input
              id={`${idPrefix}-rate-limit`}
              type="number"
              min={0}
              value={form.rateLimitPerMinute}
              onChange={(e) => setForm({ ...form, rateLimitPerMinute: e.target.value })}
            />
          </Field>
        </div>
        <Field label="API key" htmlFor={`${idPrefix}-api-key`}>
          <Input
            id={`${idPrefix}-api-key`}
            type="password"
            placeholder="Leave blank to keep the existing key unchanged"
            value={form.apiKey}
            onChange={(e) => setForm({ ...form, apiKey: e.target.value })}
          />
        </Field>
        <label className="flex items-center gap-2 text-sm">
          <Checkbox checked={form.enabled} onChange={(e) => setForm({ ...form, enabled: e.target.checked })} />
          Enabled
        </label>
        <div>
          <Label>Proactive triggers</Label>
          <div className="mt-1.5 grid grid-cols-2 gap-2 sm:grid-cols-4">
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={form.triggerOnOpen}
                onChange={(e) => setForm({ ...form, triggerOnOpen: e.target.checked })}
              />
              On open
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={form.triggerOnMitigated}
                onChange={(e) => setForm({ ...form, triggerOnMitigated: e.target.checked })}
              />
              On mitigated
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={form.triggerOnResolved}
                onChange={(e) => setForm({ ...form, triggerOnResolved: e.target.checked })}
              />
              On resolved
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={form.triggerOnPostmortemReview}
                onChange={(e) => setForm({ ...form, triggerOnPostmortemReview: e.target.checked })}
              />
              On postmortem review
            </label>
          </div>
        </div>
      </div>
    )
  }

  const list = plugins.data?.plugins ?? []

  return (
    <Section
      title="AI plugins"
      action={
        !creating && (
          <Button size="sm" onClick={() => setCreating(true)}>
            <Plus className="h-3.5 w-3.5" /> New plugin
          </Button>
        )
      }
    >
      {creating && (
        <div className="flex flex-col gap-3 rounded-md border border-border p-3">
          {renderForm(createForm, setCreateForm, 'plugin-new')}
          {createError && (
            <p role="alert" className="text-sm text-destructive">
              {createError}
            </p>
          )}
          <div className="flex gap-2">
            <Button size="sm" onClick={() => createMutation.mutate()} disabled={createMutation.isPending || !createForm.name}>
              {createMutation.isPending ? 'Creating…' : 'Create'}
            </Button>
            <Button size="sm" variant="outline" onClick={() => setCreating(false)}>
              Cancel
            </Button>
          </div>
        </div>
      )}

      {plugins.isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : list.length === 0 ? (
        <p className="text-sm text-muted-foreground">No AI plugins registered yet.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3">Name</th>
                <th className="py-2 pr-3">Handler</th>
                <th className="py-2 pr-3">Provider / model</th>
                <th className="py-2 pr-3">API key</th>
                <th className="py-2 pr-3">Status</th>
                <th className="py-2" />
              </tr>
            </thead>
            <tbody>
              {list.map((p) =>
                editingId === p.id ? (
                  <tr key={p.id} className="border-b border-border align-top">
                    <td colSpan={6} className="py-3">
                      <div className="flex flex-col gap-3">
                        {renderForm(editForm, setEditForm, `plugin-${p.id}`)}
                        {editError && (
                          <p role="alert" className="text-sm text-destructive">
                            {editError}
                          </p>
                        )}
                        <div className="flex gap-2">
                          <Button size="sm" onClick={() => updateMutation.mutate(p.id)} disabled={updateMutation.isPending}>
                            {updateMutation.isPending ? 'Saving…' : 'Save'}
                          </Button>
                          <Button size="sm" variant="outline" onClick={() => setEditingId(null)}>
                            Cancel
                          </Button>
                        </div>
                      </div>
                    </td>
                  </tr>
                ) : (
                  <tr key={p.id} className="border-b border-border">
                    <td className="py-2 pr-3 font-medium">{p.name}</td>
                    <td className="py-2 pr-3 text-muted-foreground">{p.handler_type}</td>
                    <td className="py-2 pr-3 text-muted-foreground">
                      {[p.provider, p.model].filter(Boolean).join(' / ') || '—'}
                    </td>
                    <td className="py-2 pr-3">
                      <Badge variant={p.api_key_configured ? 'secondary' : 'outline'}>
                        {p.api_key_configured ? 'Configured' : 'Not set'}
                      </Badge>
                    </td>
                    <td className="py-2 pr-3">
                      <Badge variant={p.enabled ? 'secondary' : 'outline'}>{p.enabled ? 'Enabled' : 'Disabled'}</Badge>
                    </td>
                    <td className="py-2 text-right">
                      <div className="flex justify-end gap-1">
                        <Button variant="ghost" size="icon" aria-label={`Edit ${p.name}`} onClick={() => startEdit(p)}>
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`Delete ${p.name}`}
                          onClick={() => deleteMutation.mutate(p.id)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  )
}

function Field({ label, htmlFor, children }: { label: string; htmlFor?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
    </div>
  )
}
