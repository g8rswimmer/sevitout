import { Link, useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { EyeOff, FileText, Lock } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { useSevSocket } from '@/lib/ws'
import { useAuth } from '@/auth/useAuth'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import { SeverityBadge, StatusBadge } from '@/components/sev/badges'
import { StatusTransitionControl } from '@/components/sev/StatusTransitionControl'
import { LifecyclePanel } from '@/components/sev/LifecyclePanel'
import { DetailsPanel } from '@/components/sev/DetailsPanel'
import { RolesPanel } from '@/components/sev/RolesPanel'
import { AllowedViewersPanel } from '@/components/sev/AllowedViewersPanel'
import { AnnouncementsPanel } from '@/components/sev/AnnouncementsPanel'
import { ChatLogPanel } from '@/components/sev/ChatLogPanel'
import { TasksPanel } from '@/components/sev/TasksPanel'
import { LinkedSevsPanel } from '@/components/sev/LinkedSevsPanel'
import { ShareLinkControl } from '@/components/sev/ShareLinkControl'
import { hasRole } from '@/types/api'

export function SevDetailPage() {
  const { id } = useParams<{ id: string }>()
  const { user } = useAuth()
  const sevId = id!

  useSevSocket(sevId)

  const sev = useQuery({ queryKey: ['sevs', 'detail', sevId], queryFn: () => api.sevs.get(sevId) })

  const canRespond = hasRole(user?.org_role, 'responder')
  const canCommand = hasRole(user?.org_role, 'incident-commander')

  if (sev.isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-9 w-96" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }

  if (sev.isError) {
    // A 404 here is deliberately ambiguous — it's also what a caller sees
    // when a Sensitive SEV exists but they haven't been explicitly granted
    // access (§14), so this doesn't confirm or deny which case it is.
    const notFound = sev.error instanceof ApiError && sev.error.status === 404
    return (
      <p role="alert" className="text-sm text-destructive">
        {notFound
          ? "This SEV doesn't exist or you don't have access to view it."
          : `Failed to load SEV: ${(sev.error as Error).message}`}
      </p>
    )
  }

  const record = sev.data!
  const canEditDetails = canRespond && !record.locked

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <SeverityBadge level={record.severity_level} />
          <StatusBadge status={record.status} />
          {record.sensitive && (
            <Badge variant="destructive" className="gap-1">
              <EyeOff className="h-3 w-3" /> Sensitive
            </Badge>
          )}
          {record.locked && (
            <Badge variant="outline" className="gap-1">
              <Lock className="h-3 w-3" /> Locked
            </Badge>
          )}
          <span className="text-xs text-muted-foreground">{record.id}</span>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-2xl font-semibold">{record.title}</h1>
          <div className="flex gap-2">
            <ShareLinkControl sevId={sevId} canShare={canCommand && !record.sensitive} />
            <Link to={`/sevs/${sevId}/postmortem`} className={buttonVariants({ variant: 'outline', size: 'sm' })}>
              <FileText className="h-3.5 w-3.5" /> Postmortem
            </Link>
          </div>
        </div>
        <StatusTransitionControl sev={record} canTransition={canCommand} />
      </div>

      <DetailsPanel sev={record} canEdit={canEditDetails} />
      <LifecyclePanel sev={record} canEdit={canEditDetails} />
      <RolesPanel sevId={sevId} canManage={canCommand} />
      {record.sensitive && <AllowedViewersPanel sevId={sevId} canManage={canCommand} />}
      <AnnouncementsPanel sevId={sevId} canPost={canRespond} />
      <ChatLogPanel sevId={sevId} canAdd={canRespond} />
      <TasksPanel sevId={sevId} canManage={canRespond} defaultRepo={record.github_repo} />
      <LinkedSevsPanel sevId={sevId} canManage={canRespond} />
    </div>
  )
}
