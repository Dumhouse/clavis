import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Check,
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
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { useOrgEvents } from '@/lib/events'
import { cn } from '@/lib/utils'

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
        !window.confirm(
          `Delete ${project.name}? Every environment and secret in it will be removed.`,
        )
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
        <h1 className="font-head text-[20px] font-semibold">No organization yet</h1>
        <p className="mt-1 text-[12.5px] text-muted">
          Create an organization through the API or finish setup again.
        </p>
      </section>
    )
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="font-head text-[24px] font-semibold tracking-tight">Projects</h1>
          <p className="mt-1 text-[13px] text-muted">
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
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-[12px] text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading projects…
        </div>
      )}
      {projects.isError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-[12px] text-danger">
          {projects.error instanceof Error ? projects.error.message : 'Could not load projects.'}
        </div>
      )}
      {projects.data && projects.data.length === 0 && (
        <section className="rounded-app border border-dashed border-border bg-panel p-8 text-center">
          <div className="mx-auto flex size-10 items-center justify-center rounded-app-sm bg-elev">
            <Folder className="size-5 text-dim" />
          </div>
          <h2 className="mt-3 text-[13.5px] font-semibold">Create your first project</h2>
          <p className="mx-auto mt-1 max-w-sm text-[12px] text-muted">
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

      <CliCard />

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

function GettingStarted({ projectCount }: { projectCount: number }) {
  const steps = [
    { label: 'Create your organization', done: true },
    { label: 'Create a project', done: projectCount > 0 },
    { label: 'Connect the CLI', done: false },
  ]
  const completed = steps.filter((step) => step.done).length

  return (
    <section className="rounded-app border border-border bg-surface p-4 shadow-app">
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex size-9 shrink-0 items-center justify-center rounded-app-sm bg-accent-soft">
          <Sparkles className="size-4 text-accent-text" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-[13.5px] font-semibold">Finish setting up Clavis</h2>
            <span className="text-[11px] text-dim">
              {completed} of {steps.length} done
            </span>
          </div>
          <div className="mt-2 h-1 overflow-hidden rounded-full bg-elev">
            <div
              className="h-full rounded-full bg-accent transition-[width] duration-200"
              style={{ width: `${String((completed / steps.length) * 100)}%` }}
            />
          </div>
          <div className="mt-3 flex flex-wrap gap-x-5 gap-y-2">
            {steps.map((step) => (
              <span key={step.label} className="flex items-center gap-1.5 text-[12px]">
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
        <Button variant="secondary" size="sm">
          Connect CLI
        </Button>
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
            <h3 className="truncate text-[13.5px] font-semibold">{project.name}</h3>
            <p className="mt-0.5 text-[12px] text-muted">{project.slug}</p>
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

        <div className="mt-4 flex items-center gap-4 border-t border-border-soft pt-3 text-[11.5px] text-dim">
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

function CliCard(): ReactNode {
  return (
    <section className="grid gap-3 rounded-app border border-border bg-panel p-4 md:grid-cols-[1fr_auto] md:items-center">
      <div className="flex items-start gap-3">
        <div className="flex size-9 shrink-0 items-center justify-center rounded-app-sm bg-elev">
          <TerminalSquare className="size-4 text-muted" />
        </div>
        <div>
          <h2 className="text-[13px] font-semibold">Get secrets into your app</h2>
          <p className="mt-0.5 text-[12px] text-muted">
            Run any command with secrets injected and output masked.
          </p>
        </div>
      </div>
      <code className="rounded-app-sm border border-border bg-bg px-3 py-2 font-mono text-[12px] text-accent-text">
        clavis run -- npm run dev
      </code>
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

  function submit(event: FormEvent) {
    event.preventDefault()
    onSubmit({ name, slug: slug || slugify(name) })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="w-full max-w-md rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[17px] font-semibold">{title}</h2>
            <p className="mt-0.5 text-[12px] text-muted">
              Dev, staging, and production environments are created automatically.
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <X className="size-4" />
          </Button>
        </div>
        <form onSubmit={submit} className="mt-4 space-y-3">
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">Name</span>
            <input
              autoFocus
              required
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
              placeholder="Payments API"
              value={name}
              onChange={(event) => {
                setName(event.target.value)
                if (!slug || slug === slugify(initialName)) setSlug(slugify(event.target.value))
              }}
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">Slug</span>
            <input
              required
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[12px] text-text focus:border-accent-line focus:outline-none"
              placeholder="payments-api"
              value={slug}
              onChange={(event) => {
                setSlug(event.target.value)
              }}
            />
          </label>
          {error && (
            <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[11.5px] text-danger">
              {error}
            </p>
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
      </div>
    </div>
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
