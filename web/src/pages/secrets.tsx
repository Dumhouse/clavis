import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import {
  ArrowLeft,
  Check,
  Copy,
  Eye,
  EyeOff,
  Loader2,
  Lock,
  MoreHorizontal,
  Folder,
  Pencil,
  Plus,
  Search,
  X,
} from 'lucide-react'
import { useCallback, useMemo, useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { useOrgEvents } from '@/lib/events'
import { EditSecretDialog, PasteEnvDialog } from '@/pages/secrets-dialogs'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']
type Secret = components['schemas']['SecretBody']

const envOrder: Record<string, number> = { development: 0, preview: 1, staging: 2, production: 3 }
const envClassNames: Record<string, string> = {
  development: 'env-development',
  preview: 'env-preview',
  staging: 'env-staging',
  production: 'env-production',
}
const envShort: Record<string, string> = {
  development: 'dev',
  preview: 'preview',
  staging: 'staging',
  production: 'prod',
}

function sortEnvs(envs: Env[] | null | undefined): Env[] {
  return [...(envs ?? [])].sort((a, b) => (envOrder[a.type] ?? 9) - (envOrder[b.type] ?? 9))
}

type CellState = { value: string; revealed: boolean; loading: boolean }
type EnvSecrets = { secrets: Secret[]; etag: string }
type PendingEdit = {
  env: string
  key: string
  value: string
  kind: string
  comment: string
  tags: string[]
  message: string
  existing: boolean
}

export function SecretsPage() {
  const { org } = useCurrentOrg()
  const params = useParams({ strict: false }) as { project?: string }
  const projectSlug = params.project ?? ''
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState('')
  const [folderFilter, setFolderFilter] = useState<string | null>(null)
  const [compare, setCompare] = useState(false)
  const [compareASlug, setCompareASlug] = useState('')
  const [compareBSlug, setCompareBSlug] = useState('')
  const [adding, setAdding] = useState(false)
  const [pasting, setPasting] = useState(false)
  const [editing, setEditing] = useState<{ env: string; secret: Secret } | null>(null)
  const [cells, setCells] = useState<Record<string, CellState>>({})
  const [edits, setEdits] = useState<Record<string, PendingEdit>>({})
  const [reviewOpen, setReviewOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [copied, setCopied] = useState<string | null>(null)
  const [toast, setToast] = useState<{ message: string; undo?: () => void } | null>(null)

  useOrgEvents(org?.slug, (event) => {
    if (event.project && event.project !== projectSlug) return
    if (event.type === 'secrets.changed') {
      void queryClient.invalidateQueries({ queryKey: ['secrets', org?.slug, projectSlug] })
    }
    if (event.type === 'environments.changed') {
      void queryClient.invalidateQueries({ queryKey: ['project', org?.slug, projectSlug] })
    }
  })

  const projectQuery = useQuery({
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

  const envs = useMemo(() => sortEnvs(projectQuery.data?.environments), [projectQuery.data])

  const secretQueries = useQueries({
    queries: envs.map((env) => ({
      queryKey: ['secrets', org?.slug, projectSlug, env.slug],
      enabled: Boolean(org),
      queryFn: async (): Promise<EnvSecrets> => {
        if (!org) return { secrets: [], etag: '' }
        const result = await api.GET(
          '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets',
          { params: { path: { org: org.slug, project: projectSlug, env: env.slug } } },
        )
        if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load secrets.'))
        return {
          secrets: result.data?.secrets ?? [],
          etag: result.response.headers.get('ETag') ?? '',
        }
      },
    })),
  })

  const secretsByEnv = useMemo(() => {
    const map = new Map<string, Secret[]>()
    envs.forEach((env, index) => {
      const result = secretQueries[index]
      map.set(env.slug, result?.data?.secrets ?? [])
    })
    return map
  }, [envs, secretQueries])

  const etagByEnv = useMemo(() => {
    const map = new Map<string, string>()
    envs.forEach((env, index) => {
      map.set(env.slug, secretQueries[index]?.data?.etag ?? '')
    })
    return map
  }, [envs, secretQueries])

  const canAdmin = org?.role === 'Owner' || org?.role === 'Admin'
  const writableEnvs = envs.filter((env) => !env.protected || canAdmin)
  const editCount = Object.keys(edits).length

  const ownerGroups = useMemo(() => {
    const map = new Map<string, string>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) {
        if (secret.owner_group_name) map.set(secret.key, secret.owner_group_name)
      }
    }
    return map
  }, [secretsByEnv])

  const inheritedSources = useMemo(() => {
    const map = new Map<string, string>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) {
        if (secret.inherited && secret.source_environment) {
          map.set(secret.key, secret.source_environment)
        }
      }
    }
    return map
  }, [secretsByEnv])

  const overriddenKeys = useMemo(() => {
    const set = new Set<string>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) {
        if (secret.overridden) set.add(secret.key)
      }
    }
    return set
  }, [secretsByEnv])

  const folders = useMemo(() => {
    const counts = new Map<string, number>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) {
        if (secret.folder) counts.set(secret.folder, (counts.get(secret.folder) ?? 0) + 1)
      }
    }
    return [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }, [secretsByEnv])

  const keys = useMemo(() => {
    const all = new Set<string>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) all.add(secret.key)
    }
    return [...all]
      .sort((a, b) => a.localeCompare(b))
      .filter((key) => key.toLowerCase().includes(filter.toLowerCase()))
      .filter((key) => {
        if (folderFilter === null) return true
        for (const list of secretsByEnv.values()) {
          if (list.some((secret) => secret.key === key && secret.folder === folderFilter))
            return true
        }
        return false
      })
  }, [secretsByEnv, filter, folderFilter])

  const compareA = compareASlug || envs[0]?.slug || ''
  const compareB = compareBSlug || envs[1]?.slug || envs[0]?.slug || ''

  const compareQueries = useQueries({
    queries: compare
      ? [compareA, compareB].map((slug) => ({
          queryKey: ['secrets-compare', org?.slug, projectSlug, slug],
          enabled: Boolean(org && slug),
          queryFn: async (): Promise<Secret[]> => {
            if (!org || !slug) return []
            const result = await api.GET(
              '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets',
              {
                params: {
                  path: { org: org.slug, project: projectSlug, env: slug },
                  query: { reveal: true, channel: 'ui' },
                },
              },
            )
            if (result.error)
              throw new Error(apiErrorMessage(result.error, 'Could not load comparison values.'))
            return result.data?.secrets ?? []
          },
        }))
      : [],
  })

  const compareRows = useMemo(() => {
    if (!compare) return []
    const left = compareQueries[0]?.data ?? []
    const right = compareQueries[1]?.data ?? []
    const leftMap = new Map(left.map((secret) => [secret.key, secret]))
    const rightMap = new Map(right.map((secret) => [secret.key, secret]))
    const allKeys = new Set([...leftMap.keys(), ...rightMap.keys()])
    return [...allKeys]
      .sort((a, b) => a.localeCompare(b))
      .map((key) => ({
        key,
        left: leftMap.get(key),
        right: rightMap.get(key),
        missing: !leftMap.has(key) || !rightMap.has(key),
        changed:
          leftMap.has(key) &&
          rightMap.has(key) &&
          leftMap.get(key)?.value !== rightMap.get(key)?.value,
      }))
      .filter((row) => row.missing || row.changed)
  }, [compare, compareQueries])

  const loading = projectQuery.isLoading || secretQueries.some((query) => query.isLoading)
  const loadError =
    projectQuery.error instanceof Error
      ? projectQuery.error.message
      : secretQueries.find((query) => query.error)?.error instanceof Error
        ? (secretQueries.find((query) => query.error)?.error as Error).message
        : null

  const secretsForEnv = useCallback(
    (envSlug: string) => secretsByEnv.get(envSlug) ?? [],
    [secretsByEnv],
  )

  function notify(message: string, undo?: () => void) {
    setToast(undo ? { message, undo } : { message })
    window.setTimeout(
      () => {
        setToast(null)
      },
      undo ? 8000 : 4000,
    )
  }

  const createSecret = useMutation({
    mutationFn: async (input: { env: string; key: string; value: string }) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets',
        {
          params: { path: { org: org.slug, project: projectSlug, env: input.env } },
          body: {
            key: input.key,
            value: input.value,
            kind: 'secret',
            message: 'Created from the web UI',
          },
        },
      )
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not create the secret.'))
      return result.data
    },
    onSuccess: async () => {
      setAdding(false)
      await queryClient.invalidateQueries({ queryKey: ['secrets', org?.slug, projectSlug] })
    },
  })

  async function toggleCell(env: Env, key: string) {
    if (!org) return
    const id = `${env.slug}:${key}`
    const current = cells[id]
    if (current?.revealed) {
      setCells((state) => {
        const next = { ...state }
        delete next[id]
        return next
      })
      return
    }
    let reason = 'Viewed in the web UI'
    if (env.type === 'production') {
      const entered = window.prompt('Why are you revealing this production value?')
      if (!entered || entered.trim() === '') return
      reason = entered.trim()
    }
    setCells((state) => ({
      ...state,
      [id]: { value: current?.value ?? '', revealed: false, loading: true },
    }))
    const result = await api.GET(
      '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}',
      {
        params: {
          path: { org: org.slug, project: projectSlug, env: env.slug, key },
          query: { reveal: true, reason },
        },
      },
    )
    if (result.error || !result.data) {
      setCells((state) => ({ ...state, [id]: { value: '', revealed: false, loading: false } }))
      return
    }
    setCells((state) => ({
      ...state,
      [id]: { value: result.data.value ?? '', revealed: true, loading: false },
    }))
  }

  async function startEdit(env: Env, key: string, secret?: Secret) {
    if (!org) return
    if (env.protected && org.role !== 'Owner' && org.role !== 'Admin') {
      notify('This environment is protected. Ask an Admin to make the change.')
      return
    }
    const id = `${env.slug}:${key}`
    if (edits[id]) return
    let value = cells[id]?.revealed ? (cells[id]?.value ?? '') : ''
    if (secret && !value) {
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}',
        {
          params: {
            path: { org: org.slug, project: projectSlug, env: env.slug, key },
            query: { reveal: true, reason: 'Editing in the web UI' },
          },
        },
      )
      if (result.error || !result.data) {
        notify(apiErrorMessage(result.error, 'Could not load the value.'))
        return
      }
      value = result.data.value ?? ''
    }
    setEdits((state) => ({
      ...state,
      [id]: {
        env: env.slug,
        key,
        value,
        kind: secret?.kind ?? 'secret',
        comment: secret?.comment ?? '',
        tags: secret?.tags ?? [],
        message: '',
        existing: Boolean(secret),
      },
    }))
  }

  function updateEdit(id: string, patch: Partial<PendingEdit>) {
    setEdits((state) => {
      const current = state[id]
      if (!current) return state
      return { ...state, [id]: { ...current, ...patch } }
    })
  }

  function cancelEdit(id: string) {
    setEdits((state) => {
      const next = { ...state }
      delete next[id]
      return next
    })
  }

  async function saveEdits() {
    if (!org || editCount === 0) return
    setSaving(true)
    setSaveError(null)
    const ifMatch: Record<string, string> = {}
    for (const edit of Object.values(edits)) {
      const etag = etagByEnv.get(edit.env)
      if (!etag) {
        setSaveError('An environment is still loading. Refresh and try again.')
        setSaving(false)
        return
      }
      ifMatch[edit.env] = etag
    }
    const result = await api.PATCH('/api/v1/orgs/{org}/projects/{project}/secrets:batch', {
      params: { path: { org: org.slug, project: projectSlug } },
      body: {
        changes: Object.values(edits).map((edit) => ({
          environment: edit.env,
          key: edit.key,
          value: edit.value,
          kind: edit.kind as 'secret' | 'variable' | 'file',
          comment: edit.comment,
          tags: edit.tags,
          message: edit.message || 'Saved from the grid',
        })),
        if_match: ifMatch,
        message: 'Saved from the grid',
      },
    })
    if (result.error) {
      setSaveError(apiErrorMessage(result.error, 'Could not save the changes.'))
      setSaving(false)
      return
    }
    setSaving(false)
    setEdits({})
    setReviewOpen(false)
    await queryClient.invalidateQueries({ queryKey: ['secrets', org.slug, projectSlug] })
    notify(`Saved ${String(editCount)} changes.`)
  }

  async function copy(id: string, value: string) {
    await navigator.clipboard.writeText(value)
    setCopied(id)
    window.setTimeout(() => {
      setCopied(null)
    }, 1500)
  }

  if (!projectQuery.data && !projectQuery.isLoading) {
    return (
      <div className="space-y-3">
        <Link
          to="/"
          className="inline-flex items-center gap-1.5 text-[12px] text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> Projects
        </Link>
        <div className="rounded-app bg-danger-bg px-4 py-3 text-[12px] text-danger">
          {loadError ?? 'Project not found.'}
        </div>
      </div>
    )
  }

  const project = projectQuery.data

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/"
          className="inline-flex items-center gap-1.5 text-[12px] text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> Projects
        </Link>
        <div className="mt-2 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-head text-[24px] font-semibold tracking-tight">
              {project?.name ?? projectSlug}
            </h1>
            <p className="mt-1 font-mono text-[11.5px] text-dim">{projectSlug}</p>
          </div>
          <div className="flex items-center gap-2">
            <Link
              to="/projects/$project/snapshots"
              params={{ project: projectSlug }}
              className="rounded-app-sm border border-border bg-surface px-3.5 py-2 text-[12px] font-medium text-text hover:bg-elev"
            >
              Snapshots
            </Link>
            <Link
              to="/projects/$project/access"
              params={{ project: projectSlug }}
              className="rounded-app-sm border border-border bg-surface px-3.5 py-2 text-[12px] font-medium text-text hover:bg-elev"
            >
              Access
            </Link>
            <Button
              variant="secondary"
              size="md"
              onClick={() => {
                setPasting(true)
              }}
              disabled={envs.length === 0}
            >
              Paste .env
            </Button>
            <Button
              variant="primary"
              size="md"
              onClick={() => {
                setAdding(true)
              }}
              disabled={writableEnvs.length === 0}
            >
              <Plus className="size-4" /> Add secret
            </Button>
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <div className="flex h-8 min-w-60 items-center gap-2 rounded-app-sm border border-border bg-surface px-2.5">
          <Search className="size-3.5 text-dim" />
          <input
            className="w-full bg-transparent text-[12px] text-text placeholder:text-dim focus:outline-none"
            placeholder="Filter keys…"
            value={filter}
            onChange={(event) => {
              setFilter(event.target.value)
            }}
          />
        </div>
        {envs.map((env) => (
          <span key={env.id} className={cn('env-chip', envClassNames[env.type])}>
            {envShort[env.type] ?? env.type}
            {env.protected && <Lock className="ml-1 size-2.5" />}
          </span>
        ))}
        <Button
          variant={compare ? 'primary' : 'secondary'}
          size="sm"
          onClick={() => {
            setCompare((value) => !value)
          }}
        >
          Compare
        </Button>
        {compare && envs.length >= 2 && (
          <div className="flex items-center gap-1.5 text-[11px] text-muted">
            <select
              className="h-7 rounded-app-sm border border-border bg-bg px-1.5 text-[11px] text-text focus:border-accent-line focus:outline-none"
              value={compareA}
              onChange={(event) => {
                setCompareASlug(event.target.value)
              }}
            >
              {envs.map((env) => (
                <option key={env.id} value={env.slug}>
                  {env.slug}
                </option>
              ))}
            </select>
            <span>vs</span>
            <select
              className="h-7 rounded-app-sm border border-border bg-bg px-1.5 text-[11px] text-text focus:border-accent-line focus:outline-none"
              value={compareB}
              onChange={(event) => {
                setCompareBSlug(event.target.value)
              }}
            >
              {envs.map((env) => (
                <option key={env.id} value={env.slug}>
                  {env.slug}
                </option>
              ))}
            </select>
          </div>
        )}
      </div>

      {loadError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-[12px] text-danger">
          {loadError}
        </div>
      )}

      {compare ? (
        <div className="overflow-x-auto rounded-app border border-border bg-surface">
          <table className="w-full min-w-[560px] border-collapse text-left">
            <thead>
              <tr className="border-b border-border text-[10.5px] uppercase tracking-[0.06em] text-dim">
                <th className="px-4 py-2.5 font-semibold">Key</th>
                <th className="px-4 py-2.5 font-semibold">{compareA}</th>
                <th className="px-4 py-2.5 font-semibold">{compareB}</th>
              </tr>
            </thead>
            <tbody>
              {compareRows.map((row) => (
                <tr key={row.key} className="border-b border-border-soft last:border-0">
                  <td className="px-4 py-2 font-mono text-[11.5px] text-text">{row.key}</td>
                  <td className="max-w-64 truncate px-4 py-2 font-mono text-[11px]">
                    {row.left ? (
                      <span className="text-muted">{row.left.value || '(empty)'}</span>
                    ) : (
                      <span className="text-danger">missing</span>
                    )}
                  </td>
                  <td className="max-w-64 truncate px-4 py-2 font-mono text-[11px]">
                    {row.right ? (
                      <span className="text-muted">{row.right.value || '(empty)'}</span>
                    ) : (
                      <span className="text-danger">missing</span>
                    )}
                  </td>
                </tr>
              ))}
              {compareRows.length === 0 && !compareQueries.some((query) => query.isLoading) && (
                <tr>
                  <td colSpan={3} className="px-4 py-10 text-center text-[12px] text-muted">
                    These environments have identical keys and values.
                  </td>
                </tr>
              )}
              {compareQueries.some((query) => query.isLoading) && (
                <tr>
                  <td colSpan={3} className="px-4 py-6 text-center text-[12px] text-muted">
                    <Loader2 className="mr-2 inline size-3.5 animate-spin" /> Comparing…
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="flex gap-4">
          <aside className="hidden w-44 shrink-0 md:block">
            <div className="rounded-app border border-border bg-surface p-2">
              <p className="px-1.5 pb-1 text-[10px] font-semibold uppercase tracking-[0.06em] text-dim">
                Folders
              </p>
              <button
                type="button"
                onClick={() => {
                  setFolderFilter(null)
                }}
                className={cn(
                  'flex h-7 w-full items-center gap-1.5 rounded px-1.5 text-left text-[11.5px]',
                  folderFilter === null
                    ? 'bg-hover text-text'
                    : 'text-muted hover:bg-hover hover:text-text',
                )}
              >
                <Folder className="size-3.5 text-dim" /> All keys
              </button>
              {folders.map(([path, count]) => (
                <button
                  key={path}
                  type="button"
                  onClick={() => {
                    setFolderFilter(path)
                  }}
                  className={cn(
                    'flex h-7 w-full items-center gap-1.5 rounded px-1.5 text-left font-mono text-[11px]',
                    folderFilter === path
                      ? 'bg-hover text-text'
                      : 'text-muted hover:bg-hover hover:text-text',
                  )}
                  title={path}
                >
                  <span className="truncate">{path.replace(/^\//, '')}</span>
                  <span className="ml-auto text-[10px] text-dim">{count}</span>
                </button>
              ))}
              {folders.length === 0 && (
                <p className="px-1.5 py-1 text-[10.5px] text-dim">No folders yet.</p>
              )}
            </div>
          </aside>
          <div className="min-w-0 flex-1 overflow-x-auto rounded-app border border-border bg-surface">
            {loading && keys.length === 0 ? (
              <div className="flex items-center gap-2 px-4 py-8 text-[12px] text-muted">
                <Loader2 className="size-4 animate-spin" /> Loading secrets…
              </div>
            ) : (
              <table className="w-full min-w-[640px] border-collapse text-left">
                <thead>
                  <tr className="border-b border-border">
                    <th className="sticky left-0 z-10 bg-surface px-4 py-2.5 text-[10.5px] font-semibold uppercase tracking-[0.06em] text-dim">
                      Key
                    </th>
                    {envs.map((env) => (
                      <th
                        key={env.id}
                        className="px-3 py-2.5 text-[10.5px] font-semibold uppercase tracking-[0.06em]"
                      >
                        <span className={cn('env-chip', envClassNames[env.type])}>
                          {envShort[env.type] ?? env.type}
                        </span>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {keys.map((key) => (
                    <tr key={key} className="border-b border-border-soft last:border-0">
                      <td className="sticky left-0 z-10 bg-surface px-4 py-2">
                        <span className="font-mono text-[11.5px] text-text">{key}</span>
                        {ownerGroups.get(key) && (
                          <span className="ml-2 rounded-full bg-accent-soft px-1.5 py-0.5 text-[9.5px] font-medium text-accent-text">
                            {ownerGroups.get(key)}
                          </span>
                        )}
                        {inheritedSources.get(key) && (
                          <span className="ml-1 rounded-full bg-elev px-1.5 py-0.5 font-mono text-[9px] text-dim">
                            from {inheritedSources.get(key)}
                          </span>
                        )}
                        {overriddenKeys.has(key) && (
                          <span className="ml-1 rounded-full bg-amber-bg px-1.5 py-0.5 text-[9px] font-semibold text-amber-fg">
                            override
                          </span>
                        )}
                      </td>
                      {envs.map((env) => {
                        const secret = secretsByEnv
                          .get(env.slug)
                          ?.find((candidate) => candidate.key === key)
                        const id = `${env.slug}:${key}`
                        const cell = cells[id]
                        const edit = edits[id]
                        if (edit) {
                          return (
                            <td key={env.id} className="px-3 py-2">
                              <input
                                autoFocus
                                className="h-7 w-full min-w-36 rounded border border-accent-line bg-bg px-2 font-mono text-[11px] text-text focus:outline-none"
                                value={edit.value}
                                onChange={(event) => {
                                  updateEdit(id, { value: event.target.value })
                                }}
                                onKeyDown={(event) => {
                                  if (event.key === 'Escape') cancelEdit(id)
                                  if (event.key === 'Enter') event.currentTarget.blur()
                                }}
                              />
                            </td>
                          )
                        }
                        if (!secret) {
                          return (
                            <td key={env.id} className="px-3 py-2">
                              <button
                                type="button"
                                onClick={() => {
                                  void startEdit(env, key)
                                }}
                                className="inline-flex h-6 items-center rounded border border-dashed border-border px-2 text-[10.5px] text-dim hover:border-accent-line hover:text-text"
                              >
                                missing
                              </button>
                            </td>
                          )
                        }
                        return (
                          <td key={env.id} className="px-3 py-2">
                            <div className="flex items-center gap-1">
                              <button
                                type="button"
                                onClick={() => {
                                  void toggleCell(env, key)
                                }}
                                className="inline-flex h-6 max-w-48 items-center gap-1.5 truncate rounded border border-border bg-elev px-2 font-mono text-[11px] text-muted hover:border-accent-line hover:text-text"
                                title={cell?.revealed ? 'Hide value' : 'Reveal value'}
                              >
                                {cell?.loading ? (
                                  <Loader2 className="size-3 animate-spin" />
                                ) : cell?.revealed ? (
                                  <EyeOff className="size-3 text-accent-text" />
                                ) : (
                                  <Eye className="size-3 text-dim" />
                                )}
                                <span className="truncate">
                                  {cell?.revealed ? cell.value : '••••••••'}
                                </span>
                              </button>
                              {cell?.revealed && (
                                <button
                                  type="button"
                                  onClick={() => {
                                    void copy(id, cell.value)
                                  }}
                                  className="inline-flex size-6 items-center justify-center rounded border border-border text-dim hover:text-text"
                                  aria-label="Copy value"
                                >
                                  {copied === id ? (
                                    <Check className="size-3 text-accent-text" />
                                  ) : (
                                    <Copy className="size-3" />
                                  )}
                                </button>
                              )}
                              <button
                                type="button"
                                onClick={() => {
                                  void startEdit(env, key, secret)
                                }}
                                className="inline-flex size-6 items-center justify-center rounded border border-transparent text-dim hover:border-border hover:text-text"
                                aria-label={`Edit ${key} in ${env.slug}`}
                              >
                                <Pencil className="size-3" />
                              </button>
                              <button
                                type="button"
                                onClick={() => {
                                  setEditing({ env: env.slug, secret })
                                }}
                                className="inline-flex size-6 items-center justify-center rounded border border-transparent text-dim hover:border-border hover:text-text"
                                aria-label={`Details for ${key} in ${env.slug}`}
                              >
                                <MoreHorizontal className="size-3" />
                              </button>
                            </div>
                          </td>
                        )
                      })}
                    </tr>
                  ))}
                  {keys.length === 0 && !loading && (
                    <tr>
                      <td
                        colSpan={envs.length + 1}
                        className="px-4 py-10 text-center text-[12px] text-muted"
                      >
                        No secrets yet. Add one to get started.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )}

      {editCount > 0 && (
        <div className="flex flex-wrap items-center gap-3 rounded-app border border-accent-line bg-accent-soft px-4 py-2.5">
          <span className="text-[12px] font-medium text-accent-text">
            {editCount} unsaved {editCount === 1 ? 'change' : 'changes'}
          </span>
          <span className="text-[11px] text-muted">
            Nothing is written until you review and save.
          </span>
          <div className="ml-auto flex gap-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setEdits({})
              }}
            >
              Discard
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={() => {
                setReviewOpen(true)
              }}
            >
              Review & save
            </Button>
          </div>
        </div>
      )}

      <p className="text-[11px] text-dim">
        Reveals are written to the audit log with the key's blind index and the reason. Values never
        appear in logs.
      </p>

      {adding && (
        <AddSecretDialog
          envs={writableEnvs}
          busy={createSecret.isPending}
          error={createSecret.error instanceof Error ? createSecret.error.message : null}
          onClose={() => {
            setAdding(false)
          }}
          onSubmit={(input) => {
            createSecret.mutate(input, {
              onSuccess: () => {
                notify(`Created ${input.key}.`)
              },
            })
          }}
        />
      )}

      {pasting && org && (
        <PasteEnvDialog
          orgSlug={org.slug}
          projectSlug={projectSlug}
          envs={writableEnvs}
          secretsForEnv={secretsForEnv}
          onClose={() => {
            setPasting(false)
          }}
          onDone={(message) => {
            setPasting(false)
            notify(message)
            void queryClient.invalidateQueries({ queryKey: ['secrets', org.slug, projectSlug] })
          }}
        />
      )}

      {editing && org && (
        <EditSecretDialog
          orgSlug={org.slug}
          projectSlug={projectSlug}
          env={editing.env}
          secret={editing.secret}
          readOnly={Boolean(
            envs.find((candidate) => candidate.slug === editing.env)?.protected && !canAdmin,
          )}
          onClose={() => {
            setEditing(null)
          }}
          onDone={(message) => {
            setEditing(null)
            notify(message)
            void queryClient.invalidateQueries({ queryKey: ['secrets', org.slug, projectSlug] })
          }}
          onDeleted={(key, value) => {
            notify(`Deleted ${key}.`, () => {
              if (!org) return
              void api
                .POST('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets', {
                  params: { path: { org: org.slug, project: projectSlug, env: editing.env } },
                  body: { key, value, kind: 'secret', message: 'Restored from undo' },
                })
                .then(() => {
                  notify(`Restored ${key}.`)
                  void queryClient.invalidateQueries({
                    queryKey: ['secrets', org.slug, projectSlug],
                  })
                })
            })
          }}
        />
      )}

      {reviewOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
          <div className="flex max-h-[85vh] w-full max-w-2xl flex-col rounded-app border border-border bg-surface p-5 shadow-app">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 className="font-head text-[17px] font-semibold">Review changes</h2>
                <p className="mt-0.5 text-[12px] text-muted">
                  {editCount} {editCount === 1 ? 'change' : 'changes'} will be written in one atomic
                  batch per environment.
                </p>
              </div>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => {
                  setReviewOpen(false)
                }}
                aria-label="Close"
              >
                <X className="size-4" />
              </Button>
            </div>
            <div className="mt-3 max-h-80 overflow-y-auto rounded-app-sm border border-border">
              <table className="w-full text-left text-[11.5px]">
                <thead className="sticky top-0 bg-surface">
                  <tr className="border-b border-border text-[10px] uppercase tracking-[0.06em] text-dim">
                    <th className="px-2.5 py-1.5 font-semibold">Environment</th>
                    <th className="px-2.5 py-1.5 font-semibold">Key</th>
                    <th className="px-2.5 py-1.5 font-semibold">Change</th>
                    <th className="px-2.5 py-1.5 font-semibold">Value</th>
                  </tr>
                </thead>
                <tbody>
                  {Object.entries(edits).map(([id, edit]) => (
                    <tr key={id} className="border-b border-border-soft last:border-0">
                      <td className="px-2.5 py-1.5 text-muted">{edit.env}</td>
                      <td className="px-2.5 py-1.5 font-mono text-text">{edit.key}</td>
                      <td className="px-2.5 py-1.5">
                        <span
                          className={cn(
                            'rounded px-1.5 py-0.5 text-[10px] font-medium',
                            edit.existing
                              ? 'bg-amber-bg text-amber-fg'
                              : 'bg-success-bg text-success',
                          )}
                        >
                          {edit.existing ? 'update' : 'new'}
                        </span>
                      </td>
                      <td className="max-w-64 truncate px-2.5 py-1.5 font-mono text-dim">
                        {edit.value || '(empty)'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {saveError && (
              <p className="mt-3 rounded-app-sm bg-danger-bg px-2.5 py-2 text-[11.5px] text-danger">
                {saveError}
              </p>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setReviewOpen(false)
                }}
              >
                Keep editing
              </Button>
              <Button
                variant="primary"
                size="sm"
                disabled={saving}
                onClick={() => {
                  void saveEdits()
                }}
              >
                {saving && <Loader2 className="size-3.5 animate-spin" />}
                Save {editCount} changes
              </Button>
            </div>
          </div>
        </div>
      )}

      {toast && (
        <div className="fixed bottom-4 right-4 z-50 flex items-center gap-3 rounded-app-sm border border-border bg-elev px-3 py-2 text-[12px] text-text shadow-app">
          <span>{toast.message}</span>
          {toast.undo && (
            <button
              type="button"
              className="font-medium text-accent-text hover:underline"
              onClick={() => {
                toast.undo?.()
                setToast(null)
              }}
            >
              Undo
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function AddSecretDialog({
  envs,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  envs: Env[]
  busy: boolean
  error: string | null
  onClose: () => void
  onSubmit: (input: {
    env: string
    key: string
    value: string
    folder: string
    kind: string
    message: string
  }) => void
}) {
  const [env, setEnv] = useState(envs[0]?.slug ?? '')
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [folder, setFolder] = useState('')
  const [kind, setKind] = useState('secret')
  const [message, setMessage] = useState('')

  function submit(event: FormEvent) {
    event.preventDefault()
    onSubmit({ env, key, value, folder, kind, message })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="w-full max-w-lg rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[17px] font-semibold">Add secret</h2>
            <p className="mt-0.5 text-[12px] text-muted">
              The value is encrypted before it reaches the database.
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <X className="size-4" />
          </Button>
        </div>
        <form onSubmit={submit} className="mt-4 space-y-3">
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">Environment</span>
            <select
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
              value={env}
              onChange={(event) => {
                setEnv(event.target.value)
              }}
            >
              {envs.map((candidate) => (
                <option key={candidate.id} value={candidate.slug}>
                  {candidate.name} ({envShort[candidate.type] ?? candidate.type})
                </option>
              ))}
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">Key</span>
            <input
              autoFocus
              required
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[12px] text-text focus:border-accent-line focus:outline-none"
              placeholder="STRIPE_SECRET_KEY"
              value={key}
              onChange={(event) => {
                setKey(event.target.value.toUpperCase().replace(/[^A-Z0-9_.-]/g, '_'))
              }}
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">Kind</span>
            <select
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
              value={kind}
              onChange={(event) => {
                setKind(event.target.value)
              }}
            >
              <option value="secret">Secret (masked, reveal audited)</option>
              <option value="variable">Variable (plain configuration)</option>
              <option value="file">File (masked, mount/download)</option>
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">
              Folder (optional)
            </span>
            <input
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[12px] text-text focus:border-accent-line focus:outline-none"
              placeholder="/backend/db"
              value={folder}
              onChange={(event) => {
                setFolder(event.target.value)
              }}
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">Value</span>
            <textarea
              required
              rows={3}
              className="w-full rounded-app-sm border border-border bg-bg px-2.5 py-2 font-mono text-[12px] text-text focus:border-accent-line focus:outline-none"
              placeholder="sk_live_…"
              value={value}
              onChange={(event) => {
                setValue(event.target.value)
              }}
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[11.5px] font-medium text-muted">Change message</span>
            <input
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
              placeholder="Why this exists"
              value={message}
              onChange={(event) => {
                setMessage(event.target.value)
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
              {busy && <Loader2 className="size-3.5 animate-spin" />} Save secret
            </Button>
          </div>
        </form>
      </div>
    </div>
  )
}
