import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Braces,
  Eye,
  EyeOff,
  FileText,
  KeyRound,
  Layers,
  Link2,
  Loader2,
  Pencil,
  Plus,
  Trash2,
  Upload,
  X,
} from 'lucide-react'
import { useState, type FormEvent, type ReactNode } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { confirmAction } from '@/components/ui/dialogs'
import { Input, Notice, Select, Textarea } from '@/components/ui/field'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'
import { formatDate as formatDay } from '@/lib/format'

type SharedSet = components['schemas']['SharedSetBody']
type Item = components['schemas']['SharedSetItemBody']
type Project = components['schemas']['ProjectBody']
type Kind = 'secret' | 'variable' | 'file'

/** The slug the server accepts: lowercase letters, digits and hyphens. */
function slugify(name: string) {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
}

const groups: Array<{ kind: Kind; title: string; icon: ReactNode; empty: string }> = [
  {
    kind: 'secret',
    title: 'Secrets',
    icon: <KeyRound className="size-3.5" />,
    empty: 'API keys, passwords, tokens. Masked everywhere.',
  },
  {
    kind: 'variable',
    title: 'Variables',
    icon: <Braces className="size-3.5" />,
    empty: 'Plain settings such as hosts and ports. Readable in logs.',
  },
  {
    kind: 'file',
    title: 'Files',
    icon: <FileText className="size-3.5" />,
    empty: 'Multi-line values: certificates, keys, config files.',
  },
]

/*
 * Shared sets: keys kept once and used by many environments. Using a set in
 * an environment is a live link (the API calls it an import): the
 * environment reads the set's current values every time, so a change to the
 * set applies everywhere at once. Nothing is copied.
 */
export function SharedSetsPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
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

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['sets', org?.slug] })
    await queryClient.invalidateQueries({ queryKey: ['set-values', org?.slug] })
    await queryClient.invalidateQueries({ queryKey: ['secrets', org?.slug] })
  }

  const create = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/sets', {
        params: { path: { org: org.slug } },
        body: { name: name.trim(), slug: slug.trim() || slugify(name) },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not create the set.'))
      return result.data
    },
    onSuccess: async (set) => {
      setName('')
      setSlug('')
      setCreating(false)
      setError(null)
      setSelected(set.slug)
      await invalidate()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not create the set.')
    },
  })

  const list = sets.data ?? []
  const current = list.find((set) => set.slug === selected) ?? list[0]

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <Layers className="size-5 text-muted" />
            <h1 className="font-head text-2xl font-semibold tracking-tight">Shared sets</h1>
          </div>
          <p className="mt-1 max-w-3xl text-base text-muted">
            Keys you keep once and use in many environments, such as a shared API key or an SMTP
            login. Environments that use a set read its current values: change a value here and they
            all get it at once. Nothing is copied, and a key the environment defines itself wins
            over the set.
          </p>
        </div>
        <Button
          variant="primary"
          onClick={() => {
            setCreating(true)
          }}
        >
          <Plus className="size-4" /> New set
        </Button>
      </header>

      {error && (
        <Notice>
          <span className="flex items-start gap-2">
            <span className="flex-1">{error}</span>
            <button
              type="button"
              aria-label="Dismiss"
              onClick={() => {
                setError(null)
              }}
            >
              <X className="size-3.5" />
            </button>
          </span>
        </Notice>
      )}

      {creating && (
        <section className="rounded-app border border-accent-line bg-surface px-4 py-3">
          <form
            onSubmit={(event: FormEvent) => {
              event.preventDefault()
              create.mutate()
            }}
            className="flex flex-wrap items-end gap-2"
          >
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Name</span>
              <Input
                required
                autoFocus
                value={name}
                onChange={(event) => {
                  setName(event.target.value)
                }}
                placeholder="Shared API keys"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">
                Slug (for the CLI; can’t change later)
              </span>
              <Input
                value={slug}
                onChange={(event) => {
                  setSlug(event.target.value)
                }}
                placeholder={slugify(name) || 'shared-api'}
              />
            </label>
            <Button type="submit" variant="primary" size="sm" disabled={create.isPending}>
              {create.isPending && <Loader2 className="size-3.5 animate-spin" />} Create set
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setCreating(false)
              }}
            >
              Cancel
            </Button>
          </form>
        </section>
      )}

      {sets.isLoading && (
        <p className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-5 text-sm text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading shared sets…
        </p>
      )}
      {sets.error && <Notice>{sets.error.message}</Notice>}
      {!sets.isLoading && list.length === 0 && !creating && (
        <div className="rounded-app border border-dashed border-border bg-panel px-4 py-6 text-sm text-muted">
          <p>No shared sets yet. Create one, add the keys, then use it in environments.</p>
          <Button
            variant="primary"
            size="sm"
            className="mt-3"
            onClick={() => {
              setCreating(true)
            }}
          >
            <Plus className="size-3.5" /> New set
          </Button>
        </div>
      )}

      {list.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-[16rem_minmax(0,1fr)]">
          <nav aria-label="Shared sets" className="space-y-1">
            {list.map((set) => {
              const active = set.slug === current?.slug
              const keys = set.items?.length ?? 0
              const uses = set.imports?.length ?? 0
              return (
                <button
                  key={set.id}
                  type="button"
                  aria-current={active ? 'true' : undefined}
                  onClick={() => {
                    setSelected(set.slug)
                  }}
                  className={cn(
                    'block w-full rounded-app-sm border px-3 py-2 text-left',
                    active
                      ? 'border-accent-line bg-accent-soft'
                      : 'border-transparent hover:bg-hover',
                  )}
                >
                  <span className="block truncate text-sm font-medium">{set.name}</span>
                  <span className="block text-2xs text-dim">
                    {keys} {keys === 1 ? 'key' : 'keys'} ·{' '}
                    {uses === 0
                      ? 'not used yet'
                      : `${String(uses)} ${uses === 1 ? 'environment' : 'environments'}`}
                  </span>
                </button>
              )
            })}
          </nav>
          {current && (
            <SetDetail
              key={current.id}
              set={current}
              orgSlug={org?.slug ?? ''}
              projects={projects.data ?? []}
              onChanged={() => {
                void invalidate()
              }}
              onDeleted={() => {
                setSelected(null)
                void invalidate()
              }}
              onError={setError}
            />
          )}
        </div>
      )}
    </div>
  )
}

function SetDetail({
  set,
  orgSlug,
  projects,
  onChanged,
  onDeleted,
  onError,
}: {
  set: SharedSet
  orgSlug: string
  projects: Project[]
  onChanged: () => void
  onDeleted: () => void
  onError: (message: string) => void
}) {
  const [renaming, setRenaming] = useState(false)
  const [newName, setNewName] = useState(set.name)
  const [linking, setLinking] = useState(false)
  const [reveal, setReveal] = useState(false)
  const items = set.items ?? []
  const imports = set.imports ?? []

  // Values are fetched only on request, and need permission to read values.
  const values = useQuery({
    queryKey: ['set-values', orgSlug, set.slug],
    enabled: reveal,
    queryFn: async () => {
      const result = await api.GET('/api/v1/orgs/{org}/sets/{slug}', {
        params: { path: { org: orgSlug, slug: set.slug }, query: { reveal: true } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not show values.'))
      return Object.fromEntries((result.data?.items ?? []).map((item) => [item.key, item.value]))
    },
  })

  async function rename(event: FormEvent) {
    event.preventDefault()
    const result = await api.PATCH('/api/v1/orgs/{org}/sets/{slug}', {
      params: { path: { org: orgSlug, slug: set.slug } },
      body: { name: newName.trim() },
    })
    if (result.error) {
      onError(apiErrorMessage(result.error, 'Could not rename the set.'))
      return
    }
    setRenaming(false)
    onChanged()
  }

  async function remove() {
    const used = imports.length
    if (
      !(await confirmAction(
        used > 0
          ? `Delete “${set.name}”? ${String(used)} environment${used === 1 ? '' : 's'} lose its keys right away.`
          : `Delete “${set.name}”?`,
      ))
    )
      return
    const result = await api.DELETE('/api/v1/orgs/{org}/sets/{slug}', {
      params: { path: { org: orgSlug, slug: set.slug } },
    })
    if (result.error) {
      onError(apiErrorMessage(result.error, 'Could not delete the set.'))
      return
    }
    onDeleted()
  }

  async function unlink(project: string, env: string) {
    if (!(await confirmAction(`Stop using “${set.name}” in ${project} / ${env}?`))) return
    const result = await api.DELETE(
      '/api/v1/orgs/{org}/projects/{project}/environments/{env}/sets/{slug}',
      { params: { path: { org: orgSlug, project, env, slug: set.slug } } },
    )
    if (result.error) {
      onError(apiErrorMessage(result.error, 'Could not unlink the set.'))
      return
    }
    onChanged()
  }

  return (
    <section className="min-w-0 rounded-app border border-border bg-surface">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        {renaming ? (
          <form
            className="flex flex-1 flex-wrap items-center gap-2"
            onSubmit={(event) => {
              void rename(event)
            }}
          >
            <Input
              autoFocus
              required
              maxLength={100}
              className="h-8 max-w-xs"
              aria-label="Set name"
              value={newName}
              onChange={(event) => {
                setNewName(event.target.value)
              }}
            />
            <Button type="submit" variant="primary" size="sm">
              Save
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setRenaming(false)
                setNewName(set.name)
              }}
            >
              Cancel
            </Button>
          </form>
        ) : (
          <>
            <h2 className="text-base font-semibold">{set.name}</h2>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Rename set"
              onClick={() => {
                setRenaming(true)
              }}
            >
              <Pencil className="size-3.5" />
            </Button>
          </>
        )}
        <code className="font-mono text-2xs text-dim" title="Slug, used by the CLI">
          {set.slug}
        </code>
        <div className="ml-auto flex items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            disabled={items.length === 0}
            onClick={() => {
              setReveal((state) => !state)
            }}
          >
            {reveal ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
            {reveal ? 'Hide values' : 'Show values'}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            aria-label={`Delete ${set.name}`}
            onClick={() => {
              void remove()
            }}
          >
            <Trash2 className="size-3.5" />
          </Button>
        </div>
      </div>

      <div className="space-y-2 border-b border-border-soft px-4 py-2.5">
        <div className="flex flex-wrap items-center gap-1.5 text-xs">
          <span className="text-dim">Used by</span>
          {imports.length === 0 && <span className="text-muted">no environment yet</span>}
          {imports.map((item) => (
            <span
              key={item.id}
              className="inline-flex items-center gap-1 rounded-full border border-border-soft bg-elev py-0.5 pl-2 pr-1 font-mono text-2xs"
            >
              {item.project_slug ?? '?'} / {item.environment_slug ?? '?'}
              <button
                type="button"
                className="rounded-full p-0.5 text-dim hover:bg-hover hover:text-text"
                aria-label={`Stop using in ${item.project_slug ?? ''} / ${item.environment_slug ?? ''}`}
                onClick={() => {
                  void unlink(item.project_slug ?? '', item.environment_slug ?? '')
                }}
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
          {!linking && (
            <Button
              variant="ghost"
              size="sm"
              className="h-6 px-2 text-xs"
              onClick={() => {
                setLinking(true)
              }}
            >
              <Link2 className="size-3" /> Use in an environment
            </Button>
          )}
        </div>
        {linking && (
          <LinkPicker
            orgSlug={orgSlug}
            set={set}
            projects={projects}
            onDone={() => {
              setLinking(false)
              onChanged()
            }}
            onCancel={() => {
              setLinking(false)
            }}
            onError={onError}
          />
        )}
      </div>

      {values.error && <Notice className="m-4">{values.error.message}</Notice>}

      <div className="divide-y divide-border">
        {groups.map((group) => (
          <KindGroup
            key={group.kind}
            group={group}
            set={set}
            orgSlug={orgSlug}
            items={items.filter((item) => item.kind === group.kind)}
            values={reveal ? values.data : undefined}
            used={imports.length}
            onChanged={onChanged}
            onError={onError}
          />
        ))}
      </div>
    </section>
  )
}

function KindGroup({
  group,
  set,
  orgSlug,
  items,
  values,
  used,
  onChanged,
  onError,
}: {
  group: (typeof groups)[number]
  set: SharedSet
  orgSlug: string
  items: Item[]
  values: Record<string, string | undefined> | undefined
  used: number
  onChanged: () => void
  onError: (message: string) => void
}) {
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const singular = group.title.slice(0, -1).toLowerCase()

  async function save(key: string, value: string) {
    const result = await api.PUT('/api/v1/orgs/{org}/sets/{slug}/items/{key}', {
      params: { path: { org: orgSlug, slug: set.slug, key: key.trim() } },
      body: { value, kind: group.kind },
    })
    if (result.error) {
      onError(apiErrorMessage(result.error, 'Could not save the value.'))
      return false
    }
    onChanged()
    return true
  }

  async function remove(key: string) {
    if (
      !(await confirmAction(
        used > 0
          ? `Remove ${key}? ${String(used)} environment${used === 1 ? '' : 's'} using this set lose it right away.`
          : `Remove ${key}?`,
      ))
    )
      return
    const result = await api.DELETE('/api/v1/orgs/{org}/sets/{slug}/items/{key}', {
      params: { path: { org: orgSlug, slug: set.slug, key } },
    })
    if (result.error) {
      onError(apiErrorMessage(result.error, 'Could not remove the value.'))
      return
    }
    onChanged()
  }

  return (
    <div className="px-4 py-3">
      <div className="mb-1.5 flex items-center gap-2">
        <span className="text-muted">{group.icon}</span>
        <h3 className="text-sm font-semibold">{group.title}</h3>
        <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">{items.length}</span>
        {!adding && (
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto h-7 px-2 text-xs"
            onClick={() => {
              setAdding(true)
            }}
          >
            <Plus className="size-3" /> Add {singular}
          </Button>
        )}
      </div>
      {items.length === 0 && !adding && <p className="text-xs text-dim">{group.empty}</p>}
      <div className="divide-y divide-border-soft">
        {items.map((item) =>
          editing === item.key ? (
            <ItemForm
              key={item.key}
              kind={group.kind}
              fixedKey={item.key}
              initial={values?.[item.key] ?? ''}
              used={used}
              onCancel={() => {
                setEditing(null)
              }}
              onSave={async (key, value) => {
                if (await save(key, value)) setEditing(null)
              }}
            />
          ) : (
            <div key={item.key} className="flex items-center gap-3 py-1.5">
              <code className="w-48 shrink-0 truncate font-mono text-xs">{item.key}</code>
              <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted">
                {values ? (values[item.key] ?? '') : '••••••'}
              </span>
              <span className="text-2xs text-dim">v{item.version}</span>
              <span className="hidden text-2xs text-dim sm:inline">
                {formatDay(item.updated_at)}
              </span>
              <Button
                variant="ghost"
                size="icon"
                aria-label={`Change ${item.key}`}
                onClick={() => {
                  setEditing(item.key)
                }}
              >
                <Pencil className="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                aria-label={`Remove ${item.key}`}
                onClick={() => {
                  void remove(item.key)
                }}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          ),
        )}
      </div>
      {adding && (
        <ItemForm
          kind={group.kind}
          initial=""
          used={used}
          onCancel={() => {
            setAdding(false)
          }}
          onSave={async (key, value) => {
            if (await save(key, value)) setAdding(false)
          }}
        />
      )}
    </div>
  )
}

function ItemForm({
  kind,
  fixedKey,
  initial,
  used,
  onSave,
  onCancel,
}: {
  kind: Kind
  fixedKey?: string
  initial: string
  used: number
  onSave: (key: string, value: string) => Promise<void>
  onCancel: () => void
}) {
  const [key, setKey] = useState(fixedKey ?? '')
  const [value, setValue] = useState(initial)
  const [busy, setBusy] = useState(false)
  const [fileName, setFileName] = useState('')
  const [fileNote, setFileNote] = useState('')

  // Text files are kept as they are; anything else is stored as base64.
  async function loadFile(file: File) {
    if (file.size > 256 * 1024) {
      setFileNote(`${file.name} is larger than 256 KB; keep big files outside Clavis.`)
      return
    }
    const bytes = new Uint8Array(await file.arrayBuffer())
    try {
      setValue(new TextDecoder('utf-8', { fatal: true }).decode(bytes))
      setFileNote('')
    } catch {
      let binary = ''
      for (const byte of bytes) binary += String.fromCharCode(byte)
      setValue(btoa(binary))
      setFileNote('Binary file: stored as base64. Decode it where you use it (base64 -d).')
    }
    setFileName(file.name)
    if (!fixedKey && !key) {
      setKey(
        file.name
          .toUpperCase()
          .replace(/[^A-Z0-9_]+/g, '_')
          .replace(/^_+|_+$/g, ''),
      )
    }
  }

  return (
    <form
      className="my-1.5 space-y-2 rounded-app-sm border border-accent-line bg-panel p-2.5"
      onSubmit={(event) => {
        event.preventDefault()
        setBusy(true)
        void onSave(key, value).finally(() => {
          setBusy(false)
        })
      }}
    >
      <div className="flex flex-wrap items-start gap-2">
        <Input
          required
          autoFocus={!fixedKey}
          readOnly={Boolean(fixedKey)}
          mono
          className="h-8 w-48 text-xs"
          placeholder="KEY"
          aria-label="Key"
          value={key}
          onChange={(event) => {
            setKey(event.target.value.toUpperCase().replace(/[^A-Z0-9_.-]/g, '_'))
          }}
        />
        {kind === 'file' ? (
          <div className="min-w-48 flex-1 space-y-1.5">
            <label className="flex cursor-pointer items-center gap-2 rounded-app-sm border border-dashed border-border px-3 py-2 text-xs text-muted hover:border-accent-line hover:text-text">
              <Upload className="size-3.5" />
              {fileName ? (
                <span>
                  <span className="font-mono text-text">{fileName}</span> loaded, change it below or
                  choose another file
                </span>
              ) : (
                <span>Choose a file (certificate, key, config), or paste its contents below</span>
              )}
              <input
                type="file"
                className="sr-only"
                onChange={(event) => {
                  const file = event.target.files?.[0]
                  if (file) void loadFile(file)
                  event.target.value = ''
                }}
              />
            </label>
            {fileNote && <p className="text-2xs text-amber-fg">{fileNote}</p>}
            <Textarea
              required
              mono
              autoFocus={Boolean(fixedKey)}
              rows={5}
              className="w-full text-xs"
              placeholder={fixedKey && !initial ? 'New contents' : 'Contents'}
              aria-label="Value"
              spellCheck={false}
              value={value}
              onChange={(event) => {
                setValue(event.target.value)
              }}
            />
          </div>
        ) : (
          <Input
            required
            autoFocus={Boolean(fixedKey)}
            mono
            className="h-8 min-w-48 flex-1 text-xs"
            type={kind === 'secret' ? 'password' : 'text'}
            autoComplete="off"
            placeholder={fixedKey && !initial ? 'New value' : 'Value'}
            aria-label="Value"
            value={value}
            onChange={(event) => {
              setValue(event.target.value)
            }}
          />
        )}
      </div>
      <div className="flex items-center gap-2">
        <Button type="submit" variant="primary" size="sm" disabled={busy}>
          {busy && <Loader2 className="size-3.5 animate-spin" />}
          {fixedKey ? 'Save new version' : 'Add'}
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
        {used > 0 && (
          <span className="text-2xs text-dim">
            Applies at once to {used} {used === 1 ? 'environment' : 'environments'}.
          </span>
        )}
      </div>
    </form>
  )
}

function LinkPicker({
  orgSlug,
  set,
  projects,
  onDone,
  onCancel,
  onError,
}: {
  orgSlug: string
  set: SharedSet
  projects: Project[]
  onDone: () => void
  onCancel: () => void
  onError: (message: string) => void
}) {
  const [project, setProject] = useState(projects.length === 1 ? (projects[0]?.slug ?? '') : '')
  const [envs, setEnvs] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  const detail = useQuery({
    queryKey: ['project', orgSlug, project],
    enabled: Boolean(project),
    queryFn: async () => {
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}', {
        params: { path: { org: orgSlug, project } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load project.'))
      return result.data
    },
  })
  const linked = new Set(
    (set.imports ?? [])
      .filter((item) => item.project_slug === project)
      .map((item) => item.environment_slug ?? ''),
  )
  const available = (detail.data?.environments ?? []).filter((env) => !linked.has(env.slug))

  async function link() {
    setBusy(true)
    for (const env of envs) {
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/sets/{slug}',
        { params: { path: { org: orgSlug, project, env, slug: set.slug } } },
      )
      if (result.error) {
        onError(apiErrorMessage(result.error, `Could not use the set in ${env}.`))
        break
      }
    }
    setBusy(false)
    onDone()
  }

  return (
    <div className="space-y-2 rounded-app-sm border border-border-soft bg-panel p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={project}
          aria-label="Project"
          onChange={(event) => {
            setProject(event.target.value)
            setEnvs([])
          }}
        >
          <option value="">Choose project…</option>
          {projects.map((item) => (
            <option key={item.id} value={item.slug}>
              {item.name}
            </option>
          ))}
        </Select>
        {detail.isLoading && <Loader2 className="size-3.5 animate-spin text-muted" />}
        {available.length > 1 && (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs"
            onClick={() => {
              setEnvs(envs.length === available.length ? [] : available.map((env) => env.slug))
            }}
          >
            {envs.length === available.length ? 'None' : 'All environments'}
          </Button>
        )}
        {available.map((env) => {
          const on = envs.includes(env.slug)
          return (
            <label
              key={env.id}
              className="flex cursor-pointer items-center gap-1.5 rounded-app-sm border border-border-soft px-2 py-1 text-xs"
            >
              <input
                type="checkbox"
                checked={on}
                onChange={() => {
                  setEnvs((state) =>
                    on ? state.filter((slug) => slug !== env.slug) : [...state, env.slug],
                  )
                }}
              />
              {env.name}
            </label>
          )
        })}
        {project && !detail.isLoading && available.length === 0 && (
          <span className="text-xs text-muted">Every environment of this project uses it.</span>
        )}
      </div>
      <div className="flex items-center gap-2">
        <Button
          variant="primary"
          size="sm"
          disabled={busy || envs.length === 0}
          onClick={() => {
            void link()
          }}
        >
          {busy && <Loader2 className="size-3.5 animate-spin" />} Use in{' '}
          {envs.length === 1 ? '1 environment' : `${String(envs.length)} environments`}
        </Button>
        <Button variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </div>
  )
}
