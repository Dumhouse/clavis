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
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { useOrgEvents } from '@/lib/events'
import { ensureReauth } from '@/lib/reauth'

type Identity = components['schemas']['IdentityBody']
type Project = components['schemas']['ProjectBody']
type AuditEvent = components['schemas']['AuditEventBody']

const ROLES = ['Owner', 'Admin', 'Developer', 'Viewer'] as const

export function IdentitiesPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [token, setToken] = useState<string | null>(null)
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
    queryKey: ['audit', org?.slug],
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
    onSuccess: async (newToken) => {
      setToken(newToken)
      await invalidate()
    },
  })

  const revoke = useMutation({
    mutationFn: async (identity: Identity) => {
      if (!org) throw new Error('No organization.')
      if (!window.confirm(`Revoke ${identity.name}? All of its tokens stop working immediately.`))
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
    await navigator.clipboard.writeText(token)
    setCopied(true)
    window.setTimeout(() => {
      setCopied(false)
    }, 1500)
  }

  const canManage = org?.role === 'Owner' || org?.role === 'Admin'
  if (!canManage) {
    return (
      <div className="rounded-app border border-dashed border-border bg-panel p-8">
        <h1 className="font-head text-[21px] font-semibold">Identities</h1>
        <p className="mt-1 text-[13.5px] text-muted">
          You need the Admin or Owner role to manage identities.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="font-head text-[25px] font-semibold tracking-tight">Identities</h1>
          <p className="mt-1 text-[14px] text-muted">
            Machine and agent tokens scoped to a project and environment. Tokens are stored hashed
            and shown once.
          </p>
        </div>
        <Button
          variant="primary"
          size="md"
          onClick={() => {
            setCreating(true)
          }}
        >
          <KeyRound className="size-4" /> New identity
        </Button>
      </header>

      {token && (
        <section className="rounded-app border border-accent-line bg-accent-soft p-4">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="text-[13px] font-semibold text-accent-text">Copy this token now</p>
              <p className="mt-0.5 text-[12.5px] text-muted">
                It is shown once and cannot be retrieved. Store it in your CI secret store.
              </p>
              <code className="mt-2 block break-all rounded-app-sm border border-border bg-bg px-2.5 py-1.5 font-mono text-[12px] text-text">
                {token}
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
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-[13px] text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading identities…
        </div>
      )}
      {identities.isError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-[13px] text-danger">
          {identities.error instanceof Error
            ? identities.error.message
            : 'Could not load identities.'}
        </div>
      )}

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[760px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-[11.5px] uppercase tracking-[0.06em] text-dim">
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
                        <p className="text-[13px] font-medium">{identity.name}</p>
                        <p className="text-[12px] text-dim">{identity.type}</p>
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-3 text-[12.5px]">
                    <p className="text-text">{identity.role}</p>
                    <p className="text-[12px] text-dim">
                      {identity.project_id ? 'one project' : 'all projects'} ·{' '}
                      {identity.environment || '*'}
                    </p>
                    {identity.type === 'agent' && (
                      <p className="mt-0.5 text-[11.5px] text-dim">
                        {(identity.allowed_commands ?? []).length === 0
                          ? 'commands: deny all'
                          : `commands: ${String((identity.allowed_commands ?? []).length)} pattern(s)`}
                      </p>
                    )}
                  </td>
                  <td className="px-4 py-3 font-mono text-[11.5px] text-dim">
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
                  </td>
                  <td className="px-4 py-3 text-[12px] text-dim">
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
                <td colSpan={5} className="px-4 py-10 text-center text-[13px] text-muted">
                  No identities yet. Create one for CI, Kubernetes, or an agent.
                </td>
              </tr>
            )}
          </tbody>
        </table>
        {(rotate.error || revoke.error) && (
          <p className="px-4 py-3 text-[12.5px] text-danger">
            {rotate.error instanceof Error ? rotate.error.message : ''}
            {revoke.error instanceof Error ? revoke.error.message : ''}
          </p>
        )}
      </section>

      <section className="rounded-app border border-border bg-surface">
        <div className="flex items-center justify-between border-b border-border px-4 py-2.5">
          <div>
            <h2 className="text-[14px] font-semibold">Recent agent activity</h2>
            <p className="text-[12px] text-dim">
              Command uses, denials, allowlist changes, and secret requests.
            </p>
          </div>
          {activity.isLoading && <Loader2 className="size-3.5 animate-spin text-dim" />}
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] border-collapse text-left">
            <thead>
              <tr className="border-b border-border text-[11.5px] uppercase tracking-[0.06em] text-dim">
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
                  <td className="px-4 py-2 text-[12px] text-dim">{formatDate(event.created_at)}</td>
                  <td className="px-4 py-2 text-[12.5px]">{event.actor.name || event.actor.id}</td>
                  <td className="px-4 py-2 font-mono text-[11.5px] text-muted">{event.action}</td>
                  <td className="px-4 py-2 font-mono text-[11.5px] text-muted">
                    {event.details.command ||
                      event.details.requested_key ||
                      event.details.schema_key ||
                      (event.details.commands ?? []).join(', ')}
                  </td>
                  <td className="px-4 py-2 text-[12px] text-dim">
                    {event.details.environment || '—'}
                  </td>
                </tr>
              ))}
              {agentActivity.length === 0 && !activity.isLoading && (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-[13px] text-muted">
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
          onCreated={(newToken) => {
            setCreating(false)
            setToken(newToken)
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
  onCreated: (token: string) => void
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
    onCreated(result.data.token)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="w-full max-w-lg rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[18px] font-semibold">New identity</h2>
            <p className="mt-0.5 text-[13px] text-muted">
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
          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Name</span>
              <input
                autoFocus
                required
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
                placeholder="ci-runner"
                value={name}
                onChange={(event) => {
                  setName(event.target.value)
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Type</span>
              <select
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
                value={type}
                onChange={(event) => {
                  setType(event.target.value)
                  if (event.target.value === 'agent') setDays(1)
                }}
              >
                <option value="machine">Machine</option>
                <option value="agent">Agent</option>
              </select>
            </label>
          </div>
          <div className="grid grid-cols-2 gap-3">
            {type === 'machine' ? (
              <label className="block">
                <span className="mb-1 block text-[12.5px] font-medium text-muted">Role</span>
                <select
                  className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
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
                </select>
              </label>
            ) : (
              <div className="rounded-app-sm border border-border bg-elev px-2.5 py-2 text-[12px] text-muted">
                Agents get <span className="font-mono text-text">read-names</span> +{' '}
                <span className="font-mono text-text">use</span>, never values directly. The command
                allowlist starts deny-all.
              </div>
            )}
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Expires in</span>
              <select
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
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
              </select>
            </label>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Project</span>
              <select
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
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
              </select>
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">
                Environment glob
              </span>
              <input
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
                placeholder="dev or *"
                value={environment}
                onChange={(event) => {
                  setEnvironment(event.target.value)
                }}
              />
            </label>
          </div>
          <p className="rounded-app-sm bg-elev px-2.5 py-2 text-[12px] text-muted">
            Agents are denied staging and production by default. After creating the identity, use{' '}
            <span className="font-mono text-text">Commands</span> or{' '}
            <span className="font-mono text-text">clavis agents allow</span> to approve commands.
          </p>
          {error && (
            <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger">
              {error}
            </p>
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
      </div>
    </div>
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="w-full max-w-lg rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[18px] font-semibold">Command allowlist</h2>
            <p className="mt-0.5 text-[13px] text-muted">
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
          className="mt-4 w-full rounded-app-sm border border-border bg-bg px-3 py-2 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
          placeholder={'npm test\ngo test ./...\npnpm dev'}
          value={commands}
          onChange={(event) => {
            setCommands(event.target.value)
          }}
        />
        <p className="mt-2 text-[12px] text-muted">
          Patterns support <span className="font-mono">*</span> and{' '}
          <span className="font-mono">?</span>. A bare <span className="font-mono">*</span> is
          rejected. Commands are checked before values are decrypted and every denial is audited.
        </p>
        {error && (
          <p className="mt-2 rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger">
            {error}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" size="sm" disabled={busy} onClick={() => void save()}>
            {busy && <Loader2 className="size-3.5 animate-spin" />} Save allowlist
          </Button>
        </div>
      </div>
    </div>
  )
}
