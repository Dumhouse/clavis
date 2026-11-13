import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Check,
  ChevronDown,
  Folder,
  Loader2,
  Pencil,
  Plus,
  Sparkles,
  TerminalSquare,
  Trash2,
  X,
} from 'lucide-react'
import { useState, type FormEvent, type ReactNode } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { ConnectGuide } from '@/components/connect-guide'
import { Modal } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { useOrgEvents } from '@/lib/events'
import { useShortcuts } from '@/lib/use-shortcuts'
import { cn } from '@/lib/utils'
import { confirmAction } from '@/components/ui/dialogs'
import { Input } from '@/components/ui/field'

type Project = components['schemas']['ProjectBody']
type Env = components['schemas']['EnvBody']

const envOrder: Record<string, number> = { development: 0, preview: 1, staging: 2, production: 3 }

function envClass(type: string) {
  return type === 'development'
    ? 'env-development'
    : type === 'preview'
      ? 'env-preview'
      : type === 'staging'
        ? 'env-staging'
        : 'env-production'
}

function envLabel(type: string) {
  return type === 'development'
    ? 'dev'
    : type === 'preview'
      ? 'preview'
      : type === 'staging'
        ? 'staging'
        : 'prod'
}

function sortEnvs(envs: Env[] | null | undefined): Env[] {
  return [...(envs ?? [])].sort((a, b) => (envOrder[a.type] ?? 9) - (envOrder[b.type] ?? 9))
}

export function ProjectsPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<Project | null>(null)

  // Page shortcut advertised in the hint bar: N creates a project.
  useShortcuts({
    n: () => {
      setCreating(true)
    },
  })

  useOrgEvents(org?.slug, (event) => {
    if (event.type === 'projects.changed' || event.type === 'environments.changed') {
      void queryClient.invalidateQueries({ queryKey: ['projects', org?.slug] })
    }
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

  const updateProject = useMutation({
    mutationFn: async (input: { project: Project; name: string; slug: string }) => {
      if (!org) throw new Error('No organization selected.')
      const result = await api.PATCH('/api/v1/orgs/{org}/projects/{project}', {
        params: { path: { org: org.slug, project: input.project.slug } },
        body: { name: input.name, slug: input.slug },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not update the project.'))
      return result.data
    },
    onSuccess: async () => {
      setEditing(null)
      await queryClient.invalidateQueries({ queryKey: ['projects', org?.slug] })
    },
  })

  const deleteProject = useMutation({
    mutationFn: async (project: Project) => {
      if (!org) throw new Error('No organization selected.')
      if (
        !(await confirmAction(
          `Delete ${project.name}? Every environment and secret in it will be removed.`,
        ))
      )
        return
      const result = await api.DELETE('/api/v1/orgs/{org}/projects/{project}', {
        params: { path: { org: org.slug, project: project.slug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not delete the project.'))
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['projects', org?.slug] })
    },
  })

  const createProject = useMutation({
    mutationFn: async (input: { name: string; slug: string }) => {
      if (!org) throw new Error('No organization selected.')
      const result = await api.POST('/api/v1/orgs/{org}/projects', {
        params: { path: { org: org.slug } },
        body: { name: input.name, slug: input.slug },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not create the project.'))
      return result.data
    },
    onSuccess: async () => {
      setCreating(false)
      await queryClient.invalidateQueries({ queryKey: ['projects', org?.slug] })
    },
  })

  if (!org) {
    return (
      <section className="rounded-app border border-dashed border-border bg-panel p-8">
        <h1 className="font-head text-xl font-semibold">No organization yet</h1>
        <p className="mt-1 text-sm text-muted">
          Create an organization through the API or finish setup again.
        </p>
      </section>
    )
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="font-head text-2xl font-semibold tracking-tight">Projects</h1>
          <p className="mt-1 text-base text-muted">
            Secrets, environments, and access for everything your team ships.
          </p>
        </div>
        <Button
          variant="primary"
          size="md"
          onClick={() => {
            setCreating(true)
          }}
        >
          <Plus className="size-4" /> New project
        </Button>
      </header>

      <GettingStarted projectCount={projects.data?.length ?? 0} />

      {projects.isLoading && (
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-sm text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading projects…
        </div>
      )}
      {projects.isError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-sm text-danger">
          {projects.error instanceof Error ? projects.error.message : 'Could not load projects.'}
        </div>
      )}
      {projects.data && projects.data.length === 0 && (
        <section className="rounded-app border border-dashed border-border bg-panel p-8 text-center">
          <div className="mx-auto flex size-10 items-center justify-center rounded-app-sm bg-elev">
            <Folder className="size-5 text-dim" />
          </div>
          <h2 className="mt-3 text-base font-semibold">Create your first project</h2>
          <p className="mx-auto mt-1 max-w-sm text-sm text-muted">
            A project groups environments like dev, staging, and production. Default environments
            are created for you.
          </p>
          <Button
            variant="primary"
            size="sm"
            className="mt-4"
            onClick={() => {
              setCreating(true)
            }}
          >
            <Plus className="size-3.5" /> New project
          </Button>
        </section>
      )}

      <div className="grid gap-3 sm:grid-cols-2">
        {(projects.data ?? []).map((project) => (
          <ProjectCard
            key={project.id}
            project={project}
            onEdit={() => {
              setEditing(project)
            }}
            onDelete={() => {
              deleteProject.mutate(project)
            }}
          />
        ))}
      </div>

      <CliCard orgSlug={org.slug} />

      {editing && (
        <ProjectDialog
          title="Rename project"
          initialName={editing.name}
          initialSlug={editing.slug}
          busy={updateProject.isPending}
          error={updateProject.error instanceof Error ? updateProject.error.message : null}
          onClose={() => {
            setEditing(null)
          }}
          onSubmit={(input) => {
            updateProject.mutate({ project: editing, ...input })
          }}
        />
      )}

      {creating && (
        <CreateProjectDialog
          busy={createProject.isPending}
          error={createProject.error instanceof Error ? createProject.error.message : null}
          onClose={() => {
            setCreating(false)
          }}
          onSubmit={(input) => {
            createProject.mutate(input)
          }}
        />
      )}
    </div>
  )
}

/** Remembers whether a help block was collapsed, per browser. */
function useCollapsed(id: string): [boolean, () => void] {
  const key = `clavis.collapsed.${id}`
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return window.localStorage.getItem(key) === 'true'
    } catch {
      return false
    }
  })
  return [
    collapsed,
    () => {
      const next = !collapsed
      setCollapsed(next)
      try {
        window.localStorage.setItem(key, String(next))
      } catch {
        // Storage can be unavailable; the toggle still works for this visit.
      }
    },
  ]
}

function CollapseButton({
  collapsed,
  onToggle,
  label,
}: {
  collapsed: boolean
  onToggle: () => void
  label: string
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={!collapsed}
      aria-label={`${collapsed ? 'Show' : 'Hide'} ${label}`}
      title={`${collapsed ? 'Show' : 'Hide'} ${label}`}
      className="inline-flex size-7 shrink-0 items-center justify-center rounded text-dim hover:bg-hover hover:text-text"
    >
      <ChevronDown className={cn('size-4 transition-transform', collapsed && '-rotate-90')} />
    </button>
  )
}

function GettingStarted({ projectCount }: { projectCount: number }) {
  const steps = [
    { label: 'Create your organization', done: true },
    { label: 'Create a project', done: projectCount > 0 },
    { label: 'Connect the CLI', done: false },
  ]
  const completed = steps.filter((step) => step.done).length
  const [collapsed, toggle] = useCollapsed('getting-started')

  return (
    <section className="rounded-app border border-border bg-surface p-4 shadow-app">
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex size-9 shrink-0 items-center justify-center rounded-app-sm bg-accent-soft">
          <Sparkles className="size-4 text-accent-text" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-base font-semibold">Finish setting up Clavis</h2>
            <span className="text-xs text-dim">
              {completed} of {steps.length} done
            </span>
          </div>
          <div className="mt-2 h-1 overflow-hidden rounded-full bg-elev">
            <div
              className="h-full rounded-full bg-accent transition-[width] duration-200"
              style={{ width: `${String((completed / steps.length) * 100)}%` }}
            />
          </div>
          <div className={cn('mt-3 flex flex-wrap gap-x-5 gap-y-2', collapsed && 'hidden')}>
            {steps.map((step) => (
              <span key={step.label} className="flex items-center gap-1.5 text-sm">
                <span
                  className={
                    step.done
                      ? 'flex size-4 items-center justify-center rounded-full bg-accent text-accent-fg'
                      : 'size-4 rounded-full border border-border bg-bg'
                  }
                >
                  {step.done && <Check className="size-2.5" strokeWidth={3} />}
                </span>
                <span className={step.done ? 'text-muted line-through' : 'text-text'}>
                  {step.label}
                </span>
              </span>
            ))}
          </div>
        </div>
        {!collapsed && (
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              document.getElementById('connect-cli')?.scrollIntoView({ behavior: 'smooth' })
            }}
          >
            Connect CLI
          </Button>
        )}
        <CollapseButton collapsed={collapsed} onToggle={toggle} label="setup checklist" />
      </div>
    </section>
  )
}

function ProjectCard({
  project,
  onEdit,
  onDelete,
}: {
  project: Project
  onEdit: () => void
  onDelete: () => void
}) {
  const envs = sortEnvs(project.environments)
  return (
    <article className="group relative rounded-app border border-border bg-surface p-4 transition-colors hover:border-accent-line">
      <Link to="/projects/$project" params={{ project: project.slug }} className="block">
        <div className="flex items-start gap-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-app-sm bg-accent-soft text-accent-text">
            <Folder className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h3 className="truncate text-base font-semibold">{project.name}</h3>
            <p className="mt-0.5 text-sm text-muted">{project.slug}</p>
          </div>
          <span className="w-14" />
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          {envs.map((env) => (
            <span key={env.id} className={cn('env-chip', envClass(env.type))}>
              {envLabel(env.type)}
            </span>
          ))}
        </div>

        <div className="mt-4 flex items-center gap-4 border-t border-border-soft pt-3 text-xs text-dim">
          <span>{envs.length} environments</span>
          <span className="ml-auto text-muted">Open secrets →</span>
        </div>
      </Link>
      <div className="absolute right-3 top-3 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
        <button
          type="button"
          className="inline-flex size-6 items-center justify-center rounded border border-border bg-surface text-dim hover:text-text"
          aria-label={`Rename ${project.name}`}
          onClick={onEdit}
        >
          <Pencil className="size-3" />
        </button>
        <button
          type="button"
          className="inline-flex size-6 items-center justify-center rounded border border-border bg-surface text-dim hover:text-danger"
          aria-label={`Delete ${project.name}`}
          onClick={onDelete}
        >
          <Trash2 className="size-3" />
        </button>
      </div>
    </article>
  )
}

function CliCard({ orgSlug }: { orgSlug: string }): ReactNode {
  const [collapsed, toggle] = useCollapsed('connect-cli')
  return (
    <section id="connect-cli" className="grid gap-4 rounded-app border border-border bg-panel p-4">
      <div className="flex items-start gap-3">
        <div className="flex size-9 shrink-0 items-center justify-center rounded-app-sm bg-elev">
          <TerminalSquare className="size-4 text-muted" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-base font-semibold">Get secrets into your app</h2>
          <p className="mt-0.5 text-sm text-muted">
            Pick your computer and your stack. Works with any language: secrets arrive as
            environment variables.
          </p>
        </div>
        <CollapseButton collapsed={collapsed} onToggle={toggle} label="CLI guide" />
      </div>
      {!collapsed && <ConnectGuide orgSlug={orgSlug} />}
    </section>
  )
}

function CreateProjectDialog({
  busy,
  error,
  onClose,
  onSubmit,
}: {
  busy: boolean
  error: string | null
  onClose: () => void
  onSubmit: (input: { name: string; slug: string }) => void
}) {
  return (
    <ProjectDialog
      title="New project"
      initialName=""
      initialSlug=""
      busy={busy}
      error={error}
      onClose={onClose}
      onSubmit={onSubmit}
    />
  )
}

function ProjectDialog({
  title,
  initialName,
  initialSlug,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  title: string
  initialName: string
  initialSlug: string
  busy: boolean
  error: string | null
  onClose: () => void
  onSubmit: (input: { name: string; slug: string }) => void
}) {
  const [name, setName] = useState(initialName)
  const [slug, setSlug] = useState(initialSlug)
  // The slug follows the name until the user edits it directly. Checking
  // against the current slug only worked for the first keystroke: once the
  // slug became non-empty it stopped tracking the name.
  const [slugAuto, setSlugAuto] = useState(!initialSlug || initialSlug === slugify(initialName))

  function submit(event: FormEvent) {
    event.preventDefault()
    onSubmit({ name, slug: slug || slugify(name) })
  }

  return (
    <Modal onClose={onClose} className="max-w-md">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="font-head text-lg font-semibold">{title}</h2>
          <p className="mt-0.5 text-sm text-muted">
            Dev, staging, and production environments are created automatically.
          </p>
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
          <X className="size-4" />
        </Button>
      </div>
      <form onSubmit={submit} className="mt-4 space-y-3">
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Name</span>
          <Input
            autoFocus
            required
            className="h-9"
            placeholder="Payments API"
            value={name}
            onChange={(event) => {
              const value = event.target.value
              setName(value)
              if (slugAuto) setSlug(slugify(value))
            }}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Slug</span>
          <Input
            required
            className="h-9 font-mono"
            placeholder="payments-api"
            value={slug}
            onChange={(event) => {
              setSlugAuto(false)
              setSlug(event.target.value)
            }}
          />
        </label>
        {error && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" size="sm" disabled={busy}>
            {busy && <Loader2 className="size-3.5 animate-spin" />}{' '}
            {initialName ? 'Save' : 'Create project'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
}
