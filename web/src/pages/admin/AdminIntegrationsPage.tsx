import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Info, RefreshCw } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { Badge, type BadgeProps } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { formatDateTime } from '@/lib/format'
import type { CatalogField, IntegrationCatalogEntry, IntegrationConfigResponse, IntegrationHealth, IntegrationHealthStatus } from '@/types/api'

const HEALTH_BADGE: Record<IntegrationHealthStatus, { label: string; variant: BadgeProps['variant'] }> = {
  connected: { label: 'Connected', variant: 'success' },
  error: { label: 'Error', variant: 'destructive' },
  not_configured: { label: 'Not configured', variant: 'outline' },
  unknown: { label: 'No health check', variant: 'outline' },
}

/** A short, generic starting point for troubleshooting a failed health
 * check — the raw error message (below) already carries the specifics
 * (PagerDuty/GitHub/Jira all return the real API error text; Slack returns
 * its own error code, e.g. "invalid_auth"/"token_revoked"), but a bare
 * "status 401" isn't self-explanatory to everyone. Keyed by integration_type. */
const TROUBLESHOOTING_HINTS: Record<string, string> = {
  pagerduty: 'Usually means the API Key is missing, revoked, or was typed incorrectly — generate a new one in PagerDuty under your user profile\'s API access.',
  github: "Usually means the token is expired, revoked, or lacks read access — check it's still valid and has at least the repo scope.",
  slack: 'Usually means the Bot Token was revoked or reinstalled — reinstall the Slack app and paste in the new bot_token (starts with xoxb-).',
  jira: "Usually means the API Token is invalid, or the Cloud ID doesn't match this token's site — verify both (Cloud ID is under admin.atlassian.com, not the *.atlassian.net site name).",
}

/** A real behavioral gap worth surfacing in the form itself rather than
 * leaving an admin to discover it by testing — currently only Slack has
 * one. Per docs/roadmap.md Phase 8, this bot_token/app_token pair is
 * preferred over SLACK_BOT_TOKEN/SLACK_APP_TOKEN (which remain a fallback)
 * for both Socket Mode and the REST client at startup; only the REST
 * client picks up a *later* change without a restart. Keyed by
 * integration_type, matching catalog.All's Type field. */
const INTEGRATION_NOTES: Record<string, string> = {
  slack: 'The Slack bot prefers this bot_token/app_token pair over SLACK_BOT_TOKEN/SLACK_APP_TOKEN — set both here and no env vars are needed. Changes reach message/channel actions live; Socket Mode still needs a restart to pick up a rotated credential. Settings below update live too.',
}

/** A field's label, with a "(required)"/"(optional)" hint appended for a
 * settings field (credential fields skip this — see FieldLabel's call
 * sites) — Required is a UI-only affordance (see UpsertIntegrationConfig's
 * doc comment), but still worth surfacing to whoever's filling the form in. */
function FieldLabel({ field, htmlFor, showRequirement }: { field: CatalogField; htmlFor: string; showRequirement?: boolean }) {
  return (
    <Label htmlFor={htmlFor}>
      {field.label}
      {showRequirement && (
        <span className="ml-1 font-normal text-muted-foreground">{field.required ? '(required)' : '(optional)'}</span>
      )}
    </Label>
  )
}

/** The schema-driven detail form for one selected integration. Mounted with
 * `key={entry.type}` by the parent so switching sidebar rows remounts it
 * fresh — the idiomatic React way to reset local form state on a prop
 * change, rather than an effect that copies props into state. */
function IntegrationDetailForm({
  entry,
  existingConfig,
  health,
  onSaved,
}: {
  entry: IntegrationCatalogEntry
  existingConfig?: IntegrationConfigResponse
  health?: IntegrationHealth
  onSaved: () => void
}) {
  const [credentialValues, setCredentialValues] = useState<Record<string, string>>(() =>
    Object.fromEntries((entry.credential_fields ?? []).map((f) => [f.key, ''])),
  )
  // Each settings field starts at its already-stored value when one exists,
  // else "" for a text field or the first option for a select field — so a
  // select is always a real, catalog-valid value from the moment it's
  // rendered, never a blank the backend's select-value validation would reject.
  const [settingsValues, setSettingsValues] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      (entry.settings_fields ?? []).map((f) => [f.key, existingConfig?.settings?.[f.key] ?? (f.kind === 'select' ? (f.options?.[0] ?? '') : '')]),
    ),
  )
  const [formError, setFormError] = useState<string | null>(null)

  const upsertMutation = useMutation({
    mutationFn: () => {
      // A blank credential value means "leave the stored one unchanged" —
      // omit it entirely rather than sending an empty string.
      const credentials = Object.fromEntries(Object.entries(credentialValues).filter(([, v]) => v !== ''))
      return api.config.integrations.upsert(entry.type, {
        integration_type: entry.type,
        credentials: Object.keys(credentials).length > 0 ? credentials : undefined,
        settings: Object.keys(settingsValues).length > 0 ? settingsValues : undefined,
      })
    },
    onSuccess: () => {
      setFormError(null)
      onSaved()
    },
    onError: (err) => setFormError(err instanceof ApiError ? err.message : 'Failed to save integration'),
  })

  return (
    <div className="flex min-w-0 flex-1 flex-col gap-4 rounded-lg border border-border bg-card p-4">
      <div>
        <h2 className="text-sm font-medium text-muted-foreground">{entry.label}</h2>
        {existingConfig && <p className="text-xs text-muted-foreground">Last updated {formatDateTime(existingConfig.updated_at)}</p>}
      </div>

      {health?.status === 'error' && (
        <div className="flex flex-col gap-1 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm">
          <p className="font-medium text-destructive">Health check failed</p>
          {health.error && <p className="text-destructive">{health.error}</p>}
          {TROUBLESHOOTING_HINTS[entry.type] && <p className="text-muted-foreground">{TROUBLESHOOTING_HINTS[entry.type]}</p>}
        </div>
      )}

      {INTEGRATION_NOTES[entry.type] && (
        <div className="flex items-start gap-2 rounded-md bg-muted/50 p-3 text-sm text-muted-foreground">
          <Info className="mt-0.5 h-4 w-4 shrink-0" />
          <p>{INTEGRATION_NOTES[entry.type]}</p>
        </div>
      )}

      <div className="flex flex-col gap-3">
        <Label>Credentials</Label>
        {(entry.credential_fields ?? []).length === 0 ? (
          <p className="text-sm text-muted-foreground">No credentials required.</p>
        ) : (
          <>
            <p className="-mt-2 text-xs text-muted-foreground">Write-only — leave a value blank to keep it unchanged.</p>
            <div className="flex max-w-sm flex-col gap-3">
              {entry.credential_fields?.map((field) => {
                const id = `credential-${field.key}`
                return (
                  <div key={field.key} className="flex flex-col gap-1.5">
                    <FieldLabel field={field} htmlFor={id} />
                    <Input
                      id={id}
                      type="password"
                      placeholder={existingConfig?.credentials_configured ? 'Leave blank to keep current value' : 'Enter value'}
                      value={credentialValues[field.key] ?? ''}
                      onChange={(e) => setCredentialValues((prev) => ({ ...prev, [field.key]: e.target.value }))}
                    />
                  </div>
                )
              })}
            </div>
          </>
        )}
      </div>

      {(entry.settings_fields ?? []).length > 0 && (
        <div className="flex flex-col gap-3">
          <Label>Settings</Label>
          <div className="flex max-w-sm flex-col gap-3">
            {entry.settings_fields?.map((field) => {
              const id = `setting-${field.key}`
              return (
                <div key={field.key} className="flex flex-col gap-1.5">
                  <FieldLabel field={field} htmlFor={id} showRequirement />
                  {field.kind === 'select' ? (
                    <Select
                      id={id}
                      value={settingsValues[field.key] ?? ''}
                      onChange={(e) => setSettingsValues((prev) => ({ ...prev, [field.key]: e.target.value }))}
                    >
                      {field.options?.map((opt) => (
                        <option key={opt} value={opt}>
                          {opt.charAt(0).toUpperCase() + opt.slice(1)}
                        </option>
                      ))}
                    </Select>
                  ) : (
                    <Input
                      id={id}
                      type="text"
                      value={settingsValues[field.key] ?? ''}
                      onChange={(e) => setSettingsValues((prev) => ({ ...prev, [field.key]: e.target.value }))}
                    />
                  )}
                  {field.help && <p className="text-xs text-muted-foreground">{field.help}</p>}
                </div>
              )
            })}
          </div>
        </div>
      )}

      {formError && (
        <p role="alert" className="text-sm text-destructive">
          {formError}
        </p>
      )}

      <div className="flex justify-end">
        <Button size="sm" onClick={() => upsertMutation.mutate()} disabled={upsertMutation.isPending}>
          {upsertMutation.isPending ? 'Saving…' : 'Save integration'}
        </Button>
      </div>
    </div>
  )
}

export function AdminIntegrationsPage() {
  const queryClient = useQueryClient()
  const catalog = useQuery({ queryKey: ['admin', 'integrations', 'catalog'], queryFn: api.config.integrations.catalog })
  const configs = useQuery({ queryKey: ['admin', 'integrations'], queryFn: api.config.integrations.list })
  const health = useQuery({ queryKey: ['admin', 'integrations', 'health'], queryFn: api.config.integrations.health })

  const [selectedType, setSelectedType] = useState<string | null>(null)

  const entries = catalog.data?.integrations ?? []
  const activeType = selectedType ?? entries[0]?.type ?? null
  const activeEntry = entries.find((e) => e.type === activeType)
  const configsByType = new Map((configs.data?.configs ?? []).map((c) => [c.integration_type, c]))
  const existingConfig = activeType ? configsByType.get(activeType) : undefined
  const healthByType = new Map((health.data?.integrations ?? []).map((h) => [h.integration_type, h]))

  const isLoading = catalog.isLoading || configs.isLoading

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-end">
        <Button size="sm" variant="outline" onClick={() => health.refetch()} disabled={health.isFetching}>
          <RefreshCw className={`h-3.5 w-3.5 ${health.isFetching ? 'animate-spin' : ''}`} /> Refresh health
        </Button>
      </div>

      {isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : (
        <div className="flex items-start gap-4">
          <div className="flex w-64 shrink-0 flex-col gap-0.5 rounded-lg border border-border bg-card p-2">
            {entries.map((entry) => {
              const h = healthByType.get(entry.type)
              const healthBadge = h ? HEALTH_BADGE[h.status] : HEALTH_BADGE.unknown
              const isConfigured = configsByType.has(entry.type)
              const isActive = entry.type === activeType
              return (
                <button
                  key={entry.type}
                  type="button"
                  onClick={() => setSelectedType(entry.type)}
                  aria-current={isActive ? 'true' : undefined}
                  className={`flex flex-col gap-1 rounded-md border-l-[3px] px-3 py-2 text-left transition-colors ${
                    isActive ? 'border-l-primary bg-accent' : 'border-l-transparent hover:bg-accent/50'
                  }`}
                >
                  <span className="text-sm font-semibold">{entry.label}</span>
                  <span className="flex flex-wrap gap-1.5">
                    <Badge variant={isConfigured ? 'secondary' : 'outline'}>{isConfigured ? 'Configured' : 'Not set'}</Badge>
                    <Badge variant={healthBadge.variant} title={h?.error}>
                      {healthBadge.label}
                    </Badge>
                  </span>
                </button>
              )
            })}
          </div>

          {activeEntry && (
            <IntegrationDetailForm
              key={activeEntry.type}
              entry={activeEntry}
              existingConfig={existingConfig}
              health={healthByType.get(activeEntry.type)}
              onSaved={() => {
                void queryClient.invalidateQueries({ queryKey: ['admin', 'integrations'] })
                // Re-run health checks immediately so a newly-saved (or
                // rotated) credential's status updates without waiting for
                // the next manual "Refresh health" click.
                void health.refetch()
              }}
            />
          )}
        </div>
      )}
    </div>
  )
}
