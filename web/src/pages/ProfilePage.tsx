import { useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { useAuth } from '@/auth/useAuth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Section } from '@/components/sev/Section'
import type { WhoAmIResponse } from '@/types/api'

/** Self-service integration-identity management (Roadmap Phase 10b): every
 * authenticated user manages their own Slack user ID, GitHub username, and
 * Jira account ID here, used to widen Slack auto-invite to every assigned
 * role and default new tracker issues to the creating user.
 *
 * Explicitly out of scope: name/avatar/password editing — a known
 * limitation/follow-up, not silently expanded into here. */
export function ProfilePage() {
  const { user } = useAuth()
  // ProfileForm only mounts once `user` is loaded, so its useState
  // initializers below see real values instead of AuthContext's async
  // WhoAmI hydration racing an empty first render.
  if (!user) return null
  return <ProfileForm user={user} />
}

function ProfileForm({ user }: { user: WhoAmIResponse }) {
  const { refreshUser } = useAuth()

  const [slackUserId, setSlackUserId] = useState(user.slack_user_id ?? '')
  const [githubUsername, setGithubUsername] = useState(user.github_username ?? '')
  const [jiraAccountId, setJiraAccountId] = useState(user.jira_account_id ?? '')
  const [savedAt, setSavedAt] = useState<number | null>(null)

  const save = useMutation({
    mutationFn: () =>
      api.auth.updateMyIntegrationIdentities({
        slack_user_id: slackUserId.trim(),
        github_username: githubUsername.trim(),
        jira_account_id: jiraAccountId.trim(),
      }),
    onSuccess: async () => {
      setSavedAt(Date.now())
      await refreshUser()
    },
  })

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    save.mutate()
  }

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-2xl font-semibold">My Profile</h1>

      <Section title="Identity">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:max-w-md">
          <div>
            <Label className="text-xs text-muted-foreground">Name</Label>
            <p className="text-sm">{user.name}</p>
          </div>
          <div>
            <Label className="text-xs text-muted-foreground">Email</Label>
            <p className="text-sm">{user.email}</p>
          </div>
        </div>
      </Section>

      <Section title="Integration identities">
        <p className="text-sm text-muted-foreground">
          Set these so Sevitout can invite you to a SEV's Slack channel and pre-fill you as the assignee when you
          create a GitHub or Jira issue from a SEV.
        </p>
        <form onSubmit={handleSubmit} className="flex max-w-md flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="slack-user-id">Slack User ID</Label>
            <Input
              id="slack-user-id"
              placeholder="e.g. U0123ABCDEF"
              value={slackUserId}
              onChange={(e) => setSlackUserId(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              In Slack, open your profile → "More" → "Copy member ID". Leave blank to be resolved by your email
              instead, when possible.
            </p>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="github-username">GitHub Username</Label>
            <Input
              id="github-username"
              placeholder="e.g. octocat"
              value={githubUsername}
              onChange={(e) => setGithubUsername(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">Your GitHub login — pre-fills the assignee when you create a GitHub issue.</p>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="jira-account-id">Jira Account ID</Label>
            <Input
              id="jira-account-id"
              placeholder="e.g. 5b10a2844c20165700ede21g"
              value={jiraAccountId}
              onChange={(e) => setJiraAccountId(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              An opaque Jira Cloud ID, not your email or display name — find it by opening your own Jira profile page
              and copying the ID from its URL, or ask an admin. There is no lookup-by-email helper yet.
            </p>
          </div>

          <div className="flex items-center gap-3">
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? 'Saving…' : 'Save'}
            </Button>
            {savedAt && !save.isPending && !save.isError && (
              <span className="text-sm text-muted-foreground">Saved.</span>
            )}
          </div>
          {save.isError && (
            <p role="alert" className="text-sm text-destructive">
              {save.error instanceof ApiError ? save.error.message : 'Failed to save'}
            </p>
          )}
        </form>
      </Section>
    </div>
  )
}
