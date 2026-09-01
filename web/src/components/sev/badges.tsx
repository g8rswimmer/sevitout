import { AlertTriangle } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { severityVariant } from '@/components/ui/badge'
import {
  EXTERNAL_SYSTEM_BADGE_CLASS,
  EXTERNAL_SYSTEM_LABELS,
  SEV_STATUS_BADGE_CLASS,
  SEV_STATUS_LABELS,
  type KnownExternalSystem,
  type SEVStatus,
  type SLAMetricStatus,
} from '@/types/api'

export function SeverityBadge({ level }: { level: number }) {
  return <Badge variant={severityVariant(level)}>SEV-{level}</Badge>
}

export function StatusBadge({ status }: { status: SEVStatus }) {
  return <Badge className={SEV_STATUS_BADGE_CLASS[status]}>{SEV_STATUS_LABELS[status]}</Badge>
}

/** Renders nothing for 'ok'/'not_applicable'/undefined — a badge should only
 * ever draw attention to a problem, never confirm the absence of one.
 * Mirrors TasksPanel.tsx's Overdue badge (destructive + AlertTriangle);
 * 'at_risk' gets its own amber treatment since it's a live warning about a
 * still-open SEV, not yet the harder "breached" fact StatusBadge's own
 * status colors otherwise reserve red for. */
export function SLABadge({ status, label = 'SLA' }: { status?: SLAMetricStatus; label?: string }) {
  if (status === 'breached') {
    return (
      <Badge variant="destructive" className="gap-1">
        <AlertTriangle className="h-3 w-3" /> {label} breached
      </Badge>
    )
  }
  if (status === 'at_risk') {
    return (
      <Badge className="gap-1 border-transparent bg-amber-500 text-white dark:bg-amber-600">
        <AlertTriangle className="h-3 w-3" /> {label} at risk
      </Badge>
    )
  }
  return null
}

function isKnownExternalSystem(system: string): system is KnownExternalSystem {
  return system === 'github' || system === 'jira' || system === 'generic'
}

/** A linked task's tracker — github/jira/generic render with their own
 * label+color (EXTERNAL_SYSTEM_LABELS/_BADGE_CLASS); anything else (the
 * field is unvalidated free text server-side) falls back to an outline
 * badge showing the raw value, rather than being silently mislabeled. */
export function ExternalSystemBadge({ system }: { system: string }) {
  if (isKnownExternalSystem(system)) {
    return <Badge className={EXTERNAL_SYSTEM_BADGE_CLASS[system]}>{EXTERNAL_SYSTEM_LABELS[system]}</Badge>
  }
  return <Badge variant="outline">{system}</Badge>
}
