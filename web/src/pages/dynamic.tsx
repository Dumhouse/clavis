import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft, KeyRound, Loader2, Plus, ShieldOff, X } from 'lucide-react'
import { useMemo, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Cmd, PageIntro } from '@/components/page-intro'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { Input, Select } from '@/components/ui/field'
import { formatDateTime } from '@/lib/format'

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
          className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> Secrets
        </Link>
        <div className="mt-2 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-head text-2xl font-semibold tracking-tight">Dynamic secrets</h1>
            <p className="mt-1 text-base text-muted">
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

      <PageIntro
        id="dynamic-secrets"
        summary={
          <>
            Instead of one shared database password, each person or job gets its own temporary
            PostgreSQL login. It is deleted automatically when it expires.
          </>
        }
        steps={[
          {
            title: 'Connect a database once',
            body: 'Clavis gets an admin connection that it uses only to create and remove temporary users.',
          },
          {
            title: 'Ask for credentials',
            body: 'Each request creates a new login with a short lifetime (a lease). The password is shown once.',
          },
          {
            title: 'They clean up after themselves',
            body: 'Leases expire on their own. Revoke one early if it leaks. Every request is in the audit log.',
          },
        ]}
        when="people need database access for debugging, or a CI job needs a throwaway login."
        footer={
          <p className="text-xs text-muted">
            Terminal: <Cmd>clavis lease run CONFIG_ID -- psql</Cmd> opens psql with a fresh login
            that is revoked when you exit.
          </p>
        }
      />

      {error && <p className="rounded-app bg-danger-bg px-3 py-2 text-sm text-danger">{error}</p>}

      <section className="flex flex-wrap items-end gap-3 rounded-app border border-border bg-surface px-4 py-3">
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Config</span>
          <Select
            className="h-9 min-w-48"
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
          </Select>
        </label>
        <span className="pb-2 text-xs text-dim">
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
            <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
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
                  <td className="px-4 py-2 font-mono text-xs">{lease.id.slice(0, 8)}</td>
                  <td className="px-4 py-2 text-xs text-muted">
                    {lease.identity_id ? lease.identity_id.slice(0, 8) : '—'}
                  </td>
                  <td className="px-4 py-2 text-xs text-muted">
                    {formatDateTime(lease.expires_at)}
                  </td>
                  <td className="px-4 py-2 text-xs text-muted">{status}</td>
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
                <td colSpan={5} className="px-4 py-8 text-center text-sm text-muted">
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
              <h2 className="text-base font-semibold text-accent-text">Credentials shown once</h2>
              <p className="text-xs text-muted">Lease {credentials.lease.id}</p>
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
          <div className="mt-3 grid gap-1 font-mono text-xs">
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
    <Modal onClose={onClose} className="max-w-lg">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="font-head text-lg font-semibold">New PostgreSQL config</h2>
          <p className="mt-0.5 text-sm text-muted">
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
            <span className="mb-1 block text-xs font-medium text-muted">{String(label)}</span>
            <Input
              className="h-9"
              type={label === 'Admin password' ? 'password' : 'text'}
              value={String(value)}
              onChange={(event) => {
                ;(setter as (value: string) => void)(event.target.value)
              }}
            />
          </label>
        ))}
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Port</span>
          <Input
            className="h-9"
            type="number"
            value={port}
            onChange={(event) => {
              setPort(Number(event.target.value))
            }}
          />
        </label>
      </div>
      {error && <p className="mt-2 text-xs text-danger">{error}</p>}
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button variant="primary" size="sm" disabled={busy} onClick={() => void submit()}>
          {busy && <Loader2 className="size-3.5 animate-spin" />} Create config
        </Button>
      </div>
    </Modal>
  )
}
