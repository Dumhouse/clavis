import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Clock, Loader2, RotateCcw } from 'lucide-react'
import { useState } from 'react'
import { Link, useParams } from '@tanstack/react-router'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type Env = components['schemas']['EnvBody']
type Snapshot = components['schemas']['SnapshotBody']

export function SnapshotsPage() {
  const { org } = useCurrentOrg()
  const params = useParams({ strict: false }) as { project?: string }
  const projectSlug = params.project ?? ''
  const queryClient = useQueryClient()
  const [envSlug, setEnvSlug] = useState('')
  const [reason, setReason] = useState('')
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

  const envs = [...(project.data?.environments ?? [])].sort((a, b) => a.position - b.position)
  const selected = envSlug || envs[0]?.slug || ''

  const snapshots = useQuery({
    queryKey: ['snapshots', org?.slug, projectSlug, selected],
    enabled: Boolean(org && projectSlug && selected),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/snapshots',
        { params: { path: { org: org.slug, project: projectSlug, env: selected } } },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load snapshots.'))
      return result.data?.snapshots ?? []
    },
  })

  const invalidate = async () => {
    await queryClient.invalidateQueries({
      queryKey: ['snapshots', org?.slug, projectSlug, selected],
    })
    await queryClient.invalidateQueries({ queryKey: ['secrets', org?.slug, projectSlug] })
  }

  const create = useMutation({
    mutationFn: async () => {
      if (!org || !selected) throw new Error('Choose an environment.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/snapshots',
        {
          params: { path: { org: org.slug, project: projectSlug, env: selected } },
          body: { reason: reason.trim() },
        },
      )
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not capture a snapshot.'))
    },
    onSuccess: async () => {
      setReason('')
      setError(null)
      await invalidate()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not capture a snapshot.')
    },
  })

  const restore = useMutation({
    mutationFn: async (snapshot: Snapshot) => {
      if (!org) throw new Error('No organization.')
      if (
        !window.confirm(
          `Restore ${selected} to ${new Date(snapshot.created_at).toLocaleString()}? Current values are kept as versions.`,
        )
      )
        return
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/snapshots/{id}/restore',
        {
          params: {
            path: { org: org.slug, project: projectSlug, env: selected, id: snapshot.id },
          },
        },
      )
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not restore the snapshot.'))
    },
    onSuccess: invalidate,
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not restore the snapshot.')
    },
  })

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/projects/$project"
          params={{ project: projectSlug }}
          className="inline-flex items-center gap-1.5 text-[12px] text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> {projectSlug}
        </Link>
        <div className="mt-2 flex items-center gap-2">
          <Clock className="size-5 text-muted" />
          <h1 className="font-head text-[24px] font-semibold tracking-tight">Snapshots</h1>
        </div>
        <p className="mt-1 text-[13px] text-muted">
          Point-in-time captures. Restoring writes new versions, so no history is lost.
        </p>
      </div>

      <section className="flex flex-wrap items-end gap-3 rounded-app border border-border bg-surface px-4 py-3">
        <label className="block">
          <span className="mb-1 block text-[11.5px] font-medium text-muted">Environment</span>
          <select
            className="h-8 rounded-app-sm border border-border bg-bg px-2 text-[12px] text-text focus:border-accent-line focus:outline-none"
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
        <label className="block min-w-48 flex-1">
          <span className="mb-1 block text-[11.5px] font-medium text-muted">Reason (optional)</span>
          <input
            className="h-8 w-full rounded-app-sm border border-border bg-bg px-2 text-[12px] text-text focus:border-accent-line focus:outline-none"
            placeholder="Before rotating the database password"
            value={reason}
            onChange={(event) => {
              setReason(event.target.value)
            }}
            maxLength={512}
          />
        </label>
        <Button
          variant="primary"
          size="sm"
          disabled={create.isPending || !selected}
          onClick={() => {
            create.mutate()
          }}
        >
          {create.isPending && <Loader2 className="size-3.5 animate-spin" />} Capture snapshot
        </Button>
      </section>

      {error && (
        <p className="rounded-app-sm bg-danger-bg px-3 py-2 text-[11.5px] text-danger">{error}</p>
      )}

      <section className="rounded-app border border-border bg-surface">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <h2 className="text-[13px] font-semibold">Recent snapshots</h2>
          <span className="rounded-full bg-elev px-1.5 text-[10.5px] text-muted">
            {snapshots.data?.length ?? 0}
          </span>
        </div>
        <div className="divide-y divide-border-soft">
          {(snapshots.data ?? []).map((snapshot) => (
            <div key={snapshot.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="text-[12px] font-medium">
                  {new Date(snapshot.created_at).toLocaleString()}
                </p>
                <p className="text-[11px] text-dim">{snapshot.reason || 'No reason recorded'}</p>
              </div>
              <Button
                variant="secondary"
                size="sm"
                disabled={restore.isPending}
                onClick={() => {
                  restore.mutate(snapshot)
                }}
              >
                <RotateCcw className="size-3.5" /> Restore
              </Button>
            </div>
          ))}
          {snapshots.isLoading && (
            <p className="flex items-center gap-2 px-4 py-5 text-[12px] text-muted">
              <Loader2 className="size-4 animate-spin" /> Loading snapshots…
            </p>
          )}
          {!snapshots.isLoading && (snapshots.data?.length ?? 0) === 0 && (
            <p className="px-4 py-5 text-[12px] text-muted">
              No snapshots yet. Capture one before a risky change.
            </p>
          )}
        </div>
      </section>
    </div>
  )
}
