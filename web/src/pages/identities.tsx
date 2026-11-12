import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Bot,
  Check,
  Copy,
  KeyRound,
  Loader2,
  RefreshCw,
  Server,
  Terminal,
  Trash2,
  X,
} from 'lucide-react'
import { useMemo, useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { useOrgEvents } from '@/lib/events'
import { ensureReauth } from '@/lib/reauth'
import { cn } from '@/lib/utils'
import { confirmAction } from '@/components/ui/dialogs'
import { Cmd, PageIntro } from '@/components/page-intro'
import { Input, Select } from '@/components/ui/field'

type Identity = components['schemas']['IdentityBody']
type Project = components['schemas']['ProjectBody']
type AuditEvent = components['schemas']['AuditEventBody']

const ROLES = ['Owner', 'Admin', 'Developer', 'Viewer'] as const

export function IdentitiesPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [token, setToken] = useState<{ value: string; name: string; agent: boolean } | null>(null)
  const [copied, setCopied] = useState(false)
  const [editingCommands, setEditingCommands] = useState<Identity | null>(null)

  const identities = useQuery({
    queryKey: ['identities', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/identities', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load identities.'))
      return result.data?.identities ?? []
    },
  })

  const projects = useQuery({
    queryKey: ['projects', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) return []
      const result = await api.GET('/api/v1/orgs/{org}/projects', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load projects.'))
      return result.data?.projects ?? []
    },
  })

  const activity = useQuery({
    // Own key: the audit page caches the full response under ['audit', org].
    queryKey: ['audit', org?.slug, 'agent-activity'],
    enabled: Boolean(org),
    refetchInterval: 30_000,
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/audit-events', {
        params: { path: { org: org.slug }, query: { limit: 200 } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load activity.'))
      return result.data?.events ?? []
    },
  })

  useOrgEvents(org?.slug, () => {
    void queryClient.invalidateQueries({ queryKey: ['audit', org?.slug] })
  })

  const agentActivity = useMemo(() => {
    return (activity.data ?? [])
      .filter((event) => event.actor.kind === 'agent' && event.action.startsWith('agent.'))
      .slice(-20)
      .reverse()
  }, [activity.data])

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['identities', org?.slug] })
  }

  const rotate = useMutation({
    mutationFn: async (identity: Identity) => {
      if (!org) throw new Error('No organization.')
      const reauthToken = await ensureReauth()
      const result = await api.POST('/api/v1/orgs/{org}/identities/{id}/tokens', {
        params: { path: { org: org.slug, id: identity.id } },
        headers: { 'X-Clavis-Reauth': reauthToken },
      })
      if (result.error || !result.data)
        throw new Error(apiErrorMessage(result.error, 'Could not rotate the token.'))
      return result.data.token
    },
    onSuccess: async (newToken, identity) => {
      setToken({ value: newToken, name: identity.name, agent: identity.type === 'agent' })
      await invalidate()
    },
  })

  const revoke = useMutation({
    mutationFn: async (identity: Identity) => {
      if (!org) throw new Error('No organization.')
      if (
        !(await confirmAction(
          `Revoke ${identity.name}? All of its tokens stop working immediately.`,
        ))
      )
        return
      const reauthToken = await ensureReauth()
      const result = await api.DELETE('/api/v1/orgs/{org}/identities/{id}', {
        params: { path: { org: org.slug, id: identity.id } },
        headers: { 'X-Clavis-Reauth': reauthToken },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not revoke the identity.'))
    },
    onSuccess: invalidate,
  })

  async function copyToken() {
    if (!token) return
    await navigator.clipboard.writeText(token.value)
    setCopied(true)
    window.setTimeout(() => {
      setCopied(false)
    }, 1500)
  }

  const canManage = org?.role === 'Owner' || org?.role === 'Admin'
  if (!canManage) {
    return (
      <div className="rounded-app border border-dashed border-border bg-panel p-8">
        <h1 className="font-head text-xl font-semibold">Agents &amp; machines</h1>
        <p className="mt-1 text-sm text-muted">
          You need the Admin or Owner role to manage identities.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="font-head text-2xl font-semibold tracking-tight">Agents &amp; machines</h1>
          <p className="mt-1 text-base text-muted">
            Logins for software instead of people: CI pipelines, servers, and AI coding agents.
          </p>
        </div>
        <Button
          variant="primary"
          size="md"
          onClick={() => {
            setCreating(true)
          }}
        >
          <KeyRound className="size-4" /> Add agent or machine
        </Button>
      </header>

      <PageIntro
        id="identities"
        summary={
          <>
            Software should not use a person&rsquo;s account. Give each pipeline, server, or AI
            agent its own login so its access is narrow, it expires, and everything it does shows up
            under its own name in the audit log.
          </>
        }
        steps={[
          {
            title: 'Machine: CI, servers, Kubernetes',
            body: (
              <>
                Gets a token with a role you choose, limited to one project and environment. Store
                it in your CI secrets as <Cmd>CLAVIS_TOKEN</Cmd>, then use <Cmd>clavis run</Cmd>{' '}
                there.
              </>
            ),
          },
          {
            title: 'AI agent: Claude Code, Cursor, Codex',
            body: 'Can see key names and run approved commands with secrets injected, but never reads a value itself. Each person connects it with their own login.',
          },
          {
            title: 'Allow commands for agents',
            body: (
              <>
                Agents start with nothing allowed. Add commands like <Cmd>npm test</Cmd>; anything
                else is refused and recorded.
              </>
            ),
          },
        ]}
        when="anything other than a person needs secrets. People sign in with their own account instead."
      />

      {token && (
        <section className="rounded-app border border-accent-line bg-accent-soft p-4">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              {token.agent ? (
                <>
                  <p className="text-sm font-semibold text-accent-text">
                    Connect {token.name} with your login
                  </p>
                  <p className="mt-0.5 text-xs text-muted">
                    Recommended. Each person runs this after{' '}
                    <span className="font-mono">clavis login</span>. It starts a one-hour session
                    held in memory, so no token goes in the MCP config.
                  </p>
                  <code className="mt-2 block break-all rounded-app-sm border border-border bg-bg px-2.5 py-1.5 font-mono text-xs text-text">
                    claude mcp add clavis -- clavis mcp --agent {token.name}
                  </code>
                  <p className="mt-3 text-xs text-muted">
                    Static token for headless agents only. It is shown once. Keep it out of any file
                    the agent can read.
                  </p>
                </>
              ) : (
                <>
                  <p className="text-sm font-semibold text-accent-text">Copy this token now</p>
                  <p className="mt-0.5 text-xs text-muted">
                    It is shown once and cannot be retrieved. Store it in your CI secret store.
                  </p>
                </>
              )}
              <code className="mt-2 block break-all rounded-app-sm border border-border bg-bg px-2.5 py-1.5 font-mono text-xs text-text">
                {token.value}
              </code>
            </div>
            <div className="flex flex-col gap-1.5">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  void copyToken()
                }}
              >
                {copied ? (
                  <Check className="size-3.5 text-accent-text" />
                ) : (
                  <Copy className="size-3.5" />
                )}
                {copied ? 'Copied' : 'Copy'}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setToken(null)
                }}
              >
                <X className="size-3.5" /> Dismiss
              </Button>
            </div>
          </div>
        </section>
      )}

      {identities.isLoading && (
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-sm text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading identities…
        </div>
      )}
      {identities.isError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-sm text-danger">
          {identities.error instanceof Error
            ? identities.error.message
            : 'Could not load identities.'}
        </div>
      )}

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[760px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
              <th className="px-4 py-2.5 font-semibold">Identity</th>
              <th className="px-4 py-2.5 font-semibold">Scope</th>
              <th className="px-4 py-2.5 font-semibold">Token</th>
              <th className="px-4 py-2.5 font-semibold">Last used</th>
              <th className="px-4 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {(identities.data ?? []).map((identity) => {
              const activeToken = identity.tokens?.[0]
              return (
                <tr
                  key={identity.id}
                  className="border-b border-border-soft align-top last:border-0"
                >
                  <td className="px-4 py-3">
                    <div className="flex items-start gap-2.5">
                      <div className="flex size-7 items-center justify-center rounded-app-sm bg-elev text-muted">
                        {identity.type === 'agent' ? (
                          <Bot className="size-3.5" />
                        ) : (
                          <Server className="size-3.5" />
                        )}
                      </div>
                      <div>
                        <p className="text-sm font-medium">{identity.name}</p>
                        <p className="text-xs text-dim">{identity.type}</p>
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-3 text-xs">
                    <p className="text-text">{identity.role}</p>
                    <p className="text-xs text-dim">
                      {identity.project_id
                        ? (projects.data?.find((item) => item.id === identity.project_id)?.name ??
                          'one project')
                        : 'all projects'}{' '}
                      · {identity.environment || 'any environment'}
                    </p>
                    {identity.type === 'agent' &&
                      ((identity.allowed_commands ?? []).length === 0 ? (
                        <button
                          type="button"
                          className="mt-1 inline-flex items-center gap-1 rounded bg-amber-bg px-1.5 py-0.5 text-2xs font-medium text-amber-fg hover:underline"
                          onClick={() => {
                            setEditingCommands(identity)
                          }}
                        >
                          No commands allowed yet — add some
                        </button>
                      ) : (
                        <p className="mt-0.5 text-2xs text-dim">
                          {(identity.allowed_commands ?? []).length} allowed command{' '}
                          {(identity.allowed_commands ?? []).length === 1 ? 'pattern' : 'patterns'}
                        </p>
                      ))}
                  </td>
                  <td className="px-4 py-3 font-mono text-2xs text-dim">
                    {activeToken ? (
                      <>
                        <p className="text-text">{activeToken.prefix}</p>
                        <p>
                          expires{' '}
                          {activeToken.expires_at ? formatDate(activeToken.expires_at) : 'never'}
                        </p>
                      </>
                    ) : (
                      <span className="text-danger">no active token</span>
                    )}
                    {identity.type === 'agent' && identity.active_sessions > 0 && (
                      <p className="mt-0.5 font-sans text-accent-text">
                        {identity.active_sessions} active{' '}
                        {identity.active_sessions === 1 ? 'session' : 'sessions'}
                      </p>
                    )}
                  </td>
                  <td className="px-4 py-3 text-xs text-dim">
                    {activeToken?.last_used_at ? formatDate(activeToken.last_used_at) : 'never'}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end gap-1">
                      {identity.type === 'agent' && (
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={() => {
                            setEditingCommands(identity)
                          }}
                        >
                          <Terminal className="size-3.5" /> Commands
                        </Button>
                      )}
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => {
                          rotate.mutate(identity)
                        }}
                        disabled={rotate.isPending}
                      >
                        <RefreshCw className="size-3.5" /> Rotate
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Revoke ${identity.name}`}
                        onClick={() => {
                          revoke.mutate(identity)
                        }}
                        disabled={revoke.isPending}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                  </td>
                </tr>
              )
            })}
            {(identities.data?.length ?? 0) === 0 && !identities.isLoading && (
              <tr>
                <td colSpan={5} className="px-4 py-10 text-center text-sm text-muted">
                  No identities yet. Create one for CI, Kubernetes, or an agent.
                </td>
              </tr>
            )}
          </tbody>
        </table>
        {(rotate.error || revoke.error) && (
          <p className="px-4 py-3 text-xs text-danger">
            {rotate.error instanceof Error ? rotate.error.message : ''}
            {revoke.error instanceof Error ? revoke.error.message : ''}
          </p>
        )}
      </section>

      <section className="rounded-app border border-border bg-surface">
        <div className="flex items-center justify-between border-b border-border px-4 py-2.5">
          <div>
            <h2 className="text-base font-semibold">Recent agent activity</h2>
            <p className="text-xs text-dim">
              Command uses, denials, allowlist changes, and secret requests.
            </p>
          </div>
          {activity.isLoading && <Loader2 className="size-3.5 animate-spin text-dim" />}
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] border-collapse text-left">
            <thead>
              <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
                <th className="px-4 py-2 font-semibold">Time</th>
                <th className="px-4 py-2 font-semibold">Agent</th>
                <th className="px-4 py-2 font-semibold">Action</th>
                <th className="px-4 py-2 font-semibold">Command / key</th>
                <th className="px-4 py-2 font-semibold">Environment</th>
              </tr>
            </thead>
            <tbody>
              {agentActivity.map((event: AuditEvent) => (
                <tr key={event.id} className="border-b border-border-soft last:border-0">
                  <td className="px-4 py-2 text-xs text-dim">{formatDate(event.created_at)}</td>
                  <td className="px-4 py-2 text-xs">{event.actor.name || event.actor.id}</td>
                  <td className="px-4 py-2 font-mono text-2xs text-muted">{event.action}</td>
                  <td className="px-4 py-2 font-mono text-2xs text-muted">
                    {event.details.command ||
                      event.details.requested_key ||
                      event.details.schema_key ||
                      (event.details.commands ?? []).join(', ')}
                  </td>
                  <td className="px-4 py-2 text-xs text-dim">{event.details.environment || '—'}</td>
                </tr>
              ))}
              {agentActivity.length === 0 && !activity.isLoading && (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-sm text-muted">
                    No agent activity yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      {editingCommands && org && (
        <AgentCommandsDialog
          orgSlug={org.slug}
          identity={editingCommands}
          onClose={() => {
            setEditingCommands(null)
          }}
          onSaved={() => {
            setEditingCommands(null)
            void invalidate()
          }}
        />
      )}

      {creating && (
        <CreateIdentityDialog
          orgSlug={org?.slug ?? ''}
          actorRole={org?.role ?? 'Developer'}
          projects={projects.data ?? []}
          onClose={() => {
            setCreating(false)
          }}
          onCreated={(newToken, name, agent) => {
            setCreating(false)
            setToken({ value: newToken, name, agent })
            void invalidate()
          }}
        />
      )}
    </div>
  )
}

function CreateIdentityDialog({
  orgSlug,
  actorRole,
  projects,
  onClose,
  onCreated,
}: {
  orgSlug: string
  actorRole: string
  projects: Project[]
  onClose: () => void
  onCreated: (token: string, name: string, agent: boolean) => void
}) {
  const [name, setName] = useState('')
  const [type, setType] = useState('machine')
  const [role, setRole] = useState('Developer')
  const [project, setProject] = useState('')
  const [environment, setEnvironment] = useState('dev')
  const [days, setDays] = useState(90)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/orgs/{org}/identities', {
      params: { path: { org: orgSlug } },
      body: {
        name,
        type: type as 'machine' | 'agent',
        project,
        environment,
        expires_in_days: days,
        ...(type === 'agent' ? {} : { role: role as (typeof ROLES)[number] }),
      },
    })
    setBusy(false)
    if (result.error || !result.data) {
      setError(apiErrorMessage(result.error, 'Could not create the identity.'))
      return
    }
    onCreated(result.data.token, result.data.identity.name, result.data.identity.type === 'agent')
  }

  return (
    <Modal onClose={onClose} className="max-w-lg">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="font-head text-lg font-semibold">Add agent or machine</h2>
          <p className="mt-0.5 text-sm text-muted">
            The token is shown once. Scope it as tightly as possible.
          </p>
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
          <X className="size-4" />
        </Button>
      </div>
      <form
        onSubmit={(event) => {
          void submit(event)
        }}
        className="mt-4 space-y-3"
      >
        <fieldset>
          <legend className="mb-1.5 text-xs font-medium text-muted">What is it for?</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            {(
              [
                [
                  'machine',
                  'A machine',
                  'CI pipeline, server, or Kubernetes. Gets a token with a role you pick.',
                ],
                [
                  'agent',
                  'An AI agent',
                  'Claude Code, Cursor, Codex. Runs approved commands; never sees values.',
                ],
              ] as const
            ).map(([value, title, body]) => (
              <label
                key={value}
                className={cn(
                  'flex cursor-pointer flex-col rounded-app-sm border px-3 py-2.5 transition-colors',
                  'has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-accent-line',
                  type === value
                    ? 'border-accent-line bg-accent-soft/40'
                    : 'border-border hover:bg-hover',
                )}
              >
                <input
                  type="radio"
                  name="identity-type"
                  className="sr-only"
                  checked={type === value}
                  onChange={() => {
                    setType(value)
                    setDays(value === 'agent' ? 1 : 90)
                  }}
                />
                <span className="text-sm font-medium">{title}</span>
                <span className="mt-0.5 text-xs leading-snug text-muted">{body}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <div className="grid gap-3">
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted">Name</span>
            <Input
              autoFocus
              required
              className="h-9"
              placeholder={type === 'agent' ? 'claude-code' : 'ci-runner'}
              value={name}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          {type === 'machine' ? (
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Role</span>
              <Select
                className="h-9"
                value={role}
                onChange={(event) => {
                  setRole(event.target.value)
                }}
              >
                {ROLES.filter((candidate) => candidate !== 'Owner' || actorRole === 'Owner').map(
                  (candidate) => (
                    <option key={candidate} value={candidate}>
                      {candidate}
                    </option>
                  ),
                )}
              </Select>
            </label>
          ) : (
            <div className="rounded-app-sm border border-border bg-elev px-2.5 py-2 text-xs text-muted">
              Agents get <span className="font-mono text-text">read-names</span> +{' '}
              <span className="font-mono text-text">use</span>: through Clavis they never get values
              directly. The command allowlist starts deny-all. Connect it with{' '}
              <span className="font-mono text-text">clavis mcp --agent NAME</span>; no token goes in
              the MCP config.
            </div>
          )}
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted">Expires in</span>
            <Select
              className="h-9"
              value={days}
              onChange={(event) => {
                setDays(Number(event.target.value))
              }}
            >
              {type === 'agent' ? (
                <>
                  <option value={1}>1 day</option>
                  <option value={3}>3 days</option>
                  <option value={7}>7 days</option>
                </>
              ) : (
                <>
                  <option value={30}>30 days</option>
                  <option value={90}>90 days</option>
                  <option value={180}>180 days</option>
                  <option value={365}>365 days</option>
                </>
              )}
            </Select>
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted">Project</span>
            <Select
              className="h-9"
              value={project}
              onChange={(event) => {
                setProject(event.target.value)
              }}
            >
              <option value="">All projects</option>
              {projects.map((candidate) => (
                <option key={candidate.id} value={candidate.slug}>
                  {candidate.name}
                </option>
              ))}
            </Select>
          </label>
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted">Environment glob</span>
            <Input
              className="h-9 font-mono"
              placeholder="dev or *"
              value={environment}
              onChange={(event) => {
                setEnvironment(event.target.value)
              }}
            />
          </label>
        </div>
        <p className="rounded-app-sm bg-elev px-2.5 py-2 text-xs text-muted">
          Agents are denied staging and production by default. After creating the identity, use{' '}
          <span className="font-mono text-text">Commands</span> or{' '}
          <span className="font-mono text-text">clavis agents allow</span> to approve commands.
        </p>
        {error && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" size="sm" disabled={busy}>
            {busy && <Loader2 className="size-3.5 animate-spin" />} Create token
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function AgentCommandsDialog({
  orgSlug,
  identity,
  onClose,
  onSaved,
}: {
  orgSlug: string
  identity: Identity
  onClose: () => void
  onSaved: () => void
}) {
  const [commands, setCommands] = useState((identity.allowed_commands ?? []).join('\n'))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function save() {
    setBusy(true)
    setError(null)
    const result = await api.PUT('/api/v1/orgs/{org}/identities/{id}/commands', {
      params: { path: { org: orgSlug, id: identity.id } },
      body: {
        commands: commands
          .split('\n')
          .map((command) => command.trim())
          .filter((command) => command.length > 0),
      },
    })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not save the allowlist.'))
      return
    }
    onSaved()
  }

  return (
    <Modal onClose={onClose} className="max-w-lg">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="font-head text-lg font-semibold">Command allowlist</h2>
          <p className="mt-0.5 text-sm text-muted">
            {identity.name} · deny-all until a pattern matches. One pattern per line.
          </p>
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
          <X className="size-4" />
        </Button>
      </div>
      <textarea
        autoFocus
        rows={8}
        className="mt-4 w-full rounded-app-sm border border-border bg-bg px-3 py-2 font-mono text-sm text-text focus:border-accent-line focus:outline-none"
        placeholder={'npm test\ngo test ./...\npnpm dev'}
        value={commands}
        onChange={(event) => {
          setCommands(event.target.value)
        }}
      />
      <p className="mt-2 text-xs text-muted">
        Patterns support <span className="font-mono">*</span> and{' '}
        <span className="font-mono">?</span>. A bare <span className="font-mono">*</span> is
        rejected. Commands are checked before values are decrypted and every denial is audited.
      </p>
      {error && (
        <p className="mt-2 rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
      )}
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button variant="primary" size="sm" disabled={busy} onClick={() => void save()}>
          {busy && <Loader2 className="size-3.5 animate-spin" />} Save allowlist
        </Button>
      </div>
    </Modal>
  )
}
