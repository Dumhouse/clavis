import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  ChevronDown,
  Copy,
  CornerDownRight,
  Loader2,
  Lock,
  MoreHorizontal,
  Plus,
  Trash2,
  X,
} from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Input, Notice } from '@/components/ui/field'
import { InfoHint } from '@/components/ui/info-hint'
import { Menu } from '@/components/ui/menu'
import { Modal } from '@/components/ui/modal'
import { Switch } from '@/components/ui/switch'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'
import { confirmAction } from '@/components/ui/dialogs'

type Env = components['schemas']['EnvBody']

const envTypeLabels: Record<string, string> = {
  development: 'Development',
  preview: 'Preview',
  staging: 'Staging',
  production: 'Production',
}

const envTypeDescriptions: Record<string, string> = {
  development: 'Local work. Anyone with write access can change values.',
  preview: 'Branch and review apps that come and go.',
  staging: 'Mirrors production for testing releases.',
  production: 'Always protected. Reveals need a reason; only Admins write.',
}

const envDot: Record<string, string> = {
  development: 'bg-dev',
  preview: 'bg-preview',
  staging: 'bg-stg',
  production: 'bg-prod',
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
  const [inheriting, setInheriting] = useState<Env | null>(null)

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

  // Display order: each environment followed by the ones that inherit from it.
  const tree: Array<{ env: Env; depth: number }> = []
  const placed = new Set<string>()
  const visit = (env: Env, depth: number) => {
    if (placed.has(env.id)) return
    placed.add(env.id)
    tree.push({ env, depth })
    for (const child of envs.filter((candidate) => candidate.parent_id === env.id)) {
      visit(child, depth + 1)
    }
  }
  for (const env of envs) {
    if (!env.parent_id || !envs.some((candidate) => candidate.id === env.parent_id)) visit(env, 0)
  }
  for (const env of envs) visit(env, 0)

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
      if (!(await confirmAction(`Delete ${env.name}? Its secrets will be removed.`))) return
      const result = await api.DELETE('/api/v1/orgs/{org}/projects/{project}/environments/{env}', {
        params: { path: { org: org.slug, project: projectSlug, env: env.slug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not delete the environment.'))
    },
    onSuccess: invalidate,
  })

  async function changeType(env: Env, value: string, label: string) {
    if (value === 'production' || env.type === 'production') {
      const ok = await confirmAction(
        `Change ${env.name} to ${label}? This changes who can reveal values and whether writes need an Admin.`,
        'Change type',
      )
      if (!ok) return
    }
    update.mutate({ env, body: { type: value } })
  }

  function move(index: number, direction: -1 | 1) {
    const current = envs[index]
    const swap = envs[index + direction]
    if (!current || !swap) return
    update.mutate({ env: current, body: { position: swap.position } })
    update.mutate({ env: swap, body: { position: current.position } })
  }

  if (project.isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted">
        <Loader2 className="size-4 animate-spin" /> Loading environments…
      </div>
    )
  }
  if (project.isError || !project.data) {
    return (
      <div className="rounded-app bg-danger-bg px-4 py-3 text-sm text-danger">
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
          className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> {project.data.name}
        </Link>
        <div className="mt-2 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-head text-2xl font-semibold tracking-tight">Environments</h1>
            <p className="mt-1 text-base text-muted">
              Order, type, and protection for {project.data.name}.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Link
              to="/projects/$project/snapshots"
              params={{ project: projectSlug }}
              className="rounded-app-sm border border-border bg-surface px-3.5 py-2 text-sm font-medium text-text hover:bg-elev"
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

      <ul className="divide-y divide-border-soft rounded-app border border-border bg-surface">
        {tree.map(({ env, depth }) => {
          const index = envs.indexOf(env)
          const parent = envs.find((candidate) => candidate.id === env.parent_id)
          return (
            <li key={env.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <div className="flex items-center gap-0.5">
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
              <div
                className="flex min-w-0 flex-1 items-start gap-2"
                style={{ paddingLeft: `${String(depth * 20)}px` }}
              >
                {depth > 0 && <CornerDownRight className="mt-0.5 size-3.5 shrink-0 text-dim" />}
                <div className="min-w-0">
                  <p className="flex flex-wrap items-center gap-2">
                    <span className="text-sm font-medium">{env.name}</span>
                    <Menu
                      label={`Change type of ${env.name} (now ${envTypeLabels[env.type] ?? env.type})`}
                      align="start"
                      menuClassName="w-72"
                      triggerClassName={cn(
                        'env-chip group inline-flex items-center gap-1 transition-[filter] hover:brightness-125 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-line',
                        envClass[env.type],
                      )}
                      trigger={
                        <>
                          {envTypeLabels[env.type] ?? env.type}
                          <ChevronDown className="size-3 opacity-60 group-hover:opacity-100" />
                        </>
                      }
                      items={Object.entries(envTypeLabels).map(([value, label]) => ({
                        label,
                        description: envTypeDescriptions[value],
                        icon: <span className={cn('block size-2 rounded-full', envDot[value])} />,
                        checked: value === env.type,
                        disabled: update.isPending,
                        onSelect: () => {
                          if (value === env.type) return
                          void changeType(env, value, label)
                        },
                      }))}
                    />
                    {env.protected && (
                      <span className="inline-flex items-center gap-1 text-2xs text-amber-fg">
                        <Lock className="size-3" /> protected
                      </span>
                    )}
                  </p>
                  <p className="mt-0.5 flex flex-wrap items-center gap-x-2 text-2xs text-dim">
                    <span className="font-mono">{env.slug}</span>
                    {parent && <span>· inherits missing keys from {parent.name}</span>}
                    {env.protected !== (env.type === 'production') && (
                      <span className="text-amber-fg">
                        · {env.protected ? 'protected unlike other' : 'unprotected unlike other'}{' '}
                        {envTypeLabels[env.type]?.toLowerCase()} environments
                      </span>
                    )}
                  </p>
                </div>
              </div>
              <label className="flex items-center gap-2 text-xs text-muted">
                <span className="flex items-center gap-1">
                  Protected
                  <InfoHint label="What protected means">
                    Only Admins and Owners can write or delete values here. Promotions into a
                    protected environment become approval requests, and production also requires a
                    reason to reveal values. Reads otherwise follow normal RBAC.
                  </InfoHint>
                </span>
                <Switch
                  checked={env.protected}
                  label={`Protect ${env.name}`}
                  title={
                    env.type === 'production'
                      ? 'Production is always protected'
                      : 'Only Admins can change values in protected environments'
                  }
                  disabled={update.isPending || env.type === 'production'}
                  onChange={(next) => {
                    update.mutate({ env, body: { protected: next } })
                  }}
                />
              </label>
              <Menu
                label={`Actions for ${env.name}`}
                triggerClassName="inline-flex size-8 items-center justify-center rounded-app-sm text-dim hover:bg-hover hover:text-text"
                trigger={<MoreHorizontal className="size-4" />}
                items={[
                  {
                    label: parent ? 'Stop inheriting' : 'Inherit from…',
                    icon: <CornerDownRight />,
                    disabled: !parent && envs.length < 2,
                    onSelect: () => {
                      if (parent) {
                        update.mutate({ env, body: { parent_slug: '' } })
                      } else {
                        setInheriting(env)
                      }
                    },
                  },
                  {
                    label: 'Clone',
                    icon: <Copy />,
                    onSelect: () => {
                      setCloning(env)
                    },
                  },
                  'separator' as const,
                  {
                    label: 'Delete',
                    icon: <Trash2 />,
                    danger: true,
                    disabled: remove.isPending || envs.length <= 1,
                    onSelect: () => {
                      remove.mutate(env)
                    },
                  },
                ]}
              />
            </li>
          )
        })}
        {envs.length === 0 && (
          <li className="px-4 py-8 text-center text-sm text-muted">No environments.</li>
        )}
      </ul>

      {inheriting && (
        <Modal
          onClose={() => {
            setInheriting(null)
          }}
          className="max-w-sm"
        >
          <h2 className="font-head text-md font-semibold">Inherit into {inheriting.name}</h2>
          <p className="mt-1 text-sm text-muted">
            Keys missing from {inheriting.name} are read from the parent. Setting a key here
            overrides it.
          </p>
          <ul className="mt-3 space-y-1">
            {envs
              .filter((candidate) => candidate.id !== inheriting.id)
              .map((candidate) => (
                <li key={candidate.id}>
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 rounded-app-sm border border-border px-3 py-2 text-left text-sm hover:border-accent-line hover:bg-hover-soft"
                    onClick={() => {
                      update.mutate({ env: inheriting, body: { parent_slug: candidate.slug } })
                      setInheriting(null)
                    }}
                  >
                    <span className={cn('env-chip', envClass[candidate.type])}>
                      {candidate.slug}
                    </span>
                    {candidate.name}
                  </button>
                </li>
              ))}
          </ul>
        </Modal>
      )}

      {(update.error || remove.error) && (
        <Notice>
          {update.error instanceof Error ? update.error.message : ''}
          {remove.error instanceof Error ? remove.error.message : ''}
        </Notice>
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
    <Modal onClose={onClose} className="max-w-md">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="font-head text-lg font-semibold">Clone {source.name}</h2>
          <p className="mt-0.5 text-sm text-muted">
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
          <span className="mb-1 block text-xs font-medium text-muted">Name</span>
          <Input
            required
            className="h-9"
            value={name}
            onChange={(event) => {
              setName(event.target.value)
            }}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Slug</span>
          <Input
            required
            className="h-9 font-mono"
            value={slug}
            onChange={(event) => {
              setSlug(event.target.value)
            }}
          />
        </label>
        <label className="flex items-start gap-2 text-sm">
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
            <span className="block text-xs text-muted">
              Off copies key names only; new versions start empty.
            </span>
          </span>
        </label>
        {error && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
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
    </Modal>
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
    <Modal onClose={onClose} className="max-w-md">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="font-head text-lg font-semibold">New environment</h2>
          <p className="mt-0.5 text-sm text-muted">
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
          <span className="mb-1 block text-xs font-medium text-muted">Name</span>
          <Input
            required
            autoFocus
            className="h-9"
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
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Slug</span>
          <Input
            required
            className="h-9 font-mono"
            value={slug}
            onChange={(event) => {
              setSlug(event.target.value)
            }}
          />
        </label>
        <fieldset>
          <legend className="mb-1.5 text-xs font-medium text-muted">Type</legend>
          <div className="grid grid-cols-2 gap-2">
            {Object.entries(envTypeLabels).map(([value, label]) => (
              <label
                key={value}
                className={cn(
                  'flex cursor-pointer items-start gap-2 rounded-app-sm border px-2.5 py-2 transition-colors',
                  'has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-accent-line',
                  type === value
                    ? 'border-accent-line bg-accent-soft/40'
                    : 'border-border hover:bg-hover',
                )}
              >
                <input
                  type="radio"
                  name="env-type"
                  value={value}
                  checked={type === value}
                  onChange={() => {
                    setType(value)
                  }}
                  className="sr-only"
                />
                <span className={cn('mt-1.5 size-2 shrink-0 rounded-full', envDot[value])} />
                <span className="min-w-0">
                  <span className="block text-sm font-medium">{label}</span>
                  <span className="block text-xs leading-snug text-dim">
                    {envTypeDescriptions[value]}
                  </span>
                </span>
              </label>
            ))}
          </div>
        </fieldset>
        {error && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
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
    </Modal>
  )
}
