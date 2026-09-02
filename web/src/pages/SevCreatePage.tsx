import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { ServiceChipEditor } from '@/components/sev/ServiceChipEditor'
import { LevelingCriteriaPanel } from '@/components/sev/LevelingCriteriaPanel'
import { TagRowsEditor, tagRowsToRecord, type TagRow } from '@/components/sev/TagRowsEditor'
import { DetectionFields, type DetectionFieldsValue } from '@/components/sev/DetectionFields'
import { DateTimeField } from '@/components/sev/DateTimeField'

const EMPTY_DETECTION: DetectionFieldsValue = {
  detectionMethod: '',
  monitoringTool: '',
  alertName: '',
  alertUrl: '',
  dashboardUrl: '',
  query: '',
  snapshotUrl: '',
}

export function SevCreatePage() {
  const navigate = useNavigate()

  // Owned here (rather than by LevelingCriteriaPanel) per convention — see
  // that component's doc comment. Shares a cache entry with
  // ServiceChipEditor's own ['services'] query.
  const registry = useQuery({ queryKey: ['services'], queryFn: api.services.list })

  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [severityLevel, setSeverityLevel] = useState(3)
  const [affectedServices, setAffectedServices] = useState<string[]>([])
  const [startedAt, setStartedAt] = useState('')
  const [detectedAt, setDetectedAt] = useState('')
  const [detection, setDetection] = useState<DetectionFieldsValue>(EMPTY_DETECTION)
  const [tags, setTags] = useState<TagRow[]>([])
  const [sensitive, setSensitive] = useState(false)
  const [aiDisabled, setAiDisabled] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      const sev = await api.sevs.create({
        title,
        description: description || undefined,
        severity_level: severityLevel,
        affected_services: affectedServices.length ? affectedServices : undefined,
        detection_method: detection.detectionMethod || undefined,
        alert_name: detection.alertName || undefined,
        monitoring_tool: detection.monitoringTool || undefined,
        alert_url: detection.alertUrl || undefined,
        dashboard_url: detection.dashboardUrl || undefined,
        query: detection.query || undefined,
        snapshot_url: detection.snapshotUrl || undefined,
        started_at: startedAt ? new Date(startedAt).toISOString() : undefined,
        detected_at: detectedAt ? new Date(detectedAt).toISOString() : undefined,
        tags: tagRowsToRecord(tags),
        sensitive,
        ai_disabled: aiDisabled,
      })
      navigate(`/sevs/${sev.id}`, { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to create SEV')
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto flex max-w-2xl flex-col gap-6">
      <h1 className="text-2xl font-semibold">Open a new SEV</h1>

      <form onSubmit={handleSubmit} className="flex flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base text-foreground">Details</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="title">Title</Label>
              <Input id="title" required value={title} onChange={(e) => setTitle(e.target.value)} />
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="severity">Severity level</Label>
              <Select id="severity" value={severityLevel} onChange={(e) => setSeverityLevel(Number(e.target.value))}>
                <option value={1}>SEV-1 — Critical</option>
                <option value={2}>SEV-2 — Major</option>
                <option value={3}>SEV-3 — Minor</option>
                <option value={4}>SEV-4 — Low</option>
              </Select>
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="description">Description</Label>
              <Textarea id="description" value={description} onChange={(e) => setDescription(e.target.value)} />
            </div>

            <div className="flex flex-col gap-1.5">
              <Label>Affected services</Label>
              <ServiceChipEditor services={affectedServices} onChange={setAffectedServices} />
            </div>

            <LevelingCriteriaPanel
              severityLevel={severityLevel}
              serviceIds={affectedServices}
              services={registry.data?.services ?? []}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base text-foreground">Detection</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <DateTimeField id="started-at" label="Started at" value={startedAt} onChange={setStartedAt} />
              <DateTimeField id="detected-at" label="Detected at" value={detectedAt} onChange={setDetectedAt} />
            </div>

            <DetectionFields value={detection} onChange={setDetection} />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base text-foreground">Tags</CardTitle>
          </CardHeader>
          <CardContent>
            <TagRowsEditor rows={tags} onChange={setTags} />
          </CardContent>
        </Card>

        <Card>
          <CardContent className="flex flex-col gap-3 pt-4">
            <label className="flex items-center gap-2 text-sm">
              <Checkbox checked={sensitive} onChange={(e) => setSensitive(e.target.checked)} />
              Sensitive — restricts visibility and excludes this SEV from search, reports, and AI dispatch
            </label>
            <label className="flex items-center gap-2 text-sm">
              <Checkbox checked={aiDisabled} onChange={(e) => setAiDisabled(e.target.checked)} />
              Disable AI plugin dispatch for this SEV
            </label>
          </CardContent>
        </Card>

        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}

        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => navigate(-1)}>
            Cancel
          </Button>
          <Button type="submit" disabled={submitting || !title}>
            {submitting ? 'Creating…' : 'Create SEV'}
          </Button>
        </div>
      </form>
    </div>
  )
}
