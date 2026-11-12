import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft, KeyRound, Loader2, Plus, ShieldOff, X } from 'lucide-react'
import { useMemo, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type Lease = components['schemas']['LeaseBody']

export function DynamicPage() {
  const { org } = useCurrentOrg()
  const params = useParams({ strict: false }) as { project?: string }
  const projectSlug = params.project ?? ''
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [selected, setSelected] = useState('')
  const [credentials, setCredentials] = useState<{
    lease: Lease
    secrets: Record<string, string>
  } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [now] = useState(() => Date.now())

  const configs = useQuery({
    queryKey: ['dynamic-configs', org?.slug, projectSlug],
    enabled: Boolean(org && projectSlug),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}/dynamic-configs', {
        params: { path: { org: org.slug, project: projectSlug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load configs.'))
      return result.data?.configs ?? []
    },
  })

  const selectedID = selected || configs.data?.[0]?.id || ''
  const leases = useQuery({
    queryKey: ['leases', org?.slug, projectSlug, selectedID],
    enabled: Boolean(org && projectSlug && selectedID),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/dynamic-configs/{id}/leases',
        {
          params: { path: { org: org.slug, project: projectSlug, id: selectedID } },
        },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load leases.'))
      return result.data?.leases ?? []
    },
  })

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['dynamic-configs', org?.slug, projectSlug] })
    await queryClient.invalidateQueries({
      queryKey: ['leases', org?.slug, projectSlug, selectedID],
    })
  }

  const createLease = useMutation({
    mutationFn: async () => {
      if (!org || !selectedID) throw new Error('Choose a config.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/dynamic-configs/{id}/leases',
        {
          params: { path: { org: org.slug, project: projectSlug, id: selectedID } },
          body: {},
        },
      )
      if (result.error || !result.data)
        throw new Error(apiErrorMessage(result.error, 'Could not create a lease.'))
      return { lease: result.data.lease, secrets: result.data.secrets ?? {} }
    },
    onSuccess: async (data) => {
      setCredentials(data)
      setError(null)
      await invalidate()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not create a lease.')
    },
  })

  const revoke = useMutation({
    mutationFn: async (lease: Lease) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/leases/{id}/revoke', {
        params: { path: { org: org.slug, project: projectSlug, id: lease.id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not revoke the lease.'))
    },
    onSuccess: invalidate,
  })

  const selectedConfig = useMemo(
    () => configs.data?.find((config) => config.id === selectedID),
    [configs.data, selectedID],
  )

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
            <h1 className="font-head text-[25px] font-semibold tracking-tight">Dynamic secrets</h1>
            <p className="mt-1 text-[14px] text-muted">
              Issue short-lived PostgreSQL credentials. Revoke them manually, on expiry, or when an
              identity is deleted.
            </p>
          </div>
          <Button
            variant="secondary"
            size="md"
            onClick={() => {
              setCreating(true)
            }}
          >
            <Plus className="size-4" /> New config
          </Button>
        </div>
      </div>

      {error && (
        <p className="rounded-app bg-danger-bg px-3 py-2 text-[13px] text-danger">{error}</p>
      )}

      <section className="flex flex-wrap items-end gap-3 rounded-app border border-border bg-surface px-4 py-3">
        <label className="block">
          <span className="mb-1 block text-[12.5px] font-medium text-muted">Config</span>
          <select
            className="h-9 min-w-48 rounded-app-sm border border-border bg-bg px-2 text-[13px] text-text"
            value={selectedID}
            onChange={(event) => {
              setSelected(event.target.value)
            }}
          >
            {(configs.data ?? []).map((config) => (
              <option key={config.id} value={config.id}>
                {config.engine} · {config.id.slice(0, 8)}
              </option>
            ))}
          </select>
        </label>
        <span className="pb-2 text-[12px] text-dim">
          default {selectedConfig?.default_ttl_seconds ?? 0}s · max{' '}
          {selectedConfig?.max_ttl_seconds ?? 0}s
        </span>
        <Button
          variant="primary"
          size="sm"
          disabled={!selectedID || createLease.isPending}
          onClick={() => {
            createLease.mutate()
          }}
        >
          {createLease.isPending ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <KeyRound className="size-3.5" />
          )}{' '}
          Create lease
        </Button>
      </section>

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[640px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-[11.5px] uppercase tracking-[0.06em] text-dim">
              <th className="px-4 py-2.5 font-semibold">Lease</th>
              <th className="px-4 py-2.5 font-semibold">Identity</th>
              <th className="px-4 py-2.5 font-semibold">Expires</th>
              <th className="px-4 py-2.5 font-semibold">Status</th>
              <th className="px-4 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {(leases.data ?? []).map((lease) => {
              const status = lease.revoked_at
                ? 'revoked'
                : new Date(lease.expires_at).getTime() < now
                  ? 'expired'
                  : 'active'
              return (
                <tr key={lease.id} className="border-b border-border-soft last:border-0">
                  <td className="px-4 py-2 font-mono text-[12px]">{lease.id.slice(0, 8)}</td>
                  <td className="px-4 py-2 text-[12px] text-muted">
                    {lease.identity_id ? lease.identity_id.slice(0, 8) : '—'}
                  </td>
                  <td className="px-4 py-2 text-[12px] text-muted">
                    {new Date(lease.expires_at).toLocaleString()}
                  </td>
                  <td className="px-4 py-2 text-[12px] text-muted">{status}</td>
                  <td className="px-4 py-2 text-right">
                    {status === 'active' && (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={revoke.isPending}
                        onClick={() => {
                          revoke.mutate(lease)
                        }}
                      >
                        <ShieldOff className="size-3.5" /> Revoke
                      </Button>
                    )}
                  </td>
                </tr>
              )
            })}
            {(leases.data ?? []).length === 0 && !leases.isLoading && (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-[13px] text-muted">
                  No leases yet. Create one to issue credentials.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      {credentials && (
        <section className="rounded-app border border-accent-line bg-accent-soft p-4">
          <div className="flex items-start justify-between">
            <div>
              <h2 className="text-[14px] font-semibold text-accent-text">Credentials shown once</h2>
              <p className="text-[12px] text-muted">Lease {credentials.lease.id}</p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => {
                setCredentials(null)
              }}
              aria-label="Close"
            >
              <X className="size-4" />
            </Button>
          </div>
          <div className="mt-3 grid gap-1 font-mono text-[12.5px]">
            {Object.entries(credentials.secrets).map(([key, value]) => (
              <p key={key}>
                <span className="text-dim">{key}=</span>
                <span className="break-all text-text">{value}</span>
              </p>
            ))}
          </div>
        </section>
      )}

      {creating && (
        <CreateDynamicConfigDialog
          orgSlug={org?.slug ?? ''}
          projectSlug={projectSlug}
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

function CreateDynamicConfigDialog({
  orgSlug,
  projectSlug,
  onClose,
  onCreated,
}: {
  orgSlug: string
  projectSlug: string
  onClose: () => void
  onCreated: () => Promise<void>
}) {
  const [host, setHost] = useState('localhost')
  const [port, setPort] = useState(5432)
  const [database, setDatabase] = useState('postgres')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [grantRole, setGrantRole] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/dynamic-configs', {
      params: { path: { org: orgSlug, project: projectSlug } },
      body: {
        engine: 'postgres',
        config: {
          host,
          port,
          database,
          username,
          password,
          sslmode: 'disable',
          grant_role: grantRole,
        },
      },
    })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not create the config.'))
      return
    }
    await onCreated()
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="w-full max-w-lg rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[18px] font-semibold">New PostgreSQL config</h2>
            <p className="mt-0.5 text-[13px] text-muted">
              Clavis connects with these admin credentials to create a role for each lease. Role
              grants, TTL, and revocation are managed by the lease.
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <X className="size-4" />
          </Button>
        </div>
        <div className="mt-4 grid grid-cols-2 gap-3">
          {[
            ['Host', host, setHost],
            ['Database', database, setDatabase],
            ['Admin user', username, setUsername],
            ['Admin password', password, setPassword],
            ['Grant role', grantRole, setGrantRole],
          ].map(([label, value, setter]) => (
            <label key={String(label)} className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">
                {String(label)}
              </span>
              <input
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px] text-text"
                type={label === 'Admin password' ? 'password' : 'text'}
                value={String(value)}
                onChange={(event) => {
                  ;(setter as (value: string) => void)(event.target.value)
                }}
              />
            </label>
          ))}
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Port</span>
            <input
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13px] text-text"
              type="number"
              value={port}
              onChange={(event) => {
                setPort(Number(event.target.value))
              }}
            />
          </label>
        </div>
        {error && <p className="mt-2 text-[12.5px] text-danger">{error}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" size="sm" disabled={busy} onClick={() => void submit()}>
            {busy && <Loader2 className="size-3.5 animate-spin" />} Create config
          </Button>
        </div>
      </div>
    </div>
  )
}
