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
import { Section } from '@/components/sev/Section'
import { recordToTagRows, tagRowsToRecord, TagRowsEditor, type TagRow } from '@/components/sev/TagRowsEditor'
import { formatDateTime } from '@/lib/format'
import type { IntegrationHealthStatus } from '@/types/api'

/** The integration types with a live connectivity check registered
 * server-side (cmd/server/main.go's healthCheckers map) — each also has one
 * well-known credential key its HealthChecker reads (plus, for Slack, a
 * second well-known credential key — see extraCredentialKeys below), and
 * (Jira and Slack) one or more well-known non-secret settings keys used
 * alongside the credential:
 *  - Jira: cloud_id (required — the datastore path won't activate without
 *    it, see jiraIssueResolver.apply) and site_url (optional — cosmetic
 *    browse-link generation only, mirroring JIRA_SITE_URL's independently-
 *    optional treatment in internal/config.Config).
 *  - Slack: default_channel and channel_naming_convention (both optional —
 *    read by cmd/slackbot's loadSlackSettings/runSettingsRefresher, each
 *    with a safe built-in fallback when unset; not read by
 *    slackHealthChecker, which only needs the bot_token credential).
 * "Other" covers any integration_type not in this fixed list (e.g. a future
 * Datadog/Prometheus integration), stored and displayed exactly as typed.
 *
 * `note`, where present, calls out a real behavioral gap worth surfacing in
 * the form itself rather than leaving an admin to discover it by testing —
 * currently only Slack has one. Per docs/roadmap.md Phase 8, this
 * bot_token/app_token pair is preferred over SLACK_BOT_TOKEN/SLACK_APP_TOKEN
 * (which remain a fallback) for *both* halves of the running bot at
 * startup: Socket Mode (slash commands, @mentions) and the REST client
 * (channel creation, messages, invites, history, user lookup). Only the
 * REST client picks up a change made here *without* a restart — it
 * periodically re-pulls this pair via ConfigService.GetSlackBotCredential.
 * Socket Mode's connection is still established once at startup; live-
 * reconnecting it on a credential change is an explicit, not-yet-built
 * follow-up (see the roadmap phase's "genuinely hard part"), so a rotated
 * credential still needs a restart to reach slash commands/@mentions. */
const KNOWN_INTEGRATIONS: {
  value: string
  label: string
  credentialKey: string
  /** Additional well-known credential keys beyond credentialKey, pre-filled
   * as their own rows the same way — currently only Slack's app_token. */
  extraCredentialKeys?: string[]
  settingsKeys?: { key: string; required: boolean }[]
  note?: string
}[] = [
  { value: 'pagerduty', label: 'PagerDuty', credentialKey: 'api_key' },
  { value: 'github', label: 'GitHub', credentialKey: 'token' },
  {
    value: 'slack',
    label: 'Slack',
    credentialKey: 'bot_token',
    extraCredentialKeys: ['app_token'],
    settingsKeys: [
      { key: 'default_channel', required: false },
      { key: 'channel_naming_convention', required: false },
    ],
    note: 'The Slack bot prefers this bot_token/app_token pair over SLACK_BOT_TOKEN/SLACK_APP_TOKEN (which remain a fallback) for both Socket Mode (slash commands, @mentions) and its REST client (channel creation, messages, invites, history, user lookup) at startup. Only the REST client picks up a change made here without a restart — it periodically re-pulls this pair. Socket Mode still needs a restart to pick up a rotated credential. default_channel and channel_naming_convention below reach the running bot the same periodic way.',
  },
  {
    value: 'jira',
    label: 'Jira',
    credentialKey: 'api_token',
    settingsKeys: [
      { key: 'cloud_id', required: true },
      { key: 'site_url', required: false },
    ],
  },
]

/** Builds the credential rows to show for known's type: the well-known
 * credentialKey plus any extraCredentialKeys, each as its own empty,
 * editable row — mirrors settingsRowsFor's "every well-known key is always
 * visible" treatment below, just for credentials instead of settings. */
function credentialRowsFor(known: (typeof KNOWN_INTEGRATIONS)[number] | undefined): TagRow[] {
  if (!known) return [{ key: '', value: '' }]
  return [known.credentialKey, ...(known.extraCredentialKeys ?? [])].map((key) => ({ key, value: '' }))
}
const OTHER = '__other__'

const HEALTH_BADGE: Record<IntegrationHealthStatus, { label: string; variant: BadgeProps['variant'] }> = {
  connected: { label: 'Connected', variant: 'secondary' },
  error: { label: 'Error', variant: 'destructive' },
  not_configured: { label: 'Not configured', variant: 'outline' },
  unknown: { label: 'No health check', variant: 'outline' },
}

function knownLabel(type: string): string {
  return KNOWN_INTEGRATIONS.find((k) => k.value === type)?.label ?? type
}

/** Builds the settings rows to show for known's type: any already-stored
 * values first (via existingSettings, preserving whatever isn't in
 * known.settingsKeys too — e.g. a value set before that key existed), then
 * an empty row appended for every well-known key (required or optional)
 * that isn't already present, so every well-known key is always a visible,
 * editable field — not just mentioned in the hint text below. */
function settingsRowsFor(known: (typeof KNOWN_INTEGRATIONS)[number] | undefined, existingSettings?: Record<string, string>): TagRow[] {
  const rows = recordToTagRows(existingSettings)
  for (const s of known?.settingsKeys ?? []) {
    if (!rows.some((r) => r.key === s.key)) rows.push({ key: s.key, value: '' })
  }
  return rows
}

export function AdminIntegrationsPage() {
  const queryClient = useQueryClient()
  const configs = useQuery({ queryKey: ['admin', 'integrations'], queryFn: api.config.integrations.list })
  const health = useQuery({ queryKey: ['admin', 'integrations', 'health'], queryFn: api.config.integrations.health })

  const [typeSelect, setTypeSelect] = useState<string>(KNOWN_INTEGRATIONS[0].value)
  const [customType, setCustomType] = useState('')
  const [credentials, setCredentials] = useState<TagRow[]>(credentialRowsFor(KNOWN_INTEGRATIONS[0]))
  const [settings, setSettings] = useState<TagRow[]>([])
  const [formError, setFormError] = useState<string | null>(null)

  const integrationType = typeSelect === OTHER ? customType.trim() : typeSelect
  const knownType = KNOWN_INTEGRATIONS.find((k) => k.value === typeSelect)

  function selectType(v: string) {
    setTypeSelect(v)
    const known = KNOWN_INTEGRATIONS.find((k) => k.value === v)
    setCredentials(credentialRowsFor(known))
    setSettings(settingsRowsFor(known))
  }

  const upsertMutation = useMutation({
    mutationFn: () =>
      api.config.integrations.upsert(integrationType, {
        integration_type: integrationType,
        credentials: tagRowsToRecord(credentials),
        settings: tagRowsToRecord(settings),
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['admin', 'integrations'] })
      setFormError(null)
    },
    onError: (err) => setFormError(err instanceof ApiError ? err.message : 'Failed to save integration'),
  })

  function loadForEdit(type: string, existingSettings?: Record<string, string>) {
    setTypeSelect(KNOWN_INTEGRATIONS.some((k) => k.value === type) ? type : OTHER)
    setCustomType(KNOWN_INTEGRATIONS.some((k) => k.value === type) ? '' : type)
    const known = KNOWN_INTEGRATIONS.find((k) => k.value === type)
    setCredentials(credentialRowsFor(known))
    setSettings(settingsRowsFor(known, existingSettings))
    setFormError(null)
  }

  const configured = configs.data?.configs ?? []
  const healthByType = new Map((health.data?.integrations ?? []).map((h) => [h.integration_type, h]))

  return (
    <div className="flex flex-col gap-4">
      <Section
        title="Configured integrations"
        action={
          <Button size="sm" variant="outline" onClick={() => health.refetch()} disabled={health.isFetching}>
            <RefreshCw className={`h-3.5 w-3.5 ${health.isFetching ? 'animate-spin' : ''}`} /> Refresh health
          </Button>
        }
      >
        {configs.isLoading ? (
          <Skeleton className="h-24 w-full" />
        ) : configured.length === 0 ? (
          <p className="text-sm text-muted-foreground">No integrations configured yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="py-2 pr-3">Type</th>
                  <th className="py-2 pr-3">Credentials</th>
                  <th className="py-2 pr-3">Health</th>
                  <th className="py-2 pr-3">Updated</th>
                  <th className="py-2" />
                </tr>
              </thead>
              <tbody>
                {configured.map((cfg) => {
                  const h = healthByType.get(cfg.integration_type)
                  const badge = h ? HEALTH_BADGE[h.status] : undefined
                  return (
                    <tr key={cfg.integration_type} className="border-b border-border">
                      <td className="py-2 pr-3 font-medium">{knownLabel(cfg.integration_type)}</td>
                      <td className="py-2 pr-3">
                        <Badge variant={cfg.credentials_configured ? 'secondary' : 'outline'}>
                          {cfg.credentials_configured ? 'Configured' : 'Not set'}
                        </Badge>
                      </td>
                      <td className="py-2 pr-3">
                        {badge ? (
                          <Badge variant={badge.variant} title={h?.error}>
                            {badge.label}
                          </Badge>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="py-2 pr-3 text-muted-foreground">{formatDateTime(cfg.updated_at)}</td>
                      <td className="py-2 text-right">
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => loadForEdit(cfg.integration_type, cfg.settings)}
                        >
                          Edit
                        </Button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Section>

      <Section title="Add / update integration">
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="integration-type">Type</Label>
              <Select id="integration-type" value={typeSelect} onChange={(e) => selectType(e.target.value)}>
                {KNOWN_INTEGRATIONS.map((k) => (
                  <option key={k.value} value={k.value}>
                    {k.label}
                  </option>
                ))}
                <option value={OTHER}>Other…</option>
              </Select>
            </div>
            {typeSelect === OTHER && (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="integration-custom-type">Custom type name</Label>
                <Input
                  id="integration-custom-type"
                  placeholder="datadog"
                  value={customType}
                  onChange={(e) => setCustomType(e.target.value)}
                />
              </div>
            )}
          </div>

          {knownType?.note && (
            <div className="flex items-start gap-2 rounded-md bg-muted/50 p-3 text-sm text-muted-foreground">
              <Info className="mt-0.5 h-4 w-4 shrink-0" />
              <p>{knownType.note}</p>
            </div>
          )}

          <div>
            <Label>Credentials (write-only — leave a value blank to keep it unchanged)</Label>
            <p className="mb-1.5 text-xs text-muted-foreground">
              {knownType
                ? `Well-known key${knownType.extraCredentialKeys?.length ? 's' : ''} for this type: ${[knownType.credentialKey, ...(knownType.extraCredentialKeys ?? [])]
                    .map((k) => `"${k}"`)
                    .join(', ')}`
                : 'Enter whatever key(s) this integration expects.'}
            </p>
            <TagRowsEditor rows={credentials} onChange={setCredentials} />
          </div>

          <div>
            <Label>Settings (non-secret)</Label>
            {knownType?.settingsKeys && (
              <p className="mb-1.5 text-xs text-muted-foreground">
                {`Well-known key${knownType.settingsKeys.length > 1 ? 's' : ''} for this type: ${knownType.settingsKeys
                  .map((s) => `"${s.key}"${s.required ? '' : ' (optional)'}`)
                  .join(', ')}`}
              </p>
            )}
            <TagRowsEditor rows={settings} onChange={setSettings} />
          </div>

          {formError && (
            <p role="alert" className="text-sm text-destructive">
              {formError}
            </p>
          )}
          <div>
            <Button
              size="sm"
              onClick={() => upsertMutation.mutate()}
              disabled={upsertMutation.isPending || !integrationType}
            >
              {upsertMutation.isPending ? 'Saving…' : 'Save integration'}
            </Button>
          </div>
        </div>
      </Section>
    </div>
  )
}
