import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bot, Check, Copy, KeyRound, Loader2, RefreshCw, Server, Trash2, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { ensureReauth } from '@/lib/reauth'

type Identity = components['schemas']['IdentityBody']
type Project = components['schemas']['ProjectBody']

const ROLES = ['Owner', 'Admin', 'Developer', 'Viewer'] as const

export function IdentitiesPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [token, setToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

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
        <h1 className="font-head text-[20px] font-semibold">Identities</h1>
        <p className="mt-1 text-[12.5px] text-muted">
          You need the Admin or Owner role to manage identities.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="font-head text-[24px] font-semibold tracking-tight">Identities</h1>
          <p className="mt-1 text-[13px] text-muted">
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
              <p className="text-[12px] font-semibold text-accent-text">Copy this token now</p>
              <p className="mt-0.5 text-[11.5px] text-muted">
                It is shown once and cannot be retrieved. Store it in your CI secret store.
              </p>
              <code className="mt-2 block break-all rounded-app-sm border border-border bg-bg px-2.5 py-1.5 font-mono text-[11px] text-text">
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
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-[12px] text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading identities…
        </div>
      )}
      {identities.isError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-[12px] text-danger">
          {identities.error instanceof Error
            ? identities.error.message
            : 'Could not load identities.'}
        </div>
      )}

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[760px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-[10.5px] uppercase tracking-[0.06em] text-dim">
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
                        <p className="text-[12px] font-medium">{identity.name}</p>
                        <p className="text-[11px] text-dim">{identity.type}</p>
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-3 text-[11.5px]">
                    <p className="text-text">{identity.role}</p>
                    <p className="text-[11px] text-dim">
                      {identity.project_id ? 'one project' : 'all projects'} ·{' '}
                      {identity.environment || '*'}
                    </p>
                  </td>
                  <td className="px-4 py-3 font-mono text-[10.5px] text-dim">
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
                  <td className="px-4 py-3 text-[11px] text-dim">
                    {activeToken?.last_used_at ? formatDate(activeToken.last_used_at) : 'never'}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end gap-1">
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
                <td colSpan={5} className="px-4 py-10 text-center text-[12px] text-muted">
                  No identities yet. Create one for CI, Kubernetes, or an agent.
                </td>
              </tr>
            )}
          </tbody>
        </table>
        {(rotate.error || revoke.error) && (
          <p className="px-4 py-3 text-[11.5px] text-danger">
            {rotate.error instanceof Error ? rotate.error.message : ''}
            {revoke.error instanceof Error ? revoke.error.message : ''}
          </p>
        )}
      </section>

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
        role: role as (typeof ROLES)[number],
        project,
        environment,
        expires_in_days: days,
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
            <h2 className="font-head text-[17px] font-semibold">New identity</h2>
            <p className="mt-0.5 text-[12px] text-muted">
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
              <span className="mb-1 block text-[11.5px] font-medium text-muted">Name</span>
              <input
                autoFocus
                required
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
                placeholder="ci-runner"
                value={name}
                onChange={(event) => {
                  setName(event.target.value)
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-[11.5px] font-medium text-muted">Type</span>
              <select
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
                value={type}
                onChange={(event) => {
                  setType(event.target.value)
                }}
              >
                <option value="machine">Machine</option>
                <option value="agent">Agent</option>
              </select>
            </label>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1 block text-[11.5px] font-medium text-muted">Role</span>
              <select
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
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
            <label className="block">
              <span className="mb-1 block text-[11.5px] font-medium text-muted">Expires in</span>
              <select
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
                value={days}
                onChange={(event) => {
                  setDays(Number(event.target.value))
                }}
              >
                <option value={30}>30 days</option>
                <option value={90}>90 days</option>
                <option value={180}>180 days</option>
                <option value={365}>365 days</option>
              </select>
            </label>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1 block text-[11.5px] font-medium text-muted">Project</span>
              <select
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
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
              <span className="mb-1 block text-[11.5px] font-medium text-muted">
                Environment glob
              </span>
              <input
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[12px] text-text focus:border-accent-line focus:outline-none"
                placeholder="dev or *"
                value={environment}
                onChange={(event) => {
                  setEnvironment(event.target.value)
                }}
              />
            </label>
          </div>
          <p className="rounded-app-sm bg-elev px-2.5 py-2 text-[11px] text-muted">
            Agents should normally use <span className="font-mono text-text">dev</span> or{' '}
            <span className="font-mono text-text">preview</span>. Command allowlists arrive with
            agent-safe mode.
          </p>
          {error && (
            <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[11.5px] text-danger">
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
