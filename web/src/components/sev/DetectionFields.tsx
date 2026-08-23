import { useState } from 'react'
import { ImageOff } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import {
  DETECTION_METHOD_LABELS,
  MONITORING_TOOL_LABELS,
  type DetectionMethod,
  type MonitoringTool,
} from '@/types/api'

const DETECTION_METHODS = Object.keys(DETECTION_METHOD_LABELS) as DetectionMethod[]
const MONITORING_TOOLS = Object.keys(MONITORING_TOOL_LABELS) as MonitoringTool[]

const MONITORING_SELECT_NONE = 'none'
const MONITORING_SELECT_OTHER = 'other'

/** monitoring_tool is free text on the wire (see types/api.ts) — this derives
 * which dropdown option that text corresponds to: one of the named tools,
 * "None" for empty, or "Other" for anything else (shown via a companion
 * free-text input rather than lost). */
function monitoringSelectValue(monitoringTool: string): string {
  if (monitoringTool === '') return MONITORING_SELECT_NONE
  if ((MONITORING_TOOLS as string[]).includes(monitoringTool)) return monitoringTool
  return MONITORING_SELECT_OTHER
}

export interface DetectionFieldsValue {
  detectionMethod: DetectionMethod | ''
  monitoringTool: string
  alertName: string
  alertUrl: string
  metricLink: string
  snapshotUrl: string
}

/** The detection-metadata form fragment shared by SevCreatePage and
 * DetailsPanel's edit mode: the detection-method and monitoring-tool
 * dropdowns, the alert name directly above its link (the two describe the
 * same alert), and the two remaining supporting links (with a snapshot
 * preview). Lifecycle timestamps (started_at/detected_at) are deliberately
 * not part of this — they're LifecyclePanel's responsibility on the detail
 * page, and SevCreatePage renders its own started_at/detected_at inputs
 * directly. */
export function DetectionFields({
  value,
  onChange,
}: {
  value: DetectionFieldsValue
  onChange: (value: DetectionFieldsValue) => void
}) {
  // "Other" plus an empty custom name is indistinguishable from "None" by
  // looking at monitoringTool alone (both are ""), so which one the select
  // shows while the user is mid-typing needs its own bit of state rather
  // than being re-derived from value.monitoringTool on every render.
  const [otherSelected, setOtherSelected] = useState(
    () => monitoringSelectValue(value.monitoringTool) === MONITORING_SELECT_OTHER,
  )
  const monitoringSelect = otherSelected ? MONITORING_SELECT_OTHER : monitoringSelectValue(value.monitoringTool)

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="df-detection-method">Detection method</Label>
          <Select
            id="df-detection-method"
            value={value.detectionMethod}
            onChange={(e) => onChange({ ...value, detectionMethod: e.target.value as DetectionMethod | '' })}
          >
            <option value="">— Select —</option>
            {DETECTION_METHODS.map((m) => (
              <option key={m} value={m}>
                {DETECTION_METHOD_LABELS[m]}
              </option>
            ))}
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor="df-monitoring-tool">Monitoring tool</Label>
          <Select
            id="df-monitoring-tool"
            value={monitoringSelect}
            onChange={(e) => {
              const v = e.target.value
              if (v === MONITORING_SELECT_NONE) {
                setOtherSelected(false)
                onChange({ ...value, monitoringTool: '' })
              } else if (v === MONITORING_SELECT_OTHER) {
                setOtherSelected(true)
                onChange({ ...value, monitoringTool: '' })
              } else {
                setOtherSelected(false)
                onChange({ ...value, monitoringTool: v })
              }
            }}
          >
            <option value={MONITORING_SELECT_NONE}>None</option>
            {MONITORING_TOOLS.map((t) => (
              <option key={t} value={t}>
                {MONITORING_TOOL_LABELS[t]}
              </option>
            ))}
            <option value={MONITORING_SELECT_OTHER}>Other…</option>
          </Select>
          {monitoringSelect === MONITORING_SELECT_OTHER && (
            <Input
              aria-label="Custom monitoring tool name"
              placeholder="Name the tool"
              value={value.monitoringTool}
              onChange={(e) => onChange({ ...value, monitoringTool: e.target.value })}
            />
          )}
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="df-alert-name">Alert name</Label>
        <Input
          id="df-alert-name"
          placeholder="What the alert/page was called"
          value={value.alertName}
          onChange={(e) => onChange({ ...value, alertName: e.target.value })}
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="df-alert-url">Alert link</Label>
        <Input
          id="df-alert-url"
          type="url"
          placeholder="https://…"
          value={value.alertUrl}
          onChange={(e) => onChange({ ...value, alertUrl: e.target.value })}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="df-metric-link">Metric / dashboard link</Label>
          <Input
            id="df-metric-link"
            type="url"
            placeholder="https://…"
            value={value.metricLink}
            onChange={(e) => onChange({ ...value, metricLink: e.target.value })}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="df-snapshot-url">Snapshot image link</Label>
          <Input
            id="df-snapshot-url"
            type="url"
            placeholder="https://…"
            value={value.snapshotUrl}
            onChange={(e) => onChange({ ...value, snapshotUrl: e.target.value })}
          />
        </div>
      </div>

      {value.snapshotUrl && <SnapshotPreview url={value.snapshotUrl} />}
    </div>
  )
}

/** Exported for reuse by DetailsPanel's read-only view. */
export function SnapshotPreview({ url }: { url: string }) {
  // Reset the failed state whenever the URL itself changes, so editing a
  // previously-broken link gets a fresh attempt rather than staying stuck on
  // the error message.
  const [failedURL, setFailedURL] = useState<string | null>(null)
  const failed = failedURL === url

  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs text-muted-foreground">Snapshot preview</span>
      {failed ? (
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <ImageOff className="h-3.5 w-3.5" aria-hidden />
          Couldn't load an image from that link.
        </div>
      ) : (
        <img
          src={url}
          alt="Monitoring snapshot preview"
          className="max-h-48 w-auto rounded-md border border-border object-contain"
          onError={() => setFailedURL(url)}
        />
      )}
    </div>
  )
}
