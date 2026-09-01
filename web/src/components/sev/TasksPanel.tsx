import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, ExternalLink, Trash2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import { useAuth } from '@/auth/useAuth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Section } from '@/components/sev/Section'
import { AssigneePicker } from '@/components/sev/AssigneePicker'
import { ExternalSystemBadge } from '@/components/sev/badges'
import { formatDateTime } from '@/lib/format'
import { useEnabledIntegrations } from '@/lib/useEnabledIntegrations'
import { TASK_RELATIONSHIP_LABELS, type TaskPriority, type TaskRelationshipType } from '@/types/api'

const RELATIONSHIP_TYPES = Object.keys(TASK_RELATIONSHIP_LABELS) as TaskRelationshipType[]

type Mode = 'link' | 'github' | 'jira'

/** "owner/repo" → its parts, or null if it isn't shaped that way. */
function parseRepo(value?: string): { owner: string; repo: string } | null {
  if (!value) return null
  const i = value.indexOf('/')
  if (i <= 0 || i === value.length - 1) return null
  return { owner: value.slice(0, i), repo: value.slice(i + 1) }
}

export function TasksPanel({
  sevId,
  canManage,
  defaultRepo,
}: {
  sevId: string
  canManage: boolean
  /** The SEV's Details-panel "owner/repo" (SEVResponse.github_repo), used to
   * pre-fill the Create-GitHub-issue form so a Responder isn't retyping it. */
  defaultRepo?: string
}) {
  const queryClient = useQueryClient()
  const tasks = useQuery({ queryKey: ['sevs', sevId, 'tasks'], queryFn: () => api.tasks.list(sevId) })
  const { user } = useAuth()
  // Roadmap Phase 11b: hide a tracker's create-issue action when it isn't
  // configured — if only Jira is enabled, GitHub's option isn't offered.
  const { isEnabled: isIntegrationEnabled } = useEnabledIntegrations()
  const githubEnabled = isIntegrationEnabled('github')
  const jiraEnabled = isIntegrationEnabled('jira')

  const [mode, setMode] = useState<Mode>('link')
  const [url, setUrl] = useState('')
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [relationshipType, setRelationshipType] = useState<TaskRelationshipType>('action-item')
  const [priority, setPriority] = useState<TaskPriority>('non-critical')
  const [owner, setOwner] = useState('')
  const [repo, setRepo] = useState('')
  const [projectKey, setProjectKey] = useState('')
  const [issueType, setIssueType] = useState('')
  // Assignee: the tracker-native value actually submitted (a GitHub login
  // or Jira account ID), the display name shown in its place, and the
  // Sevitout user ID sent alongside so the server can re-resolve that name
  // into TaskResponse.assignee_name — set together by AssigneePicker.tsx's
  // search-and-pick UI, pre-filled from the caller's own stored integration
  // identity (Roadmap Phase 10f), clearable, and omitted from the request
  // payload when empty.
  const [githubAssignee, setGithubAssignee] = useState('')
  const [githubAssigneeName, setGithubAssigneeName] = useState('')
  const [githubAssigneeUserId, setGithubAssigneeUserId] = useState('')
  const [jiraAssignee, setJiraAssignee] = useState('')
  const [jiraAssigneeName, setJiraAssigneeName] = useState('')
  const [jiraAssigneeUserId, setJiraAssigneeUserId] = useState('')
  const [error, setError] = useState<string | null>(null)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['sevs', sevId, 'tasks'] })

  const link = useMutation({
    mutationFn: () =>
      api.tasks.link(sevId, {
        external_system: 'generic',
        task_id: url,
        url,
        title,
        description: description || undefined,
        relationship_type: relationshipType,
        priority,
      }),
    onSuccess: () => {
      setUrl('')
      setTitle('')
      setDescription('')
      setError(null)
      void invalidate()
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to link task'),
  })

  const createIssue = useMutation({
    mutationFn: () =>
      api.tasks.createGitHubIssue(sevId, {
        owner,
        repo,
        title,
        body: description || undefined,
        relationship_type: relationshipType,
        priority,
        assignee: githubAssignee.trim() || undefined,
        assignee_user_id: githubAssigneeUserId || undefined,
      }),
    onSuccess: () => {
      setOwner('')
      setRepo('')
      setTitle('')
      setDescription('')
      setGithubAssignee('')
      setGithubAssigneeName('')
      setGithubAssigneeUserId('')
      setError(null)
      void invalidate()
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to create GitHub issue'),
  })

  const createJiraIssue = useMutation({
    mutationFn: () =>
      api.tasks.createJiraIssue(sevId, {
        project_key: projectKey,
        issue_type: issueType,
        summary: title,
        description: description || undefined,
        relationship_type: relationshipType,
        priority,
        assignee_account_id: jiraAssignee.trim() || undefined,
        assignee_user_id: jiraAssigneeUserId || undefined,
      }),
    onSuccess: () => {
      setProjectKey('')
      setIssueType('')
      setTitle('')
      setDescription('')
      setJiraAssignee('')
      setJiraAssigneeName('')
      setJiraAssigneeUserId('')
      setError(null)
      void invalidate()
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'Failed to create Jira issue'),
  })

  const unlink = useMutation({
    mutationFn: (id: string) => api.tasks.unlink(sevId, id),
    onSuccess: () => void invalidate(),
  })

  const list = tasks.data?.tasks ?? []

  return (
    <Section title="Linked tasks">
      {tasks.isLoading && <Skeleton className="h-8 w-full" />}
      {tasks.isError && (
        <p role="alert" className="text-sm text-destructive">
          Failed to load tasks: {(tasks.error as Error).message}
        </p>
      )}
      {tasks.data && list.length === 0 && <p className="text-sm text-muted-foreground">No tasks linked yet.</p>}
      {list.length > 0 && (
        <ul className="flex flex-col gap-2">
          {list.map((t) => (
            <li key={t.id} className="flex items-start justify-between gap-2 text-sm">
              <div className="flex min-w-0 flex-col gap-1">
                <a
                  href={t.url}
                  target="_blank"
                  rel="noreferrer"
                  className="flex items-center gap-1 truncate font-medium hover:underline"
                >
                  {t.title || t.task_id}
                  <ExternalLink className="h-3 w-3 shrink-0" aria-hidden />
                </a>
                <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                  <ExternalSystemBadge system={t.external_system} />
                  <Badge variant="outline">{TASK_RELATIONSHIP_LABELS[t.relationship_type]}</Badge>
                  <Badge variant={t.priority === 'critical' ? 'destructive' : 'secondary'}>{t.priority}</Badge>
                  {t.overdue && (
                    <Badge variant="destructive" className="gap-1">
                      <AlertTriangle className="h-3 w-3" /> Overdue
                    </Badge>
                  )}
                  {t.due_date && <span>Due {formatDateTime(t.due_date)}</span>}
                  {/* assignee_name (a Sevitout user's display name, resolved
                      server-side from assignee_user_id) is preferred over
                      assignee itself — the raw GitHub login/Jira account ID
                      is only shown as a fallback for a task whose assignee
                      wasn't picked from Sevitout's own directory. */}
                  {(t.assignee_name || t.assignee) && <span>Assigned to {t.assignee_name || t.assignee}</span>}
                </div>
              </div>
              {canManage && (
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Unlink ${t.title || t.task_id}`}
                  onClick={() => unlink.mutate(t.id)}
                  disabled={unlink.isPending}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      {canManage && (
        <div className="flex flex-col gap-2 border-t border-border pt-3">
          <div className="flex gap-2">
            <Button type="button" size="sm" variant={mode === 'link' ? 'default' : 'outline'} onClick={() => setMode('link')}>
              Link existing
            </Button>
            {githubEnabled && (
              <Button
                type="button"
                size="sm"
                variant={mode === 'github' ? 'default' : 'outline'}
                onClick={() => {
                  setMode('github')
                  if (!owner && !repo) {
                    const parsed = parseRepo(defaultRepo)
                    if (parsed) {
                      setOwner(parsed.owner)
                      setRepo(parsed.repo)
                    }
                  }
                  if (!githubAssignee && user?.github_username) {
                    setGithubAssignee(user.github_username)
                    setGithubAssigneeName(user.name)
                    setGithubAssigneeUserId(user.id)
                  }
                }}
              >
                Create GitHub issue
              </Button>
            )}
            {jiraEnabled && (
              <Button
                type="button"
                size="sm"
                variant={mode === 'jira' ? 'default' : 'outline'}
                onClick={() => {
                  setMode('jira')
                  if (!jiraAssignee && user?.jira_account_id) {
                    setJiraAssignee(user.jira_account_id)
                    setJiraAssigneeName(user.name)
                    setJiraAssigneeUserId(user.id)
                  }
                }}
              >
                Create Jira issue
              </Button>
            )}
          </div>

          <form
            className="flex flex-col gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (mode === 'link' && url.trim() && title.trim()) link.mutate()
              if (mode === 'github' && owner.trim() && repo.trim() && title.trim()) createIssue.mutate()
              if (mode === 'jira' && projectKey.trim() && issueType.trim() && title.trim()) createJiraIssue.mutate()
            }}
          >
            {mode === 'link' && (
              <Input aria-label="Task URL" placeholder="https://github.com/org/repo/issues/42" value={url} onChange={(e) => setUrl(e.target.value)} />
            )}
            {mode === 'github' && (
              <>
                <div className="flex gap-2">
                  <Input aria-label="Owner" placeholder="owner" value={owner} onChange={(e) => setOwner(e.target.value)} className="w-1/2" />
                  <Input aria-label="Repo" placeholder="repo" value={repo} onChange={(e) => setRepo(e.target.value)} className="w-1/2" />
                </div>
                <AssigneePicker
                  field="github_username"
                  value={githubAssignee}
                  selectedName={githubAssigneeName}
                  onSelect={(u) => {
                    setGithubAssignee(u.github_username!)
                    setGithubAssigneeName(u.name)
                    setGithubAssigneeUserId(u.id)
                  }}
                  onClear={() => {
                    setGithubAssignee('')
                    setGithubAssigneeName('')
                    setGithubAssigneeUserId('')
                  }}
                />
              </>
            )}
            {mode === 'jira' && (
              <>
                <div className="flex gap-2">
                  <Input
                    aria-label="Project key"
                    placeholder="e.g. OPS"
                    value={projectKey}
                    onChange={(e) => setProjectKey(e.target.value)}
                    className="w-1/2"
                  />
                  <Input
                    aria-label="Issue type"
                    placeholder="e.g. Task, Bug"
                    value={issueType}
                    onChange={(e) => setIssueType(e.target.value)}
                    className="w-1/2"
                  />
                </div>
                <AssigneePicker
                  field="jira_account_id"
                  value={jiraAssignee}
                  selectedName={jiraAssigneeName}
                  onSelect={(u) => {
                    setJiraAssignee(u.jira_account_id!)
                    setJiraAssigneeName(u.name)
                    setJiraAssigneeUserId(u.id)
                  }}
                  onClear={() => {
                    setJiraAssignee('')
                    setJiraAssigneeName('')
                    setJiraAssigneeUserId('')
                  }}
                />
              </>
            )}
            <Input
              aria-label={mode === 'jira' ? 'Summary' : 'Title'}
              placeholder={mode === 'jira' ? 'Summary' : 'Title'}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
            <Textarea
              aria-label="Description"
              placeholder="Description (pre-filled with SEV context for a new issue)"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
            <div className="flex flex-wrap items-center gap-2">
              <Select
                aria-label="Relationship type"
                value={relationshipType}
                onChange={(e) => setRelationshipType(e.target.value as TaskRelationshipType)}
                className="w-44"
              >
                {RELATIONSHIP_TYPES.map((rt) => (
                  <option key={rt} value={rt}>
                    {TASK_RELATIONSHIP_LABELS[rt]}
                  </option>
                ))}
              </Select>
              <Select aria-label="Priority" value={priority} onChange={(e) => setPriority(e.target.value as TaskPriority)} className="w-36">
                <option value="critical">Critical</option>
                <option value="non-critical">Non-critical</option>
              </Select>
              <Button type="submit" size="sm" disabled={link.isPending || createIssue.isPending || createJiraIssue.isPending}>
                {mode === 'link' &&
                  (link.isPending ? 'Linking…' : 'Link task')}
                {mode === 'github' &&
                  (createIssue.isPending ? 'Creating…' : 'Create issue')}
                {mode === 'jira' &&
                  (createJiraIssue.isPending ? 'Creating…' : 'Create issue')}
              </Button>
            </div>
          </form>
        </div>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </Section>
  )
}
