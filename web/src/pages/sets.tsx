import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Layers, Loader2, Plus, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type SharedSet = components['schemas']['SharedSetBody']
type Project = components['schemas']['ProjectBody']

const inputClass =
  'h-8 rounded-app-sm border border-border bg-bg px-2 text-[12px] text-text focus:border-accent-line focus:outline-none'

export function SharedSetsPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [importSet, setImportSet] = useState('')
  const [importProject, setImportProject] = useState('')
  const [importEnv, setImportEnv] = useState('')
  const [error, setError] = useState<string | null>(null)

  const sets = useQuery({
    queryKey: ['sets', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/sets', {
        params: { path: { org: org.slug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load shared sets.'))
      return result.data?.sets ?? []
    },
  })

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

  const project = useQuery({
    queryKey: ['project', org?.slug, importProject],
    enabled: Boolean(org && importProject),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}', {
        params: { path: { org: org.slug, project: importProject } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load project.'))
      return result.data
    },
  })

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['sets', org?.slug] })
    await queryClient.invalidateQueries({ queryKey: ['secrets', org?.slug] })
  }

  const create = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/sets', {
        params: { path: { org: org.slug } },
        body: { name: name.trim(), slug: slug.trim() },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not create the set.'))
    },
    onSuccess: async () => {
      setName('')
      setSlug('')
      setError(null)
      await invalidate()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not create the set.')
    },
  })

  const remove = useMutation({
    mutationFn: async (set: SharedSet) => {
      if (!org) throw new Error('No organization.')
      if (!window.confirm(`Delete “${set.name}” and all its imports?`)) return
      const result = await api.DELETE('/api/v1/orgs/{org}/sets/{slug}', {
        params: { path: { org: org.slug, slug: set.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not delete the set.'))
    },
    onSuccess: invalidate,
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not delete the set.')
    },
  })

  const importAction = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      if (!importSet || !importProject || !importEnv)
        throw new Error('Choose a set, project, and environment.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/sets/{slug}',
        {
          params: {
            path: { org: org.slug, project: importProject, env: importEnv, slug: importSet },
          },
        },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not import the set.'))
    },
    onSuccess: async () => {
      setError(null)
      await invalidate()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not import the set.')
    },
  })

  const envs = project.data?.environments ?? []

  return (
    <div className="space-y-5">
      <header>
        <div className="flex items-center gap-2">
          <Layers className="size-5 text-muted" />
          <h1 className="font-head text-[24px] font-semibold tracking-tight">Shared sets</h1>
        </div>
        <p className="mt-1 text-[13px] text-muted">
          Org-level secret sets imported by environments. Local values always win; imported sets
          resolve through <code className="font-mono">{'${set:KEY}'}</code>.
        </p>
      </header>

      {error && (
        <p className="rounded-app-sm bg-danger-bg px-3 py-2 text-[11.5px] text-danger">{error}</p>
      )}

      <section className="rounded-app border border-border bg-surface px-4 py-3">
        <form
          onSubmit={(event: FormEvent) => {
            event.preventDefault()
            create.mutate()
          }}
          className="flex flex-wrap items-end gap-2"
        >
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">Name</span>
            <input
              required
              className={inputClass}
              value={name}
              onChange={(event) => {
                setName(event.target.value)
              }}
              placeholder="Shared API keys"
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">Slug</span>
            <input
              className={inputClass}
              value={slug}
              onChange={(event) => {
                setSlug(event.target.value)
              }}
              placeholder="shared-api"
            />
          </label>
          <Button type="submit" variant="primary" size="sm" disabled={create.isPending}>
            <Plus className="size-3.5" /> Create set
          </Button>
        </form>
      </section>

      <section className="rounded-app border border-border bg-surface px-4 py-3">
        <p className="mb-2 text-[12px] font-semibold">Import into an environment</p>
        <div className="flex flex-wrap items-end gap-2">
          <select
            className={inputClass}
            value={importSet}
            onChange={(event) => {
              setImportSet(event.target.value)
            }}
          >
            <option value="">Choose set…</option>
            {(sets.data ?? []).map((set) => (
              <option key={set.id} value={set.slug}>
                {set.name}
              </option>
            ))}
          </select>
          <select
            className={inputClass}
            value={importProject}
            onChange={(event) => {
              setImportProject(event.target.value)
              setImportEnv('')
            }}
          >
            <option value="">Choose project…</option>
            {(projects.data ?? []).map((item: Project) => (
              <option key={item.id} value={item.slug}>
                {item.name}
              </option>
            ))}
          </select>
          <select
            className={inputClass}
            value={importEnv}
            onChange={(event) => {
              setImportEnv(event.target.value)
            }}
          >
            <option value="">Choose environment…</option>
            {envs.map((env) => (
              <option key={env.id} value={env.slug}>
                {env.name}
              </option>
            ))}
          </select>
          <Button
            variant="secondary"
            size="sm"
            disabled={importAction.isPending || !importSet || !importProject || !importEnv}
            onClick={() => {
              importAction.mutate()
            }}
          >
            {importAction.isPending && <Loader2 className="size-3.5 animate-spin" />} Import
          </Button>
        </div>
      </section>

      <div className="space-y-4">
        {(sets.data ?? []).map((set) => (
          <SetCard
            key={set.id}
            set={set}
            orgSlug={org?.slug ?? ''}
            onDelete={() => {
              remove.mutate(set)
            }}
            onChanged={() => {
              void invalidate()
            }}
            onError={setError}
          />
        ))}
        {sets.isLoading && (
          <p className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-5 text-[12px] text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading shared sets…
          </p>
        )}
        {!sets.isLoading && (sets.data?.length ?? 0) === 0 && (
          <p className="rounded-app border border-dashed border-border bg-panel px-4 py-6 text-[12px] text-muted">
            No shared sets yet.
          </p>
        )}
      </div>
    </div>
  )
}

function SetCard({
  set,
  orgSlug,
  onDelete,
  onChanged,
  onError,
}: {
  set: SharedSet
  orgSlug: string
  onDelete: () => void
  onChanged: () => void
  onError: (message: string) => void
}) {
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [kind, setKind] = useState('secret')

  async function addItem(event: FormEvent) {
    event.preventDefault()
    const result = await api.PUT('/api/v1/orgs/{org}/sets/{slug}/items/{key}', {
      params: { path: { org: orgSlug, slug: set.slug, key: key.trim() } },
      body: { value, kind: kind as 'secret' | 'variable' | 'file' },
    })
    if (result.error) {
      onError(apiErrorMessage(result.error, 'Could not save the value.'))
      return
    }
    setKey('')
    setValue('')
    onChanged()
  }

  async function removeItem(itemKey: string) {
    const result = await api.DELETE('/api/v1/orgs/{org}/sets/{slug}/items/{key}', {
      params: { path: { org: orgSlug, slug: set.slug, key: itemKey } },
    })
    if (result.error) {
      onError(apiErrorMessage(result.error, 'Could not remove the value.'))
      return
    }
    onChanged()
  }

  return (
    <section className="rounded-app border border-border bg-surface">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <h2 className="text-[13px] font-semibold">{set.name}</h2>
        <code className="font-mono text-[10.5px] text-dim">{set.slug}</code>
        <span className="rounded-full bg-elev px-1.5 text-[10.5px] text-muted">
          {set.items?.length ?? 0} keys
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="ml-auto"
          aria-label="Delete set"
          onClick={onDelete}
        >
          <Trash2 className="size-3.5" />
        </Button>
      </div>
      <div className="border-b border-border-soft px-4 py-2 text-[11px] text-dim">
        {(set.imports ?? []).length === 0
          ? 'Not imported by any environment.'
          : `Used by ${(set.imports ?? [])
              .map((item) => `${item.project_slug ?? ''}/${item.environment_slug ?? ''}`)
              .join(', ')}`}
      </div>
      <div className="divide-y divide-border-soft">
        {(set.items ?? []).map((item) => (
          <div key={item.key} className="flex items-center gap-3 px-4 py-2">
            <code className="flex-1 font-mono text-[11.5px]">{item.key}</code>
            <span className="text-[10.5px] text-dim">{item.kind}</span>
            <span className="text-[10.5px] text-dim">v{item.version}</span>
            <Button
              variant="ghost"
              size="icon"
              aria-label={`Remove ${item.key}`}
              onClick={() => {
                void removeItem(item.key)
              }}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        ))}
      </div>
      <form
        onSubmit={(event) => {
          void addItem(event)
        }}
        className="flex flex-wrap items-end gap-2 border-t border-border px-4 py-3"
      >
        <input
          required
          className={inputClass + ' font-mono'}
          placeholder="KEY"
          value={key}
          onChange={(event) => {
            setKey(event.target.value.toUpperCase().replace(/[^A-Z0-9_.-]/g, '_'))
          }}
        />
        <input
          required
          className={inputClass + ' min-w-48 flex-1'}
          placeholder="Value"
          value={value}
          onChange={(event) => {
            setValue(event.target.value)
          }}
        />
        <select
          className={inputClass}
          value={kind}
          onChange={(event) => {
            setKind(event.target.value)
          }}
        >
          <option value="secret">Secret</option>
          <option value="variable">Variable</option>
          <option value="file">File</option>
        </select>
        <Button type="submit" variant="secondary" size="sm">
          <Plus className="size-3.5" /> Add
        </Button>
      </form>
    </section>
  )
}
