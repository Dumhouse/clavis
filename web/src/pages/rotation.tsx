import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft, Loader2, Play, Plus, Trash2, X } from 'lucide-react'
import { useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type Env = components['schemas']['EnvBody']
type Policy = components['schemas']['RotationPolicyBody']
type Run = components['schemas']['RotationRunBody']
type Status = components['schemas']['RotationStatusOutputBody']

export function RotationPage() {
  const { org } = useCurrentOrg()
  const params = useParams({ strict: false }) as { project?: string }
  const projectSlug = params.project ?? ''
  const queryClient = useQueryClient()
  const [envSlug, setEnvSlug] = useState('')
  const [creating, setCreating] = useState(false)
  const [runsFor, setRunsFor] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const project = useQuery({
    queryKey: ['project', org?.slug, projectSlug],
    enabled: Boolean(org && projectSlug),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}', {
        params: { path: { org: org.slug, project: projectSlug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load the project.'))
      return result.data
    },
  })
  const envs = project.data?.environments ?? []
  const selected = envSlug || envs[0]?.slug || ''

  const policies = useQuery({
    queryKey: ['rotations', org?.slug, projectSlug, selected],
    enabled: Boolean(org && projectSlug && selected),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/rotations',
        { params: { path: { org: org.slug, project: projectSlug, env: selected } } },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load policies.'))
      return result.data?.policies ?? []
    },
  })

  const status = useQuery({
    queryKey: ['rotation-status', org?.slug, projectSlug, selected],
    enabled: Boolean(org && projectSlug && selected),
    queryFn: async (): Promise<Status> => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/rotations/status',
        {
          params: {
            path: { org: org.slug, project: projectSlug, env: selected },
            query: { stale_days: 90 },
          },
        },
      )
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load rotation status.'))
      return result.data as Status
    },
  })

  const runs = useQuery({
    queryKey: ['rotation-runs', org?.slug, projectSlug, selected, runsFor],
    enabled: Boolean(org && runsFor),
    queryFn: async () => {
      if (!org || !runsFor) return []
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/rotations/{id}/runs',
        { params: { path: { org: org.slug, project: projectSlug, env: selected, id: runsFor } } },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load runs.'))
      return result.data?.runs ?? []
    },
  })

  const invalidate = async () => {
    await queryClient.invalidateQueries({
      queryKey: ['rotations', org?.slug, projectSlug, selected],
    })
    await queryClient.invalidateQueries({ queryKey: ['secrets', org?.slug, projectSlug] })
  }

  const run = useMutation({
    mutationFn: async (policy: Policy) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/rotations/{id}/run',
        { params: { path: { org: org.slug, project: projectSlug, env: selected, id: policy.id } } },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Rotation failed.'))
      return result.data
    },
    onSuccess: invalidate,
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Rotation failed.')
    },
  })

  const toggle = useMutation({
    mutationFn: async (policy: Policy) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/rotations/{id}/enabled',
        {
          params: { path: { org: org.slug, project: projectSlug, env: selected, id: policy.id } },
          body: { enabled: !policy.enabled },
        },
      )
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not update the policy.'))
    },
    onSuccess: invalidate,
  })

  const remove = useMutation({
    mutationFn: async (policy: Policy) => {
      if (!org) throw new Error('No organization.')
      const result = await api.DELETE(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/rotations/{id}',
        { params: { path: { org: org.slug, project: projectSlug, env: selected, id: policy.id } } },
      )
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not delete the policy.'))
    },
    onSuccess: invalidate,
  })

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/projects/$project"
          params={{ project: projectSlug }}
          className="inline-flex items-center gap-1.5 text-[13px] text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> Secrets
        </Link>
        <div className="mt-2 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-head text-[25px] font-semibold tracking-tight">Rotation</h1>
            <p className="mt-1 text-[14px] text-muted">
              Two-phase rotation: create new credentials, write and verify the secret, then revoke
              the old credential.
            </p>
          </div>
          <Button
            variant="secondary"
            size="md"
            onClick={() => {
              setCreating(true)
            }}
          >
            <Plus className="size-4" /> New policy
          </Button>
        </div>
      </div>

      {error && (
        <p className="rounded-app bg-danger-bg px-3 py-2 text-[13px] text-danger">{error}</p>
      )}

      <section className="flex flex-wrap items-end gap-3 rounded-app border border-border bg-surface px-4 py-3">
        <label className="block">
          <span className="mb-1 block text-[12.5px] font-medium text-muted">Environment</span>
          <select
            className="h-9 rounded-app-sm border border-border bg-bg px-2 text-[13px] text-text"
            value={selected}
            onChange={(event) => {
              setEnvSlug(event.target.value)
            }}
          >
            {envs.map((env: Env) => (
              <option key={env.id} value={env.slug}>
                {env.name}
              </option>
            ))}
          </select>
        </label>
        <span className="pb-2 text-[12px] text-dim">
          {policies.data?.filter((policy) => policy.enabled).length ?? 0} enabled ·{' '}
          {policies.data?.length ?? 0} total
        </span>
      </section>

      <section className="rounded-app border border-border bg-surface p-4">
        <div className="flex flex-wrap items-center gap-3">
          <div>
            <h2 className="text-[14px] font-semibold">Manual rotation</h2>
            <p className="text-[12px] text-dim">
              Secrets without a policy. Reminders go to owner groups when they are 90+ days old.
            </p>
          </div>
          <span className="ml-auto text-[12px] text-muted">
            {status.data?.managed ?? 0} managed · {status.data?.unmanaged ?? 0} unmanaged ·{' '}
            {status.data?.stale?.length ?? 0} stale
          </span>
        </div>
        {(status.data?.stale ?? []).length > 0 && (
          <div className="mt-3 overflow-x-auto rounded-app-sm border border-border">
            <table className="w-full min-w-[640px] text-left text-[12.5px]">
              <thead>
                <tr className="border-b border-border text-[11px] uppercase tracking-[0.05em] text-dim">
                  <th className="px-3 py-2">Key</th>
                  <th className="px-3 py-2">Age</th>
                  <th className="px-3 py-2">Owner group</th>
                  <th className="px-3 py-2">Updated</th>
                </tr>
              </thead>
              <tbody>
                {(status.data?.stale ?? []).map((secret) => (
                  <tr key={secret.key} className="border-b border-border-soft last:border-0">
                    <td className="px-3 py-2 font-mono">{secret.key}</td>
                    <td className="px-3 py-2 text-amber-fg">{secret.age_days} days</td>
                    <td className="px-3 py-2 text-muted">{secret.owner_group_name || '—'}</td>
                    <td className="px-3 py-2 text-muted">
                      {new Date(secret.updated_at).toLocaleDateString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[820px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-[11.5px] uppercase tracking-[0.06em] text-dim">
              <th className="px-4 py-2.5 font-semibold">Key</th>
              <th className="px-4 py-2.5 font-semibold">Provider</th>
              <th className="px-4 py-2.5 font-semibold">Every</th>
              <th className="px-4 py-2.5 font-semibold">Next run</th>
              <th className="px-4 py-2.5 font-semibold">Last status</th>
              <th className="px-4 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {(policies.data ?? []).map((policy) => (
              <tr key={policy.id} className="border-b border-border-soft last:border-0">
                <td className="px-4 py-2 font-mono text-[12.5px]">{policy.key}</td>
                <td className="px-4 py-2 text-[12.5px] text-muted">{policy.provider}</td>
                <td className="px-4 py-2 text-[12px] text-muted">
                  {Math.round(policy.interval_seconds / 60)}m
                </td>
                <td className="px-4 py-2 text-[12px] text-muted">
                  {new Date(policy.next_run_at).toLocaleString()}
                </td>
                <td className="px-4 py-2 text-[12px]">
                  <span className="rounded bg-elev px-1.5 py-0.5 text-[11px] text-muted">
                    {policy.last_status || 'never'}
                  </span>
                  {policy.last_error && (
                    <p className="mt-1 max-w-52 text-[11px] text-danger">{policy.last_error}</p>
                  )}
                </td>
                <td className="px-4 py-2">
                  <div className="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setRunsFor(policy.id)
                      }}
                    >
                      Runs
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        toggle.mutate(policy)
                      }}
                    >
                      {policy.enabled ? 'Disable' : 'Enable'}
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={run.isPending}
                      onClick={() => {
                        run.mutate(policy)
                      }}
                    >
                      <Play className="size-3.5" /> Run
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Delete policy"
                      onClick={() => {
                        remove.mutate(policy)
                      }}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
            {(policies.data ?? []).length === 0 && !policies.isLoading && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-[13px] text-muted">
                  No rotation policies yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      {runsFor && (
        <section className="rounded-app border border-border bg-surface p-4">
          <div className="flex items-center justify-between">
            <h2 className="text-[14px] font-semibold">Run history</h2>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => {
                setRunsFor(null)
              }}
              aria-label="Close"
            >
              <X className="size-4" />
            </Button>
          </div>
          <div className="mt-2 divide-y divide-border-soft">
            {(runs.data ?? []).map((item: Run) => (
              <div key={item.id} className="flex flex-wrap items-center gap-3 py-2 text-[12.5px]">
                <span className="text-dim">{new Date(item.started_at).toLocaleString()}</span>
                <span className="font-medium">{item.status}</span>
                <span className="text-muted">{item.phase}</span>
                <span className="text-danger">{item.message}</span>
              </div>
            ))}
            {runs.isLoading && (
              <p className="py-3 text-[13px] text-muted">
                <Loader2 className="mr-2 inline size-3.5 animate-spin" /> Loading…
              </p>
            )}
            {(runs.data ?? []).length === 0 && !runs.isLoading && (
              <p className="py-3 text-[13px] text-muted">No runs yet.</p>
            )}
          </div>
        </section>
      )}

      {creating && (
        <CreateRotationDialog
          orgSlug={org?.slug ?? ''}
          projectSlug={projectSlug}
          env={selected}
          onClose={() => {
            setCreating(false)
          }}
          onCreated={async () => {
            setCreating(false)
            await invalidate()
          }}
        />
      )}
    </div>
  )
}

function CreateRotationDialog({
  orgSlug,
  projectSlug,
  env,
  onClose,
  onCreated,
}: {
  orgSlug: string
  projectSlug: string
  env: string
  onClose: () => void
  onCreated: () => Promise<void>
}) {
  const [key, setKey] = useState('')
  const [provider, setProvider] = useState('postgres')
  const [hours, setHours] = useState(24)
  const [host, setHost] = useState('localhost')
  const [port, setPort] = useState(5432)
  const [database, setDatabase] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState('')
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    const config =
      provider === 'postgres'
        ? { host, port, database, username, password, sslmode: 'disable', role }
        : provider === 'webhook'
          ? { url }
          : {}
    const result = await api.POST(
      '/api/v1/orgs/{org}/projects/{project}/environments/{env}/rotations',
      {
        params: { path: { org: orgSlug, project: projectSlug, env } },
        body: {
          key,
          provider: provider as 'postgres' | 'aws' | 'webhook',
          config,
          interval_seconds: Math.max(60, Math.round(hours * 3600)),
          enabled: true,
        },
      },
    )
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not create the policy.'))
      return
    }
    await onCreated()
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="w-full max-w-lg rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[18px] font-semibold">New rotation policy</h2>
            <p className="mt-0.5 text-[13px] text-muted">
              Two-phase rotation: create the new credential, write and verify the secret, then
              revoke the old one. A failed verification rolls back before the old credential is
              revoked.
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <X className="size-4" />
          </Button>
        </div>
        <div className="mt-4 grid grid-cols-2 gap-3">
          <label className="block">
            <span className="mb-1 block text-[12.5px] text-muted">Secret key</span>
            <input
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[13px]"
              value={key}
              onChange={(e) => {
                setKey(e.target.value)
              }}
              placeholder="DATABASE_PASSWORD"
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[12.5px] text-muted">Provider</span>
            <select
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px]"
              value={provider}
              onChange={(e) => {
                setProvider(e.target.value)
              }}
            >
              <option value="postgres">PostgreSQL</option>
              <option value="webhook">Webhook</option>
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-[12.5px] text-muted">Every (hours)</span>
            <input
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px]"
              type="number"
              value={hours}
              onChange={(e) => {
                setHours(Number(e.target.value))
              }}
            />
          </label>
          {provider === 'postgres' ? (
            <>
              <label className="block">
                <span className="mb-1 block text-[12.5px] text-muted">Host</span>
                <input
                  className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px]"
                  value={host}
                  onChange={(e) => {
                    setHost(e.target.value)
                  }}
                />
              </label>
              <label className="block">
                <span className="mb-1 block text-[12.5px] text-muted">Port</span>
                <input
                  className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px]"
                  type="number"
                  value={port}
                  onChange={(e) => {
                    setPort(Number(e.target.value))
                  }}
                />
              </label>
              <label className="block">
                <span className="mb-1 block text-[12.5px] text-muted">Database</span>
                <input
                  className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px]"
                  value={database}
                  onChange={(e) => {
                    setDatabase(e.target.value)
                  }}
                />
              </label>
              <label className="block">
                <span className="mb-1 block text-[12.5px] text-muted">Role</span>
                <input
                  className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px]"
                  value={role}
                  onChange={(e) => {
                    setRole(e.target.value)
                  }}
                />
              </label>
              <label className="block">
                <span className="mb-1 block text-[12.5px] text-muted">Admin user</span>
                <input
                  className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px]"
                  value={username}
                  onChange={(e) => {
                    setUsername(e.target.value)
                  }}
                />
              </label>
              <label className="block">
                <span className="mb-1 block text-[12.5px] text-muted">Admin password</span>
                <input
                  className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px]"
                  type="password"
                  value={password}
                  onChange={(e) => {
                    setPassword(e.target.value)
                  }}
                />
              </label>
            </>
          ) : (
            <label className="col-span-2 block">
              <span className="mb-1 block text-[12.5px] text-muted">Webhook URL</span>
              <input
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px]"
                value={url}
                onChange={(e) => {
                  setUrl(e.target.value)
                }}
              />
            </label>
          )}
        </div>
        {error && <p className="mt-2 text-[12.5px] text-danger">{error}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" size="sm" disabled={busy || !key} onClick={() => void submit()}>
            {busy && <Loader2 className="size-3.5 animate-spin" />} Create policy
          </Button>
        </div>
      </div>
    </div>
  )
}
