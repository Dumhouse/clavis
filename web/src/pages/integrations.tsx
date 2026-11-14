import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Cable,
  CircleAlert,
  CloudDownload,
  KeyRound,
  Send,
  Sparkles,
  Loader2,
  Play,
  Plus,
  RefreshCw,
  ShieldAlert,
  Trash2,
  X,
} from 'lucide-react'
import { useState, type ReactNode } from 'react'

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
import { ConnectionsSection, useConnections } from '@/pages/integration-connections'
import { CiAccessSection } from '@/pages/integration-ci'
import { SetupGuide } from '@/pages/integration-guide'
import { platforms, type Method } from '@/lib/integration-guide'
import { cn } from '@/lib/utils'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { confirmAction, promptDialog } from '@/components/ui/dialogs'
import { formatDateTime } from '@/lib/format'

type Project = components['schemas']['ProjectBody']
type Sync = components['schemas']['SyncBody']
type Run = components['schemas']['SyncRunBody']
type Tab = 'syncs' | 'ci' | 'credentials'

function WayCard({
  icon,
  title,
  text,
  active,
  onClick,
}: {
  icon: ReactNode
  title: string
  text: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'rounded-app border p-3 text-left transition-colors',
        active
          ? 'border-accent-line bg-accent-soft/40'
          : 'border-border bg-surface hover:bg-hover-soft',
      )}
    >
      <span className="flex items-center gap-2 text-sm font-semibold">
        {icon}
        {title}
      </span>
      <span className="mt-1 block text-xs text-muted">{text}</span>
    </button>
  )
}

export function IntegrationsPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [projectSlug, setProjectSlug] = useState('')
  const [selected, setSelected] = useState<Sync | null>(null)
  const [creating, setCreating] = useState<Provider | null>(null)
  const [guide, setGuide] = useState<{ platform?: string | undefined; method?: Method } | null>(
    null,
  )
  const [tab, setTabState] = useState<Tab>(() => {
    const saved = window.localStorage.getItem('clavis.integrations.tab')
    return saved === 'syncs' || saved === 'credentials' ? saved : 'ci'
  })
  const setTab = (next: Tab) => {
    setTabState(next)
    window.localStorage.setItem('clavis.integrations.tab', next)
  }
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

  const connections = useConnections(org?.slug)

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['syncs', org?.slug, project] })
    await queryClient.invalidateQueries({ queryKey: ['connections', org?.slug] })
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

  // Migration path: turn a sync's own credential into a reusable connection.
  const share = useMutation({
    mutationFn: async (sync: Sync) => {
      if (!org) throw new Error('No organization.')
      const meta = providers.find((candidate) => candidate.id === sync.provider)
      const name = await promptDialog({
        title: 'Save this token as a platform token?',
        body: 'Other syncs can then pick it instead of asking for the token again. Rotating it later updates every sync that uses it.',
        label: 'Name',
        placeholder: meta?.name ?? sync.provider,
        confirmLabel: 'Save',
      })
      if (name === null) return null
      const result = await api.PUT('/api/v1/orgs/{org}/projects/{project}/syncs/{id}/connection', {
        params: { path: { org: org.slug, project, id: sync.id } },
        body: { name: name.trim() || (meta?.name ?? sync.provider) },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not save the platform token.'))
      return result.data
    },
    onSuccess: async (data) => {
      if (data) toast(`This sync now uses the connection “${data.connection.name}”.`)
      await invalidate()
    },
    onError: (cause: unknown) => {
      toast(cause instanceof Error ? cause.message : 'Could not save the platform token.', {
        tone: 'error',
      })
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

  // Forgejo and GitLab keep one value per name: two syncs that push different
  // environments to the same repository overwrite each other.
  const clash = (sync: Sync) => {
    if (!['forgejo', 'gitlab'].includes(sync.provider) || !sync.target) return ''
    const other = (syncs.data ?? []).find(
      (candidate) =>
        candidate.id !== sync.id &&
        candidate.provider === sync.provider &&
        candidate.target === sync.target &&
        candidate.environment !== sync.environment,
    )
    return other?.environment ?? ''
  }

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <Cable className="size-5 text-muted" />
            <h1 className="font-head text-2xl font-semibold tracking-tight">Integrations</h1>
          </div>
          <p className="mt-1 max-w-2xl text-base text-muted">
            Get this project’s secrets to where they are used: let CI jobs fetch them when they run,
            or push copies into a platform with a sync.
          </p>
        </div>
        <div className="flex items-end gap-2">
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
          <Button
            variant="primary"
            disabled={!project || (projectDetail.data?.environments ?? []).length === 0}
            onClick={() => {
              setGuide({})
            }}
          >
            <Sparkles className="size-4" /> Set up an integration
          </Button>
        </div>
      </header>

      <div className="grid gap-2 md:grid-cols-2">
        <WayCard
          icon={<CloudDownload className="size-4 text-accent-text" />}
          title="Fetch when the job runs (CI access)"
          text="The job asks Clavis for one environment with a Clavis machine token. Best for Forgejo, Gitea, GitHub and GitLab pipelines: same names in every environment, nothing copied, every read audited."
          active={tab === 'ci'}
          onClick={() => {
            setTab('ci')
          }}
        />
        <WayCard
          icon={<Send className="size-4 text-muted" />}
          title="Push with a sync"
          text="Clavis copies one environment into a platform’s own secret store, using that platform’s access token (a platform token). Best for Vercel, Netlify, Cloudflare, AWS, GCP and Vault."
          active={tab === 'syncs'}
          onClick={() => {
            setTab('syncs')
          }}
        />
      </div>

      <div role="tablist" aria-label="Integrations" className="flex gap-1 border-b border-border">
        {(
          [
            ['ci', 'CI access (fetch)', undefined],
            ['syncs', 'Syncs (push)', syncs.data?.length],
            ['credentials', 'Platform tokens', connections.data?.length],
          ] as const
        ).map(([id, label, count]) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            onClick={() => {
              setTab(id)
            }}
            className={cn(
              '-mb-px flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm',
              tab === id
                ? 'border-accent font-medium text-text'
                : 'border-transparent text-muted hover:text-text',
            )}
          >
            {label}
            {count !== undefined && (
              <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">{count}</span>
            )}
          </button>
        ))}
      </div>

      {tab === 'ci' && org && (
        <CiAccessSection
          orgSlug={org.slug}
          projectId={projectDetail.data?.id}
          onAdd={() => {
            setGuide({ method: 'fetch' })
          }}
        />
      )}

      {tab === 'credentials' && org && (
        <>
          <p className="text-sm text-muted">
            Access tokens for GitHub, Forgejo, Vercel and others that <strong>syncs</strong> use to
            write secrets there. Save one once and every sync can pick it. CI access doesn’t need
            them: jobs use Clavis machine tokens instead.
          </p>
          <ConnectionsSection orgSlug={org.slug} />
        </>
      )}

      {tab === 'syncs' && (
        <>
          <section className="rounded-app border border-border bg-surface">
            <div className="flex items-center gap-2 border-b border-border px-4 py-3">
              <h2 className="text-base font-semibold">Syncs</h2>
              <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">
                {syncs.data?.length ?? 0}
              </span>
              <span className="hidden text-2xs text-dim md:inline">
                Clavis copies one environment into a platform’s own secret store.
              </span>
              <Button
                variant="secondary"
                size="sm"
                className="ml-auto"
                disabled={!project || (projectDetail.data?.environments ?? []).length === 0}
                onClick={() => {
                  setGuide({ method: 'push' })
                }}
              >
                <Plus className="size-3.5" /> Add a sync
              </Button>
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
                      {sync.target && (
                        <span className="font-mono text-xs text-text">{sync.target}</span>
                      )}
                      <span className="text-dim">←</span>
                      <span className="font-mono text-xs text-muted">{sync.environment}</span>
                      {clash(sync) && (
                        <span
                          className="inline-flex items-center gap-1 rounded-full bg-amber-bg px-1.5 py-0.5 text-2xs font-semibold text-amber-fg"
                          title="Another sync pushes a different environment to the same place. Their values overwrite each other. Use CI access instead."
                        >
                          <CircleAlert className="size-3" /> same target as {clash(sync)}
                        </span>
                      )}
                      {sync.connection_id ? (
                        <span
                          className="inline-flex items-center gap-1 rounded-full bg-elev px-1.5 py-0.5 text-2xs text-muted"
                          title="Uses a platform token"
                        >
                          <KeyRound className="size-3" />
                          {connections.data?.find((item) => item.id === sync.connection_id)?.name ??
                            'platform token'}
                        </span>
                      ) : (
                        sync.provider !== 'http' && (
                          <button
                            type="button"
                            className="rounded-full px-1.5 py-0.5 text-2xs text-dim hover:bg-hover hover:text-text"
                            title="This sync stores its own access token. Save it as a credential so other syncs can reuse it."
                            disabled={share.isPending}
                            onClick={() => {
                              share.mutate(sync)
                            }}
                          >
                            own token · save for reuse
                          </button>
                        )
                      )}
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
                      {sync.last_synced_at ? formatDateTime(sync.last_synced_at) : 'never'} · drift
                      checked{' '}
                      {sync.drift_checked_at ? formatDateTime(sync.drift_checked_at) : 'never'}
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
                <div className="px-4 py-6 text-sm text-muted">
                  <p>
                    No syncs for this project yet. Use a sync when the platform runs your app with
                    its own settings: Vercel, Netlify, Cloudflare, AWS, Google Cloud or Vault. For
                    Forgejo, Gitea, GitHub or GitLab pipelines,{' '}
                    <button
                      type="button"
                      className="font-medium text-accent-text hover:underline"
                      onClick={() => {
                        setTab('ci')
                      }}
                    >
                      CI access
                    </button>{' '}
                    is usually the better fit. For Kubernetes, see the{' '}
                    <Link to="/docs" className="font-medium text-accent-text hover:underline">
                      operator docs
                    </Link>
                    .
                  </p>
                  <Button
                    variant="primary"
                    size="sm"
                    className="mt-3"
                    disabled={!project || (projectDetail.data?.environments ?? []).length === 0}
                    onClick={() => {
                      setGuide({ method: 'push' })
                    }}
                  >
                    <Plus className="size-3.5" /> Add a sync
                  </Button>
                </div>
              )}
            </div>
          </section>
        </>
      )}

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
                  <span className="ml-auto text-dim">{formatDateTime(item.started_at)}</span>
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
      {guide && org && (
        <SetupGuide
          orgSlug={org.slug}
          projectSlug={project}
          envs={projectDetail.data?.environments ?? []}
          start={guide}
          onClose={() => {
            setGuide(null)
          }}
          onPush={(providerId) => {
            const provider = providers.find((candidate) => candidate.id === providerId)
            setGuide(null)
            setTab('syncs')
            if (provider) setCreating(provider)
          }}
          onCreated={() => {
            setTab('ci')
          }}
        />
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
          existing={syncs.data ?? []}
          onUseFetch={() => {
            const platform = platforms.find((candidate) => candidate.sync === creating.id)
            setCreating(null)
            setGuide({ platform: platform?.id, method: 'fetch' })
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
