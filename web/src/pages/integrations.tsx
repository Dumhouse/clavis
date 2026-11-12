import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Cable, Loader2, Play, RefreshCw, ShieldAlert, Trash2 } from 'lucide-react'
import { useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type Project = components['schemas']['ProjectBody']
type Sync = components['schemas']['SyncBody']
type Run = components['schemas']['SyncRunBody']

export function IntegrationsPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [projectSlug, setProjectSlug] = useState('')
  const [selected, setSelected] = useState<Sync | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

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

  const project = projectSlug || projects.data?.[0]?.slug || ''

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
      setNotice(
        input.confirm
          ? `Sync ${data?.run?.status ?? 'started'} with ${String(data?.run?.secret_count ?? 0)} keys.`
          : `Preview: ${String(data?.preview?.count ?? 0)} keys → ${data?.preview?.destination ?? ''}`,
      )
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
      if (!window.confirm('Delete this sync? The destination keeps its current values.')) return
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
            <h1 className="font-head text-[24px] font-semibold tracking-tight">Integrations</h1>
          </div>
          <p className="mt-1 text-[13px] text-muted">
            Syncs push resolved secrets to GitHub, GitLab, Vercel, AWS, GCP, Netlify, Cloudflare, or
            a signed HTTP endpoint.
          </p>
        </div>
        <label className="block">
          <span className="mb-1 block text-[11.5px] font-medium text-muted">Project</span>
          <select
            className="h-8 rounded-app-sm border border-border bg-bg px-2 text-[12px] text-text focus:border-accent-line focus:outline-none"
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
          </select>
        </label>
      </header>

      {notice && (
        <p className="rounded-app-sm border border-border bg-surface px-3 py-2 text-[11.5px] text-muted">
          {notice}
        </p>
      )}

      <section className="rounded-app border border-border bg-surface">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <h2 className="text-[13px] font-semibold">Syncs</h2>
          <span className="rounded-full bg-elev px-1.5 text-[10.5px] text-muted">
            {syncs.data?.length ?? 0}
          </span>
        </div>
        <div className="divide-y divide-border-soft">
          {(syncs.data ?? []).map((sync) => {
            const driftKeys = sync.drift_keys ?? []
            return (
              <div key={sync.id} className="px-4 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="rounded-full bg-elev px-1.5 py-0.5 text-[10px] font-semibold uppercase text-muted">
                    {sync.provider}
                  </span>
                  <span className="text-[12px] font-medium">{sync.environment}</span>
                  <span
                    className={
                      sync.status === 'success'
                        ? 'rounded-full bg-success-bg px-1.5 py-0.5 text-[10px] font-semibold text-success'
                        : sync.status === 'failed'
                          ? 'rounded-full bg-danger-bg px-1.5 py-0.5 text-[10px] font-semibold text-danger'
                          : 'rounded-full bg-elev px-1.5 py-0.5 text-[10px] font-semibold text-muted'
                    }
                  >
                    {sync.status}
                  </span>
                  {!sync.confirmed && (
                    <span className="rounded-full bg-amber-bg px-1.5 py-0.5 text-[10px] font-semibold text-amber-fg">
                      first run unconfirmed
                    </span>
                  )}
                  {driftKeys.length > 0 && (
                    <span className="inline-flex items-center gap-1 rounded-full bg-danger-bg px-1.5 py-0.5 text-[10px] font-semibold text-danger">
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
                <label className="mt-1 flex items-center gap-1.5 text-[10.5px] text-dim">
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
                <p className="mt-1 text-[10.5px] text-dim">
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
            <p className="flex items-center gap-2 px-4 py-5 text-[12px] text-muted">
              <Loader2 className="size-4 animate-spin" /> Loading syncs…
            </p>
          )}
          {!syncs.isLoading && (syncs.data?.length ?? 0) === 0 && (
            <p className="px-4 py-6 text-[12px] text-muted">
              No syncs for this project. Create one with{' '}
              <code className="font-mono">clavis syncs create</code> or the API.
            </p>
          )}
        </div>
      </section>

      {selected && (
        <section className="rounded-app border border-border bg-surface">
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <h2 className="text-[13px] font-semibold">Run history</h2>
            <code className="font-mono text-[10.5px] text-dim">{selected.id}</code>
            <Button
              variant="ghost"
              size="sm"
              className="ml-auto"
              onClick={() => {
                setSelected(null)
              }}
            >
              Close
            </Button>
          </div>
          <div className="divide-y divide-border-soft">
            {(runs.data ?? []).map((item: Run) => (
              <div key={item.id} className="px-4 py-2">
                <div className="flex flex-wrap items-center gap-2 text-[11.5px]">
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
                {item.error && (
                  <p className="mt-1 rounded-app-sm bg-danger-bg px-2 py-1 text-[11px] text-danger">
                    {item.error}
                  </p>
                )}
              </div>
            ))}
            {runs.isLoading && (
              <p className="flex items-center gap-2 px-4 py-4 text-[12px] text-muted">
                <Loader2 className="size-4 animate-spin" /> Loading runs…
              </p>
            )}
            {!runs.isLoading && (runs.data?.length ?? 0) === 0 && (
              <p className="px-4 py-4 text-[12px] text-muted">No runs yet.</p>
            )}
          </div>
        </section>
      )}
    </div>
  )
}
