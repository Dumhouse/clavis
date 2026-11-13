import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  Cable,
  Loader2,
  Play,
  Plus,
  RefreshCw,
  ShieldAlert,
  Trash2,
  X,
} from 'lucide-react'
import { useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Field, Notice, Select } from '@/components/ui/field'
import { Modal } from '@/components/ui/modal'
import { toast } from '@/components/ui/toast'
import { lastProject } from '@/lib/last-project'
import {
  CreateSyncDialog,
  ProviderMark,
  providers,
  type Provider,
} from '@/pages/integration-create'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { confirmAction } from '@/components/ui/dialogs'

type Project = components['schemas']['ProjectBody']
type Sync = components['schemas']['SyncBody']
type Run = components['schemas']['SyncRunBody']

export function IntegrationsPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [projectSlug, setProjectSlug] = useState('')
  const [selected, setSelected] = useState<Sync | null>(null)
  const [creating, setCreating] = useState<Provider | null>(null)
  const setNotice = (message: string | null) => {
    if (message) toast(message, { tone: 'info' })
  }

  const projects = useQuery({
    queryKey: ['projects', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load projects.'))
      return result.data?.projects ?? []
    },
  })

  const remembered = lastProject()
  const project =
    projectSlug ||
    projects.data?.find((item) => item.slug === remembered)?.slug ||
    projects.data?.[0]?.slug ||
    ''

  const projectDetail = useQuery({
    queryKey: ['project', org?.slug, project],
    enabled: Boolean(org && project),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}', {
        params: { path: { org: org.slug, project } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load the project.'))
      return result.data
    },
  })

  const syncs = useQuery({
    queryKey: ['syncs', org?.slug, project],
    enabled: Boolean(org && project),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}/syncs', {
        params: { path: { org: org.slug, project } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load syncs.'))
      return result.data?.syncs ?? []
    },
  })

  const runs = useQuery({
    queryKey: ['sync-runs', org?.slug, project, selected?.id],
    enabled: Boolean(org && project && selected),
    queryFn: async () => {
      if (!org || !selected) throw new Error('No sync selected.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}/syncs/{id}/runs', {
        params: { path: { org: org.slug, project, id: selected.id } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load runs.'))
      return result.data?.runs ?? []
    },
  })

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['syncs', org?.slug, project] })
    await queryClient.invalidateQueries({
      queryKey: ['sync-runs', org?.slug, project, selected?.id],
    })
  }

  const run = useMutation({
    mutationFn: async (input: { sync: Sync; confirm: boolean }) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/syncs/{id}/run', {
        params: { path: { org: org.slug, project, id: input.sync.id } },
        body: input.confirm ? { confirm: true } : { dry_run: true },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not run the sync.'))
      return result.data
    },
    onSuccess: async (data, input) => {
      // A failed push is still a 200: the run records the provider's error.
      if (data?.run?.status === 'failed') {
        toast(`Sync failed: ${data.run.error || 'the provider returned no details.'}`, {
          tone: 'error',
        })
        setSelected(input.sync)
      } else {
        setNotice(
          input.confirm
            ? `Sync ${data?.run?.status ?? 'started'} with ${String(data?.run?.secret_count ?? 0)} keys.`
            : `Preview: ${String(data?.preview?.count ?? 0)} keys → ${data?.preview?.destination ?? ''}`,
        )
      }
      await invalidate()
    },
    onError: (cause: unknown) => {
      setNotice(cause instanceof Error ? cause.message : 'Could not run the sync.')
    },
  })

  const drift = useMutation({
    mutationFn: async (sync: Sync) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/syncs/{id}/drift', {
        params: { path: { org: org.slug, project, id: sync.id } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not check drift.'))
      return result.data
    },
    onSuccess: async (data) => {
      setNotice(
        data?.supported
          ? (data.keys ?? []).length > 0
            ? `Drift: ${(data.keys ?? []).join(', ')}`
            : 'No drift detected.'
          : (data?.detail ?? 'Drift detection is unavailable.'),
      )
      await invalidate()
    },
    onError: (cause: unknown) => {
      setNotice(cause instanceof Error ? cause.message : 'Could not check drift.')
    },
  })

  const toggleDelete = useMutation({
    mutationFn: async (input: { sync: Sync; value: boolean }) => {
      if (!org) throw new Error('No organization.')
      const result = await api.PATCH('/api/v1/orgs/{org}/projects/{project}/syncs/{id}', {
        params: { path: { org: org.slug, project, id: input.sync.id } },
        body: { delete_missing: input.value },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not update the sync.'))
    },
    onSuccess: invalidate,
    onError: (cause: unknown) => {
      setNotice(cause instanceof Error ? cause.message : 'Could not update the sync.')
    },
  })

  const remove = useMutation({
    mutationFn: async (sync: Sync) => {
      if (!org) throw new Error('No organization.')
      if (!(await confirmAction('Delete this sync? The destination keeps its current values.')))
        return
      const result = await api.DELETE('/api/v1/orgs/{org}/projects/{project}/syncs/{id}', {
        params: { path: { org: org.slug, project, id: sync.id } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not delete the sync.'))
    },
    onSuccess: async () => {
      setSelected(null)
      await invalidate()
    },
    onError: (cause: unknown) => {
      setNotice(cause instanceof Error ? cause.message : 'Could not delete the sync.')
    },
  })

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <Cable className="size-5 text-muted" />
            <h1 className="font-head text-2xl font-semibold tracking-tight">Integrations</h1>
          </div>
          <p className="mt-1 text-base text-muted">
            Syncs push resolved secrets to GitHub, GitLab, Forgejo, Vercel, AWS, GCP, Netlify,
            Cloudflare, Vault, or a signed HTTP endpoint.
          </p>
        </div>
        <Field label="Project">
          <Select
            className="min-w-44"
            value={project}
            onChange={(event) => {
              setProjectSlug(event.target.value)
              setSelected(null)
            }}
          >
            {(projects.data ?? []).map((item: Project) => (
              <option key={item.id} value={item.slug}>
                {item.name}
              </option>
            ))}
          </Select>
        </Field>
      </header>

      <section className="rounded-app border border-border bg-surface">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <h2 className="text-base font-semibold">Syncs</h2>
          <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">
            {syncs.data?.length ?? 0}
          </span>
        </div>
        <div className="divide-y divide-border-soft">
          {(syncs.data ?? []).map((sync) => {
            const driftKeys = sync.drift_keys ?? []
            return (
              <div key={sync.id} className="px-4 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  {(() => {
                    const meta = providers.find((candidate) => candidate.id === sync.provider)
                    return meta ? (
                      <>
                        <ProviderMark provider={meta} size="sm" />
                        <span className="text-sm font-medium">{meta.name}</span>
                      </>
                    ) : (
                      <span className="text-sm font-medium">{sync.provider}</span>
                    )
                  })()}
                  <span className="text-dim">←</span>
                  <span className="font-mono text-xs text-muted">{sync.environment}</span>
                  <button
                    type="button"
                    title="Show run history"
                    onClick={() => {
                      setSelected(sync)
                    }}
                    className={
                      sync.status === 'success'
                        ? 'rounded-full bg-success-bg px-1.5 py-0.5 text-2xs font-semibold text-success'
                        : sync.status === 'failed'
                          ? 'rounded-full bg-danger-bg px-1.5 py-0.5 text-2xs font-semibold text-danger'
                          : 'rounded-full bg-elev px-1.5 py-0.5 text-2xs font-semibold text-muted'
                    }
                  >
                    {sync.status}
                  </button>
                  {!sync.confirmed && (
                    <span className="rounded-full bg-amber-bg px-1.5 py-0.5 text-2xs font-semibold text-amber-fg">
                      first run unconfirmed
                    </span>
                  )}
                  {driftKeys.length > 0 && (
                    <span className="inline-flex items-center gap-1 rounded-full bg-danger-bg px-1.5 py-0.5 text-2xs font-semibold text-danger">
                      <ShieldAlert className="size-3" /> drift: {driftKeys.join(', ')}
                    </span>
                  )}
                  <span className="ml-auto flex items-center gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setSelected(sync)
                      }}
                    >
                      Runs
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={drift.isPending}
                      onClick={() => {
                        drift.mutate(sync)
                      }}
                    >
                      <RefreshCw className="size-3.5" /> Drift
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={run.isPending}
                      onClick={() => {
                        run.mutate({ sync, confirm: false })
                      }}
                    >
                      Preview
                    </Button>
                    <Button
                      variant="primary"
                      size="sm"
                      disabled={run.isPending}
                      onClick={() => {
                        run.mutate({ sync, confirm: true })
                      }}
                    >
                      <Play className="size-3.5" /> Run
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Delete sync"
                      disabled={remove.isPending}
                      onClick={() => {
                        remove.mutate(sync)
                      }}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </span>
                </div>
                <label className="mt-1 flex items-center gap-1.5 text-2xs text-dim">
                  <input
                    type="checkbox"
                    checked={sync.delete_missing}
                    disabled={toggleDelete.isPending}
                    onChange={(event) => {
                      toggleDelete.mutate({ sync, value: event.target.checked })
                    }}
                  />
                  Delete remote keys not in Clavis
                </label>
                <p className="mt-1 text-2xs text-dim">
                  Last synced{' '}
                  {sync.last_synced_at ? new Date(sync.last_synced_at).toLocaleString() : 'never'} ·
                  drift checked{' '}
                  {sync.drift_checked_at
                    ? new Date(sync.drift_checked_at).toLocaleString()
                    : 'never'}
                </p>
              </div>
            )
          })}
          {syncs.isLoading && (
            <p className="flex items-center gap-2 px-4 py-5 text-sm text-muted">
              <Loader2 className="size-4 animate-spin" /> Loading syncs…
            </p>
          )}
          {!syncs.isLoading && (syncs.data?.length ?? 0) === 0 && (
            <p className="px-4 py-6 text-sm text-muted">
              No syncs for this project yet. Pick a destination below.
            </p>
          )}
        </div>
      </section>

      <section aria-labelledby="add-integration">
        <h2 id="add-integration" className="mb-2 text-base font-semibold">
          Add an integration
        </h2>
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {providers.map((provider) => (
            <button
              key={provider.id}
              type="button"
              disabled={!project || (projectDetail.data?.environments ?? []).length === 0}
              onClick={() => {
                setCreating(provider)
              }}
              className="group flex items-start gap-3 rounded-app border border-border bg-surface p-3 text-left transition-colors hover:border-accent-line hover:bg-hover-soft disabled:opacity-60"
            >
              <ProviderMark provider={provider} />
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium text-text">{provider.name}</span>
                <span className="block text-xs text-muted">{provider.blurb}</span>
              </span>
              <Plus className="size-4 text-dim group-hover:text-accent-text" />
            </button>
          ))}
          <Link
            to="/docs"
            className="group flex items-start gap-3 rounded-app border border-dashed border-border p-3 text-left hover:border-accent-line"
          >
            <span className="flex size-9 shrink-0 items-center justify-center rounded-app-sm bg-elev font-mono text-2xs font-bold text-blue-fg">
              K8s
            </span>
            <span className="min-w-0 flex-1">
              <span className="block text-sm font-medium text-text">Kubernetes</span>
              <span className="block text-xs text-muted">
                Operator, External Secrets, or the CSI driver. Installed with Helm.
              </span>
            </span>
            <ArrowRight className="size-4 text-dim group-hover:text-accent-text" />
          </Link>
        </div>
      </section>

      {selected && (
        <Modal
          variant="sheet"
          className="max-w-lg"
          onClose={() => {
            setSelected(null)
          }}
        >
          <div className="flex items-center gap-2 border-b border-border px-5 py-4">
            <h2 className="font-head text-md font-semibold">Run history</h2>
            <span className="font-mono text-2xs text-dim">
              {selected.provider} ← {selected.environment}
            </span>
            <Button
              variant="ghost"
              size="icon"
              className="ml-auto"
              aria-label="Close"
              onClick={() => {
                setSelected(null)
              }}
            >
              <X className="size-4" />
            </Button>
          </div>
          <div className="flex-1 divide-y divide-border-soft overflow-y-auto">
            {(runs.data ?? []).map((item: Run) => (
              <div key={item.id} className="px-4 py-2">
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <span
                    className={
                      item.status === 'success'
                        ? 'font-semibold text-success'
                        : item.status === 'failed'
                          ? 'font-semibold text-danger'
                          : 'text-muted'
                    }
                  >
                    {item.status}
                  </span>
                  <span className="text-dim">
                    {item.dry_run ? 'dry run' : `${String(item.secret_count)} keys`}
                  </span>
                  <span className="ml-auto text-dim">
                    {new Date(item.started_at).toLocaleString()}
                  </span>
                </div>
                {item.error && <Notice className="mt-1">{item.error}</Notice>}
              </div>
            ))}
            {runs.isLoading && (
              <p className="flex items-center gap-2 px-4 py-4 text-sm text-muted">
                <Loader2 className="size-4 animate-spin" /> Loading runs…
              </p>
            )}
            {!runs.isLoading && (runs.data?.length ?? 0) === 0 && (
              <p className="px-4 py-4 text-sm text-muted">No runs yet.</p>
            )}
          </div>
        </Modal>
      )}
      {creating && org && (
        <CreateSyncDialog
          orgSlug={org.slug}
          projectSlug={project}
          envs={projectDetail.data?.environments ?? []}
          provider={creating}
          onClose={() => {
            setCreating(null)
          }}
          onCreated={() => {
            setCreating(null)
            toast(`Sync to ${creating.name} created. Run a preview before the first push.`)
            void invalidate()
          }}
        />
      )}
    </div>
  )
}
