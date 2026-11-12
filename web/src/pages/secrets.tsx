import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import {
  ArrowLeft,
  Check,
  Copy,
  Eye,
  EyeOff,
  Folder,
  GitCompare,
  KeyRound,
  Loader2,
  Lock,
  MoreHorizontal,
  PanelRight,
  Pencil,
  Plus,
  Search,
  Sparkles,
  Upload,
  X,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button, buttonVariants } from '@/components/ui/button'
import { askRevealReason } from '@/components/ui/dialogs'
import { Field, Input, Notice, Select, Textarea } from '@/components/ui/field'
import { InfoHint } from '@/components/ui/info-hint'
import { Menu } from '@/components/ui/menu'
import { Modal, ModalFooter, ModalHeader } from '@/components/ui/modal'
import { toast } from '@/components/ui/toast'
import { apiErrorMessage } from '@/lib/api-error'
import { rememberProject } from '@/lib/last-project'
import { takeOpenKey } from '@/lib/open-key'
import { useCurrentOrg } from '@/lib/org'
import { useOrgEvents } from '@/lib/events'
import { GenerateSecretDialog } from '@/pages/generate-dialog'
import { ImportSecretsDialog } from '@/pages/import-dialog'
import { envClassNames, envShort, SecretSheet } from '@/pages/secret-sheet'
import { isRequired, validateSchemaValue, type SchemaEntry } from '@/lib/schema'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']
type Secret = components['schemas']['SecretBody']
type Overlay = components['schemas']['OverlayBody']
type OverlaySecret = components['schemas']['OverlaySecretBody']

const envOrder: Record<string, number> = { development: 0, preview: 1, staging: 2, production: 3 }
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
  const navigate = useNavigate()
  useEffect(() => {
    if (projectSlug) rememberProject(projectSlug)
  }, [projectSlug])
  // Open a key requested from the command palette once its data is loaded.
  const [pendingKey, setPendingKey] = useState<string | null>(() => takeOpenKey())
  useEffect(() => {
    const handler = (event: Event) => {
      const key = (event as CustomEvent<string>).detail
      takeOpenKey()
      setPendingKey(key)
    }
    window.addEventListener('clavis:open-key', handler)
    return () => {
      window.removeEventListener('clavis:open-key', handler)
    }
  }, [])
  const [filter, setFilter] = useState('')
  const [folderFilter, setFolderFilter] = useState<string | null>(null)
  const [compare, setCompare] = useState(false)
  const [overlaySlug, setOverlaySlug] = useState('')
  const [compareASlug, setCompareASlug] = useState('')
  const [compareBSlug, setCompareBSlug] = useState('')
  const [adding, setAdding] = useState(false)
  const [pasting, setPasting] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [detail, setDetail] = useState<{ key: string; env: string } | null>(null)
  const [cells, setCells] = useState<Record<string, CellState>>({})
  const [edits, setEdits] = useState<Record<string, PendingEdit>>({})
  const [reviewOpen, setReviewOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [copied, setCopied] = useState<string | null>(null)
  // Page shortcuts advertised in the hint bar: N add, P import, / filter.
  const filterRef = useRef<HTMLInputElement>(null)
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const typing =
        target?.tagName === 'INPUT' ||
        target?.tagName === 'TEXTAREA' ||
        target?.tagName === 'SELECT' ||
        target?.isContentEditable === true
      if (typing || event.metaKey || event.ctrlKey || event.altKey) return
      if (document.querySelector('dialog[open]')) return
      const key = event.key.toLowerCase()
      if (key === 'n') {
        event.preventDefault()
        setAdding(true)
      } else if (key === 'p') {
        event.preventDefault()
        setPasting(true)
      } else if (event.key === '/') {
        event.preventDefault()
        filterRef.current?.focus()
      }
    }
    window.addEventListener('keydown', handler)
    return () => {
      window.removeEventListener('keydown', handler)
    }
  }, [])

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

  const schemaQuery = useQuery({
    queryKey: ['schema', org?.slug, projectSlug],
    enabled: Boolean(org && projectSlug),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}/schema', {
        params: { path: { org: org.slug, project: projectSlug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load the schema.'))
      return result.data
    },
  })

  const overlayLists = useQueries({
    queries: envs.map((env) => ({
      queryKey: ['overlays', org?.slug, projectSlug, env.slug],
      enabled: Boolean(org && projectSlug),
      queryFn: async (): Promise<Overlay[]> => {
        if (!org) return []
        const result = await api.GET(
          '/api/v1/orgs/{org}/projects/{project}/environments/{env}/overlays',
          { params: { path: { org: org.slug, project: projectSlug, env: env.slug } } },
        )
        if (result.error) return []
        return result.data?.overlays ?? []
      },
    })),
  })

  const overlaySlugs = useMemo(() => {
    const set = new Set<string>()
    for (const result of overlayLists) {
      for (const overlay of result.data ?? []) set.add(overlay.slug)
    }
    return [...set].sort()
  }, [overlayLists])

  const overlayQueries = useQueries({
    queries: envs.map((env) => ({
      queryKey: ['overlay-secrets', org?.slug, projectSlug, env.slug, overlaySlug],
      enabled: Boolean(org && projectSlug && overlaySlug),
      queryFn: async (): Promise<OverlaySecret[]> => {
        if (!org || !overlaySlug) return []
        const result = await api.GET(
          '/api/v1/orgs/{org}/projects/{project}/environments/{env}/overlays/{overlay}/secrets',
          {
            params: {
              path: {
                org: org.slug,
                project: projectSlug,
                env: env.slug,
                overlay: overlaySlug,
              },
              query: { reveal: true },
            },
          },
        )
        if (result.error) return []
        return result.data?.secrets ?? []
      },
    })),
  })

  const overlayByEnv = useMemo(() => {
    const map = new Map<string, Map<string, string>>()
    envs.forEach((env, index) => {
      const values = new Map<string, string>()
      for (const secret of overlayQueries[index]?.data ?? []) {
        values.set(secret.key, secret.value ?? '')
      }
      map.set(env.slug, values)
    })
    return map
  }, [envs, overlayQueries])

  const schemaByKey = useMemo(() => {
    const map = new Map<string, SchemaEntry>()
    for (const entry of schemaQuery.data?.schema ?? []) map.set(entry.key, entry)
    return map
  }, [schemaQuery.data])

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

  const expiringKeys = useMemo(() => {
    const map = new Map<string, string>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) {
        if (secret.expires_at) map.set(secret.key, secret.expires_at)
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

  useEffect(() => {
    if (!pendingKey || envs.length === 0 || secretQueries.some((query) => query.isLoading)) return
    const env =
      envs.find((candidate) =>
        secretsByEnv.get(candidate.slug)?.some((secret) => secret.key === pendingKey),
      ) ?? envs[0]
    setDetail({ key: pendingKey, env: env?.slug ?? '' })
    setPendingKey(null)
  }, [pendingKey, envs, secretsByEnv, secretQueries])

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

  function notify(message: string, tone: 'success' | 'error' | 'info' = 'success') {
    toast(message, { tone })
  }

  const createSecret = useMutation({
    mutationFn: async (input: {
      env: string
      key: string
      value: string
      kind: string
      folder: string
      message: string
    }) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets',
        {
          params: { path: { org: org.slug, project: projectSlug, env: input.env } },
          body: {
            key: input.key,
            value: input.value,
            kind: input.kind as 'secret' | 'variable' | 'file',
            folder: input.folder,
            message: input.message || 'Created from the web UI',
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
      const entered = await askRevealReason(`${key} in ${env.slug}`, true)
      if (!entered) return
      reason = entered
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
      notify(apiErrorMessage(result.error, 'Could not reveal the value.'), 'error')
      return
    }
    setCells((state) => ({
      ...state,
      [id]: { value: result.data.value ?? '', revealed: true, loading: false },
    }))
  }

  // Inline edits never reveal: secrets are replaced with a new value, while
  // variables (already plain in the list) start from their current value.
  function startEdit(env: Env, key: string, secret?: Secret) {
    if (!org) return
    if (env.protected && !canAdmin) {
      notify('This environment is protected. Ask an Admin to make the change.', 'info')
      return
    }
    const id = `${env.slug}:${key}`
    if (edits[id]) return
    const value =
      secret?.kind === 'variable'
        ? (secret.value ?? '')
        : cells[id]?.revealed
          ? (cells[id]?.value ?? '')
          : ''
    setEdits((state) => ({
      ...state,
      [id]: {
        env: env.slug,
        key,
        value,
        kind: secret?.kind ?? schemaByKey.get(key)?.kind ?? 'secret',
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
    for (const edit of Object.values(edits)) {
      if (edit.value === '' && edit.kind !== 'variable') {
        setSaveError(`${edit.key} in ${edit.env}: enter a value or discard this change.`)
        return
      }
      const problem = validateSchemaValue(schemaByKey.get(edit.key), edit.value)
      if (problem) {
        setSaveError(`${edit.key} in ${edit.env}: ${problem}`)
        return
      }
    }
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

  const project = projectQuery.data
  const prodEdits = Object.values(edits).filter(
    (edit) => envs.find((env) => env.slug === edit.env)?.type === 'production',
  ).length

  if (!project && !projectQuery.isLoading) {
    return (
      <div className="space-y-3">
        <BackLink />
        <Notice>{loadError ?? 'Project not found.'}</Notice>
      </div>
    )
  }

  const projectLinks: Array<[string, string]> = [
    ['/projects/$project/environments', 'Environments'],
    ['/projects/$project/schema', 'Schema'],
    ['/projects/$project/promote', 'Promote'],
    ['/projects/$project/rotation', 'Rotation'],
    ['/projects/$project/dynamic', 'Dynamic secrets'],
    ['/projects/$project/snapshots', 'Snapshots'],
    ['/projects/$project/access', 'Access'],
  ]

  return (
    <div className="space-y-4">
      <div>
        <BackLink />
        <div className="mt-2 flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <h1 className="font-head text-2xl font-semibold tracking-tight">
              {project?.name ?? projectSlug}
            </h1>
            <p className="mt-1 text-sm text-muted">
              <span className="font-mono text-dim">{projectSlug}</span>
              {' · '}
              {keys.length} {keys.length === 1 ? 'key' : 'keys'} across {envs.length}{' '}
              {envs.length === 1 ? 'environment' : 'environments'}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Menu
              label="More actions"
              triggerClassName={cn(buttonVariants({ variant: 'secondary', size: 'md' }), 'px-2.5')}
              trigger={<MoreHorizontal className="size-4" />}
              items={[
                {
                  label: 'Generate a value',
                  icon: <Sparkles />,
                  disabled: writableEnvs.length === 0,
                  onSelect: () => {
                    setGenerating(true)
                  },
                },
                {
                  label: 'Import .env or JSON',
                  icon: <Upload />,
                  disabled: envs.length === 0,
                  onSelect: () => {
                    setPasting(true)
                  },
                },
                'separator',
                ...projectLinks.map(([to, label]) => ({
                  label,
                  onSelect: () => {
                    void navigate({ to, params: { project: projectSlug } })
                  },
                })),
              ]}
            />
            <Button
              variant="secondary"
              className="hidden sm:inline-flex"
              onClick={() => {
                setPasting(true)
              }}
              disabled={envs.length === 0}
            >
              <Upload className="size-4" /> Import
            </Button>
            <Button
              variant="primary"
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
        <label className="flex h-8 min-w-0 flex-1 items-center gap-2 rounded-app-sm border border-border bg-surface px-2.5 focus-within:border-accent-line sm:max-w-72">
          <Search className="size-3.5 shrink-0 text-dim" aria-hidden />
          <span className="sr-only">Filter keys</span>
          <Input
            className="bg-transparent"
            ref={filterRef}
            placeholder="Filter keys…  /"
            value={filter}
            onChange={(event) => {
              setFilter(event.target.value)
            }}
          />
          {filter && (
            <button
              type="button"
              aria-label="Clear filter"
              className="text-dim hover:text-text"
              onClick={() => {
                setFilter('')
              }}
            >
              <X className="size-3.5" />
            </button>
          )}
        </label>
        <Button
          variant={compare ? 'primary' : 'secondary'}
          size="sm"
          aria-pressed={compare}
          onClick={() => {
            setCompare((value) => !value)
          }}
        >
          <GitCompare className="size-3.5" /> Compare
        </Button>
        {overlaySlugs.length > 0 && (
          <>
            <Select
              size="sm"
              aria-label="Tenant or region overlay"
              value={overlaySlug}
              onChange={(event) => {
                setOverlaySlug(event.target.value)
              }}
            >
              <option value="">No overlay</option>
              {overlaySlugs.map((slug) => (
                <option key={slug} value={slug}>
                  overlay: {slug}
                </option>
              ))}
            </Select>
            <InfoHint label="What overlays do" align="right">
              Overlays resolve per-region or per-tenant values without duplicating every key. Values
              defined in the overlay win; missing keys fall back to the environment.
            </InfoHint>
          </>
        )}
        {compare && envs.length >= 2 && (
          <div className="flex items-center gap-1.5 text-xs text-muted">
            <Select
              size="sm"
              aria-label="Compare from"
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
            </Select>
            <span>vs</span>
            <Select
              size="sm"
              aria-label="Compare to"
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
            </Select>
          </div>
        )}
      </div>

      {loadError && <Notice>{loadError}</Notice>}

      {compare ? (
        <div
          className="overflow-x-auto rounded-app border border-border bg-surface"
          role="region"
          aria-label="Environment comparison"
          tabIndex={0}
        >
          <table className="w-full min-w-[560px] border-collapse text-left">
            <caption className="sr-only">
              Keys that differ between {compareA} and {compareB}
            </caption>
            <thead>
              <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
                <th scope="col" className="px-4 py-2.5 font-semibold">
                  Key
                </th>
                <th scope="col" className="px-4 py-2.5 font-semibold">
                  {compareA}
                </th>
                <th scope="col" className="px-4 py-2.5 font-semibold">
                  {compareB}
                </th>
              </tr>
            </thead>
            <tbody>
              {compareRows.map((row) => (
                <tr key={row.key} className="border-b border-border-soft last:border-0">
                  <td className="px-4 py-2 font-mono text-xs text-text">{row.key}</td>
                  {[row.left, row.right].map((side, index) => (
                    <td key={index} className="max-w-64 truncate px-4 py-2 font-mono text-xs">
                      {side ? (
                        <span className="text-muted">{side.value || '(empty)'}</span>
                      ) : (
                        <span className="text-danger">missing</span>
                      )}
                    </td>
                  ))}
                </tr>
              ))}
              {compareRows.length === 0 && !compareQueries.some((query) => query.isLoading) && (
                <tr>
                  <td colSpan={3} className="px-4 py-10 text-center text-sm text-muted">
                    These environments have identical keys and values.
                  </td>
                </tr>
              )}
              {compareQueries.some((query) => query.isLoading) && (
                <tr>
                  <td colSpan={3} className="px-4 py-6 text-center text-sm text-muted">
                    <Loader2 className="mr-2 inline size-3.5 animate-spin" /> Comparing…
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="flex gap-4">
          {folders.length > 0 && (
            <nav aria-label="Folders" className="hidden w-44 shrink-0 md:block">
              <div className="rounded-app border border-border bg-surface p-2">
                <p className="px-1.5 pb-1 text-2xs font-semibold uppercase tracking-[0.06em] text-dim">
                  Folders
                </p>
                <FolderButton
                  active={folderFilter === null}
                  onClick={() => {
                    setFolderFilter(null)
                  }}
                >
                  <Folder className="size-3.5 text-dim" /> All keys
                </FolderButton>
                {folders.map(([path, count]) => (
                  <FolderButton
                    key={path}
                    active={folderFilter === path}
                    onClick={() => {
                      setFolderFilter(path)
                    }}
                  >
                    <span className="truncate font-mono text-xs" title={path}>
                      {path.replace(/^\//, '')}
                    </span>
                    <span className="ml-auto text-2xs text-dim">{count}</span>
                  </FolderButton>
                ))}
              </div>
            </nav>
          )}
          <div
            className="min-w-0 flex-1 overflow-x-auto rounded-app border border-border bg-surface"
            role="region"
            aria-label="Secrets grid"
            tabIndex={0}
          >
            {loading && keys.length === 0 ? (
              <div className="flex items-center gap-2 px-4 py-8 text-sm text-muted">
                <Loader2 className="size-4 animate-spin" /> Loading secrets…
              </div>
            ) : keys.length === 0 && !filter && folderFilter === null ? (
              <EmptyState
                canWrite={writableEnvs.length > 0}
                onAdd={() => {
                  setAdding(true)
                }}
                onImport={() => {
                  setPasting(true)
                }}
              />
            ) : (
              <table className="w-full border-collapse text-left">
                <caption className="sr-only">Secrets by environment</caption>
                <thead>
                  <tr className="border-b border-border">
                    <th
                      scope="col"
                      className="sticky left-0 z-10 bg-surface px-4 py-2.5 text-2xs font-semibold uppercase tracking-[0.06em] text-dim"
                    >
                      Key
                    </th>
                    {envs.map((env) => (
                      <th key={env.id} scope="col" className="px-3 py-2.5">
                        <span className={cn('env-chip', envClassNames[env.type])}>
                          {envShort[env.type] ?? env.type}
                          {env.protected && (
                            <Lock className="ml-1 size-2.5" aria-label="protected" />
                          )}
                        </span>
                      </th>
                    ))}
                    {overlaySlug &&
                      envs.map((env) => (
                        <th
                          key={env.id + '-overlay'}
                          scope="col"
                          className="px-3 py-2.5 text-2xs font-semibold uppercase tracking-[0.06em] text-dim"
                        >
                          {overlaySlug}:{envShort[env.type] ?? env.type}
                        </th>
                      ))}
                  </tr>
                </thead>
                <tbody>
                  {keys.map((key) => (
                    <tr
                      key={key}
                      className="group border-b border-border-soft last:border-0 hover:bg-hover-soft"
                    >
                      <th
                        scope="row"
                        className="sticky left-0 z-10 max-w-72 bg-surface px-4 py-2 text-left font-normal group-hover:bg-[color-mix(in_srgb,var(--surface),var(--text)_2%)]"
                      >
                        <button
                          type="button"
                          className="max-w-full truncate font-mono text-xs text-text hover:text-accent-text hover:underline"
                          title="Open details and history"
                          onClick={() => {
                            const first =
                              envs.find((env) =>
                                secretsByEnv.get(env.slug)?.some((secret) => secret.key === key),
                              ) ?? envs[0]
                            setDetail({ key, env: first?.slug ?? '' })
                          }}
                        >
                          {key}
                        </button>
                        <KeyBadges
                          schemaType={schemaByKey.get(key)?.type}
                          owner={ownerGroups.get(key)}
                          inheritedFrom={inheritedSources.get(key)}
                          overridden={overriddenKeys.has(key)}
                          expires={expiringKeys.get(key)}
                        />
                      </th>
                      {envs.map((env) => {
                        const secret = secretsByEnv
                          .get(env.slug)
                          ?.find((candidate) => candidate.key === key)
                        const id = `${env.slug}:${key}`
                        const cell = cells[id]
                        const edit = edits[id]
                        const schemaEntry = schemaByKey.get(key)
                        if (edit) {
                          const validation =
                            edit.value === '' ? null : validateSchemaValue(schemaEntry, edit.value)
                          return (
                            <td key={env.id} className="px-3 py-2">
                              <div className="space-y-1">
                                <Input
                                  autoFocus
                                  aria-label={`New value for ${key} in ${env.slug}`}
                                  aria-invalid={validation ? true : undefined}
                                  placeholder={edit.existing ? 'New value' : 'Value'}
                                  autoComplete="off"
                                  spellCheck={false}
                                  className={cn(
                                    'h-7 w-full min-w-36 rounded border border-accent-line bg-bg px-2 font-mono text-xs text-text focus:outline-none',
                                    validation && 'border-danger',
                                  )}
                                  value={edit.value}
                                  onChange={(event) => {
                                    updateEdit(id, { value: event.target.value })
                                  }}
                                  onKeyDown={(event) => {
                                    if (event.key === 'Escape') cancelEdit(id)
                                    if (event.key === 'Enter') event.currentTarget.blur()
                                  }}
                                />
                                {validation && (
                                  <p className="max-w-48 text-2xs text-danger">{validation}</p>
                                )}
                              </div>
                            </td>
                          )
                        }
                        if (!secret) {
                          const required = isRequired(schemaEntry, env.type)
                          const writable = !env.protected || canAdmin
                          return (
                            <td key={env.id} className="px-3 py-2">
                              <button
                                type="button"
                                disabled={!writable}
                                onClick={() => {
                                  startEdit(env, key)
                                }}
                                className={cn(
                                  'inline-flex h-6 items-center gap-1 rounded border border-dashed px-2 text-2xs disabled:cursor-not-allowed disabled:opacity-60',
                                  required
                                    ? 'border-danger/60 text-danger hover:border-danger'
                                    : 'border-border text-dim hover:border-accent-line hover:text-text',
                                )}
                                aria-label={`Add ${key} to ${env.slug}`}
                                title={required ? `Required in ${env.type}` : undefined}
                              >
                                <Plus className="size-3" />
                                {required ? 'Required' : 'Add'}
                              </button>
                            </td>
                          )
                        }
                        const plain = secret.kind === 'variable'
                        const shown = plain
                          ? (secret.value ?? '')
                          : cell?.revealed
                            ? cell.value
                            : null
                        return (
                          <td key={env.id} className="px-3 py-2">
                            <div className="flex items-center gap-1">
                              {plain ? (
                                <span
                                  className="inline-flex h-6 max-w-48 items-center truncate rounded border border-transparent px-1 font-mono text-xs text-muted"
                                  title={shown ?? ''}
                                >
                                  {shown || <span className="text-dim">(empty)</span>}
                                </span>
                              ) : (
                                <button
                                  type="button"
                                  onClick={() => {
                                    void toggleCell(env, key)
                                  }}
                                  className="inline-flex h-6 max-w-48 items-center gap-1.5 truncate rounded border border-border bg-elev px-2 font-mono text-xs text-muted hover:border-accent-line hover:text-text"
                                  aria-label={
                                    cell?.revealed
                                      ? `Hide ${key} in ${env.slug}`
                                      : `Reveal ${key} in ${env.slug}`
                                  }
                                >
                                  {cell?.loading ? (
                                    <Loader2 className="size-3 animate-spin" />
                                  ) : cell?.revealed ? (
                                    <EyeOff className="size-3 text-accent-text" />
                                  ) : (
                                    <Eye className="size-3 text-dim" />
                                  )}
                                  <span className="truncate">{shown ?? '••••••••'}</span>
                                </button>
                              )}
                              <div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
                                {shown !== null && (
                                  <CellAction
                                    label={`Copy ${key} in ${env.slug}`}
                                    onClick={() => {
                                      void copy(id, shown)
                                    }}
                                  >
                                    {copied === id ? (
                                      <Check className="size-3 text-accent-text" />
                                    ) : (
                                      <Copy className="size-3" />
                                    )}
                                  </CellAction>
                                )}
                                {(!env.protected || canAdmin) && (
                                  <CellAction
                                    label={`Edit ${key} in ${env.slug}`}
                                    onClick={() => {
                                      startEdit(env, key, secret)
                                    }}
                                  >
                                    <Pencil className="size-3" />
                                  </CellAction>
                                )}
                                <CellAction
                                  label={`Details for ${key} in ${env.slug}`}
                                  onClick={() => {
                                    setDetail({ key, env: env.slug })
                                  }}
                                >
                                  <PanelRight className="size-3" />
                                </CellAction>
                              </div>
                            </div>
                          </td>
                        )
                      })}
                      {overlaySlug &&
                        envs.map((env) => (
                          <td key={env.id + '-overlay'} className="px-3 py-2">
                            <span className="inline-flex h-6 max-w-36 items-center truncate rounded border border-border bg-elev px-2 font-mono text-xs text-muted">
                              {overlayByEnv.get(env.slug)?.get(key) || '—'}
                            </span>
                          </td>
                        ))}
                    </tr>
                  ))}
                  {keys.length === 0 && !loading && (
                    <tr>
                      <td
                        colSpan={envs.length * (overlaySlug ? 2 : 1) + 1}
                        className="px-4 py-10 text-center text-sm text-muted"
                      >
                        No keys match{filter ? ` “${filter}”` : ' this folder'}.
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
        <div
          role="region"
          aria-label="Unsaved changes"
          className="sticky bottom-12 z-20 flex flex-wrap items-center gap-3 rounded-app border border-accent-line bg-surface px-4 py-2.5 shadow-app"
        >
          <span className="text-sm font-medium text-accent-text">
            {editCount} unsaved {editCount === 1 ? 'change' : 'changes'}
          </span>
          {prodEdits > 0 && (
            <span className="env-chip env-production">{prodEdits} in production</span>
          )}
          <span className="hidden text-xs text-muted sm:inline">
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
                setSaveError(null)
                setReviewOpen(true)
              }}
            >
              Review & save
            </Button>
          </div>
        </div>
      )}

      {adding && (
        <AddSecretDialog
          envs={writableEnvs}
          schemaKind={(key) => schemaByKey.get(key)?.kind}
          busy={createSecret.isPending}
          error={createSecret.error instanceof Error ? createSecret.error.message : null}
          onClose={() => {
            createSecret.reset()
            setAdding(false)
          }}
          onSubmit={(input) => {
            createSecret.mutate(input, {
              onSuccess: () => {
                notify(`Added ${input.key} to ${input.env}.`)
              },
            })
          }}
        />
      )}

      {generating && org && (
        <GenerateSecretDialog
          orgSlug={org.slug}
          projectSlug={projectSlug}
          envs={writableEnvs}
          secretsForEnv={secretsForEnv}
          onClose={() => {
            setGenerating(false)
          }}
          onDone={(message) => {
            setGenerating(false)
            notify(message)
            void queryClient.invalidateQueries({ queryKey: ['secrets', org.slug, projectSlug] })
          }}
        />
      )}

      {pasting && org && (
        <ImportSecretsDialog
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

      {detail && org && (
        <SecretSheet
          orgSlug={org.slug}
          projectSlug={projectSlug}
          secretKey={detail.key}
          envs={envs}
          initialEnv={detail.env}
          secretIn={(envSlug) =>
            secretsByEnv.get(envSlug)?.find((candidate) => candidate.key === detail.key)
          }
          canWrite={(env) => !env.protected || canAdmin}
          schema={schemaByKey.get(detail.key)}
          onClose={() => {
            setDetail(null)
          }}
          onChanged={() => {
            void queryClient.invalidateQueries({ queryKey: ['secrets', org.slug, projectSlug] })
          }}
        />
      )}

      {reviewOpen && (
        <Modal
          onClose={() => {
            setReviewOpen(false)
          }}
          className="max-w-2xl"
        >
          <ModalHeader
            title="Review changes"
            description={`${String(editCount)} ${editCount === 1 ? 'change' : 'changes'} will be written in one atomic batch per environment.`}
            onClose={() => {
              setReviewOpen(false)
            }}
          />
          {prodEdits > 0 && (
            <Notice tone="warning" className="mt-3">
              {prodEdits} {prodEdits === 1 ? 'change touches' : 'changes touch'} production and
              apply immediately.
            </Notice>
          )}
          <div className="mt-3 max-h-80 overflow-y-auto rounded-app-sm border border-border">
            <table className="w-full text-left text-xs">
              <thead className="sticky top-0 bg-surface">
                <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
                  <th scope="col" className="px-2.5 py-1.5 font-semibold">
                    Environment
                  </th>
                  <th scope="col" className="px-2.5 py-1.5 font-semibold">
                    Key
                  </th>
                  <th scope="col" className="px-2.5 py-1.5 font-semibold">
                    Change
                  </th>
                  <th scope="col" className="px-2.5 py-1.5 font-semibold">
                    Value
                  </th>
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
                          'rounded px-1.5 py-0.5 text-2xs font-medium',
                          edit.existing
                            ? 'bg-amber-bg text-amber-fg'
                            : 'bg-success-bg text-success',
                        )}
                      >
                        {edit.existing ? 'update' : 'new'}
                      </span>
                    </td>
                    <td className="max-w-64 truncate px-2.5 py-1.5 font-mono text-dim">
                      {edit.kind === 'variable'
                        ? edit.value || '(empty)'
                        : edit.value
                          ? `•••• (${String(edit.value.length)} chars)`
                          : '(empty)'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {saveError && <Notice className="mt-3">{saveError}</Notice>}
          <ModalFooter>
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
              Save {editCount} {editCount === 1 ? 'change' : 'changes'}
            </Button>
          </ModalFooter>
        </Modal>
      )}
    </div>
  )
}

function BackLink() {
  return (
    <Link to="/" className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-text">
      <ArrowLeft className="size-3.5" /> Projects
    </Link>
  )
}

function FolderButton({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      aria-current={active ? 'true' : undefined}
      onClick={onClick}
      className={cn(
        'flex h-7 w-full items-center gap-1.5 rounded px-1.5 text-left text-xs',
        active ? 'bg-hover text-text' : 'text-muted hover:bg-hover hover:text-text',
      )}
    >
      {children}
    </button>
  )
}

function CellAction({
  label,
  onClick,
  children,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className="inline-flex size-6 items-center justify-center rounded border border-transparent text-dim hover:border-border hover:bg-elev hover:text-text"
    >
      {children}
    </button>
  )
}

function KeyBadges({
  schemaType,
  owner,
  inheritedFrom,
  overridden,
  expires,
}: {
  schemaType: string | undefined
  owner: string | undefined
  inheritedFrom: string | undefined
  overridden: boolean
  expires: string | undefined
}) {
  const expiresAt = expires ? new Date(expires) : null
  const [now] = useState(() => Date.now())
  const soon = expiresAt ? expiresAt.getTime() - now < 14 * 86_400_000 : false
  return (
    <span className="ml-1.5 inline-flex flex-wrap gap-1 align-middle">
      {schemaType && (
        <span className="rounded-full border border-border bg-elev px-1.5 text-3xs font-medium text-dim">
          {schemaType}
        </span>
      )}
      {owner && (
        <span className="rounded-full bg-accent-soft px-1.5 text-3xs font-medium text-accent-text">
          {owner}
        </span>
      )}
      {inheritedFrom && (
        <span className="rounded-full bg-elev px-1.5 font-mono text-3xs text-dim">
          from {inheritedFrom}
        </span>
      )}
      {overridden && (
        <span className="rounded-full bg-amber-bg px-1.5 text-3xs font-semibold text-amber-fg">
          override
        </span>
      )}
      {expiresAt && (
        <span
          className={cn(
            'rounded-full px-1.5 text-3xs font-medium',
            soon ? 'bg-danger-bg text-danger' : 'bg-elev text-dim',
          )}
        >
          expires {expiresAt.toLocaleDateString()}
        </span>
      )}
    </span>
  )
}

function EmptyState({
  canWrite,
  onAdd,
  onImport,
}: {
  canWrite: boolean
  onAdd: () => void
  onImport: () => void
}) {
  return (
    <div className="flex flex-col items-center px-6 py-14 text-center">
      <span className="flex size-11 items-center justify-center rounded-full bg-accent-soft text-accent-text">
        <KeyRound className="size-5" />
      </span>
      <h2 className="mt-3 font-head text-md font-semibold">No secrets yet</h2>
      <p className="mt-1 max-w-sm text-sm text-muted">
        Add your first secret, or import an existing <span className="font-mono">.env</span> file.
        Values are encrypted before they reach the database.
      </p>
      {canWrite && (
        <div className="mt-4 flex gap-2">
          <Button variant="secondary" onClick={onImport}>
            <Upload className="size-4" /> Import .env
          </Button>
          <Button variant="primary" onClick={onAdd}>
            <Plus className="size-4" /> Add secret
          </Button>
        </div>
      )}
      <p className="mt-4 text-2xs text-dim">
        Or from a terminal: <code className="font-mono">clavis import .env</code>
      </p>
    </div>
  )
}

const keyPattern = /^[A-Za-z_][A-Za-z0-9_.-]*$/

function AddSecretDialog({
  envs,
  schemaKind,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  envs: Env[]
  schemaKind: (key: string) => string | undefined
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
  const [env, setEnv] = useState(
    envs.find((candidate) => candidate.type === 'development')?.slug ?? envs[0]?.slug ?? '',
  )
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [folder, setFolder] = useState('')
  const [kind, setKind] = useState('secret')
  const [kindTouched, setKindTouched] = useState(false)
  const [message, setMessage] = useState('')
  const [more, setMore] = useState(false)
  const target = envs.find((candidate) => candidate.slug === env)
  const keyError =
    key !== '' && !keyPattern.test(key)
      ? 'Start with a letter or _; use letters, digits, _ . -'
      : null

  function submit(event: FormEvent) {
    event.preventDefault()
    if (keyError) return
    onSubmit({ env, key, value, folder, kind, message })
  }

  return (
    <Modal onClose={onClose}>
      <form onSubmit={submit}>
        <ModalHeader
          title="Add secret"
          description="The value is encrypted before it reaches the database."
          onClose={onClose}
        />
        <div className="mt-4 space-y-3">
          <Field label="Key" error={keyError}>
            <Input
              autoFocus
              required
              mono
              autoComplete="off"
              spellCheck={false}
              placeholder="STRIPE_SECRET_KEY"
              value={key}
              onChange={(event) => {
                const next = event.target.value.toUpperCase().replace(/\s/g, '_')
                setKey(next)
                const fromSchema = schemaKind(next)
                if (fromSchema && !kindTouched) setKind(fromSchema)
              }}
            />
          </Field>
          <Field label="Value">
            <Textarea
              required
              mono
              rows={3}
              autoComplete="off"
              spellCheck={false}
              placeholder={kind === 'variable' ? 'info' : 'sk_live_…'}
              value={value}
              onChange={(event) => {
                setValue(event.target.value)
              }}
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Environment">
              <Select
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
              </Select>
            </Field>
            <Field label="Kind">
              <Select
                value={kind}
                onChange={(event) => {
                  setKind(event.target.value)
                  setKindTouched(true)
                }}
              >
                <option value="secret">Secret — masked</option>
                <option value="variable">Variable — plain</option>
                <option value="file">File — masked</option>
              </Select>
            </Field>
          </div>
          {target?.type === 'production' && (
            <Notice tone="warning">This writes to production and applies immediately.</Notice>
          )}
          {more ? (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Folder">
                <Input
                  mono
                  placeholder="/backend/db"
                  value={folder}
                  onChange={(event) => {
                    setFolder(event.target.value)
                  }}
                />
              </Field>
              <Field label="Change message">
                <Input
                  placeholder="Why this exists"
                  value={message}
                  onChange={(event) => {
                    setMessage(event.target.value)
                  }}
                />
              </Field>
            </div>
          ) : (
            <button
              type="button"
              className="text-2xs font-medium text-accent-text hover:underline"
              onClick={() => {
                setMore(true)
              }}
            >
              Folder and change message…
            </button>
          )}
          {error && <Notice>{error}</Notice>}
        </div>
        <ModalFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" size="sm" disabled={busy || Boolean(keyError)}>
            {busy && <Loader2 className="size-3.5 animate-spin" />} Add to {env || 'environment'}
          </Button>
        </ModalFooter>
      </form>
    </Modal>
  )
}
