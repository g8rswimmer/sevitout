import { Badge } from '@/components/ui/badge'
import { severityVariant } from '@/components/ui/badge'
import {
  EXTERNAL_SYSTEM_BADGE_CLASS,
  EXTERNAL_SYSTEM_LABELS,
  SEV_STATUS_BADGE_CLASS,
  SEV_STATUS_LABELS,
  type KnownExternalSystem,
  type SEVStatus,
} from '@/types/api'

export function SeverityBadge({ level }: { level: number }) {
  return <Badge variant={severityVariant(level)}>SEV-{level}</Badge>
}

export function StatusBadge({ status }: { status: SEVStatus }) {
  return <Badge className={SEV_STATUS_BADGE_CLASS[status]}>{SEV_STATUS_LABELS[status]}</Badge>
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
