import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowDown, ArrowLeft, ArrowUp, Copy, Loader2, Lock, Plus, Trash2, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']

const envTypeLabels: Record<string, string> = {
  development: 'Development',
  preview: 'Preview',
  staging: 'Staging',
  production: 'Production',
}

const envClass: Record<string, string> = {
  development: 'env-development',
  preview: 'env-preview',
  staging: 'env-staging',
  production: 'env-production',
}

export function EnvironmentsPage() {
  const { org } = useCurrentOrg()
  const params = useParams({ strict: false }) as { project?: string }
  const projectSlug = params.project ?? ''
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [cloning, setCloning] = useState<Env | null>(null)

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

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['project', org?.slug, projectSlug] })
    await queryClient.invalidateQueries({ queryKey: ['projects', org?.slug] })
  }

  const update = useMutation({
    mutationFn: async (input: { env: Env; body: Record<string, unknown> }) => {
      if (!org) throw new Error('No organization.')
      const result = await api.PATCH('/api/v1/orgs/{org}/projects/{project}/environments/{env}', {
        params: { path: { org: org.slug, project: projectSlug, env: input.env.slug } },
        body: input.body as {
          name?: string
          type?: 'development' | 'preview' | 'staging' | 'production'
          protected?: boolean
          position?: number
          parent_slug?: string
        },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not update the environment.'))
    },
    onSuccess: invalidate,
  })

  const remove = useMutation({
    mutationFn: async (env: Env) => {
      if (!org) throw new Error('No organization.')
      if (!window.confirm(`Delete ${env.name}? Its secrets will be removed.`)) return
      const result = await api.DELETE('/api/v1/orgs/{org}/projects/{project}/environments/{env}', {
        params: { path: { org: org.slug, project: projectSlug, env: env.slug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not delete the environment.'))
    },
    onSuccess: invalidate,
  })

  function move(index: number, direction: -1 | 1) {
    const current = envs[index]
    const swap = envs[index + direction]
    if (!current || !swap) return
    update.mutate({ env: current, body: { position: swap.position } })
    update.mutate({ env: swap, body: { position: current.position } })
  }

  if (project.isLoading) {
    return (
      <div className="flex items-center gap-2 text-[13px] text-muted">
        <Loader2 className="size-4 animate-spin" /> Loading environments…
      </div>
    )
  }
  if (project.isError || !project.data) {
    return (
      <div className="rounded-app bg-danger-bg px-4 py-3 text-[13px] text-danger">
        {project.error instanceof Error ? project.error.message : 'Project not found.'}
      </div>
    )
  }

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/projects/$project"
          params={{ project: projectSlug }}
          className="inline-flex items-center gap-1.5 text-[13px] text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> {project.data.name}
        </Link>
        <div className="mt-2 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-head text-[25px] font-semibold tracking-tight">Environments</h1>
            <p className="mt-1 text-[14px] text-muted">
              Order, type, and protection for {project.data.name}.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Link
              to="/projects/$project/snapshots"
              params={{ project: projectSlug }}
              className="rounded-app-sm border border-border bg-surface px-3.5 py-2 text-[13px] font-medium text-text hover:bg-elev"
            >
              Snapshots
            </Link>
            <Button
              variant="primary"
              size="md"
              onClick={() => {
                setCreating(true)
              }}
            >
              <Plus className="size-4" /> New environment
            </Button>
          </div>
        </div>
      </div>

      <div className="divide-y divide-border-soft overflow-hidden rounded-app border border-border bg-surface">
        {envs.map((env, index) => (
          <div key={env.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <div className="flex w-12 items-center gap-1">
              <button
                type="button"
                className="inline-flex size-6 items-center justify-center rounded text-dim hover:bg-hover hover:text-text disabled:opacity-30"
                disabled={index === 0 || update.isPending}
                onClick={() => {
                  move(index, -1)
                }}
                aria-label={`Move ${env.name} up`}
              >
                <ArrowUp className="size-3.5" />
              </button>
              <button
                type="button"
                className="inline-flex size-6 items-center justify-center rounded text-dim hover:bg-hover hover:text-text disabled:opacity-30"
                disabled={index === envs.length - 1 || update.isPending}
                onClick={() => {
                  move(index, 1)
                }}
                aria-label={`Move ${env.name} down`}
              >
                <ArrowDown className="size-3.5" />
              </button>
            </div>
            <div className="min-w-0 flex-1">
              <p className="flex items-center gap-2 text-[13.5px] font-medium">
                {env.name}
                {env.protected && <Lock className="size-3 text-amber-fg" />}
                {env.protected !== (env.type === 'production') && (
                  <span className="rounded-full bg-amber-bg px-1.5 py-0.5 text-[10.5px] font-semibold text-amber-fg">
                    deviates from type default
                  </span>
                )}
              </p>
              <p className="font-mono text-[12px] text-dim">{env.slug}</p>
              <label className="mt-1 flex items-center gap-1.5 text-[11.5px] text-dim">
                Inherits from
                <select
                  className="h-6 rounded border border-border bg-bg px-1 text-[11.5px] text-text focus:border-accent-line focus:outline-none"
                  value={envs.find((candidate) => candidate.id === env.parent_id)?.slug ?? ''}
                  onChange={(event) => {
                    update.mutate({ env, body: { parent_slug: event.target.value } })
                  }}
                  disabled={update.isPending}
                >
                  <option value="">Nothing</option>
                  {envs
                    .filter((candidate) => candidate.id !== env.id)
                    .map((candidate) => (
                      <option key={candidate.id} value={candidate.slug}>
                        {candidate.name}
                      </option>
                    ))}
                </select>
              </label>
            </div>
            <span className={cn('env-chip', envClass[env.type])}>
              {envTypeLabels[env.type] ?? env.type}
            </span>
            <select
              className="h-7 rounded-app-sm border border-border bg-bg px-1.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
              value={env.type}
              onChange={(event) => {
                update.mutate({ env, body: { type: event.target.value } })
              }}
              disabled={update.isPending}
            >
              {Object.entries(envTypeLabels).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
            <label className="flex items-center gap-1.5 text-[12px] text-muted">
              <input
                type="checkbox"
                checked={env.protected}
                onChange={(event) => {
                  update.mutate({ env, body: { protected: event.target.checked } })
                }}
                disabled={update.isPending || env.type === 'production'}
              />
              protected
            </label>
            <Button
              variant="ghost"
              size="icon"
              aria-label={`Clone ${env.name}`}
              onClick={() => {
                setCloning(env)
              }}
            >
              <Copy className="size-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              aria-label={`Delete ${env.name}`}
              disabled={remove.isPending || envs.length <= 1}
              onClick={() => {
                remove.mutate(env)
              }}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        ))}
        {envs.length === 0 && (
          <p className="px-4 py-8 text-center text-[13px] text-muted">No environments.</p>
        )}
      </div>

      {(update.error || remove.error) && (
        <p className="rounded-app bg-danger-bg px-4 py-3 text-[13px] text-danger">
          {update.error instanceof Error ? update.error.message : ''}
          {remove.error instanceof Error ? remove.error.message : ''}
        </p>
      )}

      {cloning && (
        <CloneEnvironmentDialog
          orgSlug={org?.slug ?? ''}
          projectSlug={projectSlug}
          source={cloning}
          onClose={() => {
            setCloning(null)
          }}
          onCloned={() => {
            setCloning(null)
            void invalidate()
          }}
        />
      )}

      {creating && (
        <CreateEnvironmentDialog
          orgSlug={org?.slug ?? ''}
          projectSlug={projectSlug}
          onClose={() => {
            setCreating(false)
          }}
          onCreated={() => {
            setCreating(false)
            void invalidate()
          }}
        />
      )}
    </div>
  )
}

function CloneEnvironmentDialog({
  orgSlug,
  projectSlug,
  source,
  onClose,
  onCloned,
}: {
  orgSlug: string
  projectSlug: string
  source: Env
  onClose: () => void
  onCloned: () => void
}) {
  const [name, setName] = useState(`${source.name} copy`)
  const [slug, setSlug] = useState(`${source.slug}-copy`)
  const [includeValues, setIncludeValues] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const result = await api.POST(
      '/api/v1/orgs/{org}/projects/{project}/environments/{env}/clone',
      {
        params: { path: { org: orgSlug, project: projectSlug, env: source.slug } },
        body: { slug, name, include_values: includeValues },
      },
    )
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not clone the environment.'))
      return
    }
    onCloned()
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="w-full max-w-md rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[18px] font-semibold">Clone {source.name}</h2>
            <p className="mt-0.5 text-[13px] text-muted">
              Copies key names, folders, tags, comments, and runbook notes.
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
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Name</span>
            <input
              required
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
              value={name}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Slug</span>
            <input
              required
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
              value={slug}
              onChange={(event) => {
                setSlug(event.target.value)
              }}
            />
          </label>
          <label className="flex items-start gap-2 text-[13px]">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={includeValues}
              onChange={(event) => {
                setIncludeValues(event.target.checked)
              }}
            />
            <span>
              <span className="block font-medium">Include values</span>
              <span className="block text-[12px] text-muted">
                Off copies key names only; new versions start empty.
              </span>
            </span>
          </label>
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
              {busy && <Loader2 className="size-3.5 animate-spin" />} Clone environment
            </Button>
          </div>
        </form>
      </div>
    </div>
  )
}

function CreateEnvironmentDialog({
  orgSlug,
  projectSlug,
  onClose,
  onCreated,
}: {
  orgSlug: string
  projectSlug: string
  onClose: () => void
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [type, setType] = useState('development')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/environments', {
      params: { path: { org: orgSlug, project: projectSlug } },
      body: { name, slug, type: type as 'development' | 'preview' | 'staging' | 'production' },
    })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not create the environment.'))
      return
    }
    onCreated()
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="w-full max-w-md rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[18px] font-semibold">New environment</h2>
            <p className="mt-0.5 text-[13px] text-muted">
              Production environments are protected by default.
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
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Name</span>
            <input
              required
              autoFocus
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
              placeholder="QA"
              value={name}
              onChange={(event) => {
                setName(event.target.value)
                if (!slug)
                  setSlug(
                    event.target.value
                      .toLowerCase()
                      .replace(/[^a-z0-9]+/g, '-')
                      .replace(/^-|-$/g, ''),
                  )
              }}
            />
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Slug</span>
              <input
                required
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
                value={slug}
                onChange={(event) => {
                  setSlug(event.target.value)
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
                }}
              >
                {Object.entries(envTypeLabels).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
          </div>
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
              {busy && <Loader2 className="size-3.5 animate-spin" />} Create environment
            </Button>
          </div>
        </form>
      </div>
    </div>
  )
}
