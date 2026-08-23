import { Badge } from '@/components/ui/badge'
import { severityVariant } from '@/components/ui/badge'
import { SEV_STATUS_BADGE_CLASS, SEV_STATUS_LABELS, type SEVStatus } from '@/types/api'

export function SeverityBadge({ level }: { level: number }) {
  return <Badge variant={severityVariant(level)}>SEV-{level}</Badge>
}

export function StatusBadge({ status }: { status: SEVStatus }) {
  return <Badge className={SEV_STATUS_BADGE_CLASS[status]}>{SEV_STATUS_LABELS[status]}</Badge>
}
