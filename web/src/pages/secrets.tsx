import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from '@tanstack/react-router'
import {
  ArrowLeft,
  ChevronRight,
  Braces,
  Check,
  Copy,
  Eye,
  EyeOff,
  FileText,
  Folder,
  Layers,
  GitCompare,
  MoveHorizontal,
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
import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button, buttonVariants } from '@/components/ui/button'
import { askRevealReason, confirmDialog } from '@/components/ui/dialogs'
import { DateInput } from '@/components/ui/date-input'
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
import { GenerateSecretDialog, GenerateValueDialog } from '@/pages/generate-dialog'
import { ImportSecretsDialog } from '@/pages/import-dialog'
import { CompareEnvironments } from '@/pages/secrets-compare'
import { envClassNames, envShort, SecretSheet } from '@/pages/secret-sheet'
import { isRequired, validateSchemaValue, type SchemaEntry } from '@/lib/schema'
import { useShortcuts } from '@/lib/use-shortcuts'
import { cn } from '@/lib/utils'
import { formatDate as formatDay } from '@/lib/format'

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
  // Wide values: roomy cells for long secrets and connection strings.
  const [wide, setWide] = useState(() => {
    try {
      return window.localStorage.getItem('clavis.secrets.wide') === 'true'
    } catch {
      return false
    }
  })
  const cellMax = wide ? 'max-w-[40rem]' : 'max-w-full'
  const [kindFilter, setKindFilter] = useState<KindFilter>('all')
  const [revealingAll, setRevealingAll] = useState(false)
  const [generateFor, setGenerateFor] = useState<string | null>(null)
  // Group by kind: secrets, variables and files in their own sections.
  const [grouped, setGrouped] = useState(() => {
    try {
      return window.localStorage.getItem('clavis.secrets.grouped') !== 'false'
    } catch {
      return true
    }
  })
  // Page shortcuts advertised in the hint bar: N add, P import, / filter.
  const filterRef = useRef<HTMLInputElement>(null)
  useShortcuts({
    n: () => {
      setAdding(true)
    },
    p: () => {
      setPasting(true)
    },
    '/': () => {
      filterRef.current?.focus()
    },
  })

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
    // Distinct keys per folder (a key in three environments counts once).
    const keysByFolder = new Map<string, Set<string>>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) {
        if (!secret.folder) continue
        const set = keysByFolder.get(secret.folder) ?? new Set<string>()
        set.add(secret.key)
        keysByFolder.set(secret.folder, set)
      }
    }
    return [...keysByFolder.entries()]
      .map(([path, set]) => [path, set.size] as [string, number])
      .sort((a, b) => a[0].localeCompare(b[0]))
  }, [secretsByEnv])

  const kindByKey = useMemo(() => {
    const map = new Map<string, SecretKind>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) {
        if (!map.has(secret.key)) map.set(secret.key, asSecretKind(secret.kind))
      }
    }
    return map
  }, [secretsByEnv])
  const kindCounts = useMemo(() => {
    const counts: Record<SecretKind, number> = { secret: 0, variable: 0, file: 0 }
    for (const kind of kindByKey.values()) counts[kind]++
    return counts
  }, [kindByKey])

  const keys = useMemo(() => {
    const all = new Set<string>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) all.add(secret.key)
    }
    // Everything the filter can match per key, across environments. Secret
    // values are not searched: they aren't loaded until revealed.
    const haystack = new Map<string, string>()
    for (const list of secretsByEnv.values()) {
      for (const secret of list) {
        const parts = [
          haystack.get(secret.key) ?? secret.key,
          secret.kind,
          secret.folder ?? '',
          secret.comment ?? '',
          (secret.tags ?? []).join(' '),
          secret.owner_group_name ?? '',
          secret.source_environment ?? '',
          secret.kind === 'variable' ? (secret.value ?? '') : '',
        ]
        haystack.set(secret.key, parts.join('\u0000').toLowerCase())
      }
    }
    const terms = filter.toLowerCase().split(/\s+/).filter(Boolean)
    return [...all]
      .sort((a, b) => a.localeCompare(b))
      .filter((key) => {
        const text = haystack.get(key) ?? key.toLowerCase()
        return terms.every((term) => text.includes(term))
      })
      .filter((key) => {
        if (folderFilter === null) return true
        for (const list of secretsByEnv.values()) {
          if (list.some((secret) => secret.key === key && secret.folder === folderFilter))
            return true
        }
        return false
      })
      .filter((key) => kindFilter === 'all' || kindByKey.get(key) === kindFilter)
  }, [secretsByEnv, filter, folderFilter, kindFilter, kindByKey])
  const sections = useMemo(
    () =>
      grouped
        ? kindOrder
            .map((kind) => ({ kind, keys: keys.filter((key) => kindByKey.get(key) === kind) }))
            .filter((section) => section.keys.length > 0)
        : [{ kind: null, keys }],
    [grouped, keys, kindByKey],
  )

  useEffect(() => {
    if (!pendingKey || envs.length === 0 || secretQueries.some((query) => query.isLoading)) return
    const env =
      envs.find((candidate) =>
        secretsByEnv.get(candidate.slug)?.some((secret) => secret.key === pendingKey),
      ) ?? envs[0]
    setDetail({ key: pendingKey, env: env?.slug ?? '' })
    setPendingKey(null)
  }, [pendingKey, envs, secretsByEnv, secretQueries])

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
    mutationFn: async (input: NewSecretInput) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets',
        {
          params: { path: { org: org.slug, project: projectSlug, env: input.env } },
          body: {
            key: input.key,
            value: input.value,
            kind: input.kind,
            folder: input.folder,
            message: input.message || 'Created from the web UI',
            ...(input.comment ? { comment: input.comment } : {}),
            ...(input.notes ? { notes: input.notes } : {}),
            ...(input.providerURL ? { provider_url: input.providerURL } : {}),
            ...(input.tags.length > 0 ? { tags: input.tags } : {}),
            ...(input.expiresAt ? { expires_at: `${input.expiresAt}T00:00:00Z` } : {}),
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

  const revealedCount = Object.values(cells).filter((cell) => cell.revealed).length

  async function revealAll() {
    if (!org) return
    if (revealedCount > 0) {
      setCells({})
      return
    }
    const hidden = envs.reduce(
      (count, env) =>
        count +
        (secretsByEnv.get(env.slug) ?? []).filter((secret) => secret.kind !== 'variable').length,
      0,
    )
    const hasProduction = envs.some((env) => env.type === 'production')
    let reason = 'Revealed all values in the web UI'
    if (hasProduction) {
      const entered = await askRevealReason(
        `all ${String(hidden)} secret and file values, production included`,
        true,
      )
      if (!entered) return
      reason = entered
    } else if (
      !(await confirmDialog({
        title: 'Reveal every value?',
        body: `${String(hidden)} secret and file values in ${String(envs.length)} environments will be shown in plain text. Each environment's reveal is recorded in the audit log.`,
        confirmLabel: 'Reveal all',
      }))
    ) {
      return
    }
    setRevealingAll(true)
    const next: Record<string, CellState> = {}
    for (const env of envs) {
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets',
        {
          params: {
            path: { org: org.slug, project: projectSlug, env: env.slug },
            query: { reveal: true, channel: 'ui', reason },
          },
        },
      )
      if (result.error) {
        notify(`${env.slug}: ${apiErrorMessage(result.error, 'could not reveal values')}`, 'error')
        continue
      }
      for (const secret of result.data?.secrets ?? []) {
        if (secret.kind === 'variable' || secret.value === undefined) continue
        next[`${env.slug}:${secret.key}`] = { value: secret.value, revealed: true, loading: false }
      }
    }
    setCells(next)
    setRevealingAll(false)
  }

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
              {kindByKey.size} {kindByKey.size === 1 ? 'key' : 'keys'} across {envs.length}{' '}
              {envs.length === 1 ? 'environment' : 'environments'}
              {keys.length !== kindByKey.size && ` · ${String(keys.length)} shown`}
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
              variant="secondary"
              size="icon"
              className="size-9"
              aria-label="Generate a secret"
              title="Generate a secret (password, token, JWT key, key pair…)"
              onClick={() => {
                setGenerating(true)
              }}
              disabled={writableEnvs.length === 0}
            >
              <Sparkles className="size-4" />
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
        <label className="flex h-8 min-w-0 flex-1 items-center gap-2 rounded-app-sm border border-border bg-surface px-2.5 focus-within:border-accent-line focus-within:ring-2 focus-within:ring-accent-soft sm:max-w-80">
          <Search className="size-3.5 shrink-0 text-dim" aria-hidden />
          <span className="sr-only">Filter keys</span>
          <input
            className="h-full min-w-0 flex-1 bg-transparent text-sm text-text placeholder:text-dim focus:outline-none"
            ref={filterRef}
            title="Matches key names, tags, comments, folders, kinds and variable values"
            placeholder="Filter by key, tag, comment, folder…"
            value={filter}
            onChange={(event) => {
              setFilter(event.target.value)
            }}
          />
          {!filter && (
            <kbd className="rounded border border-border bg-elev px-1 font-mono text-3xs text-dim">
              /
            </kbd>
          )}
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
        {!compare && (
          <>
            <div
              className="flex rounded-app-sm border border-border bg-surface p-0.5"
              role="group"
              aria-label="Show kinds"
            >
              {(['all', ...kindOrder] as KindFilter[]).map((kind) => (
                <button
                  key={kind}
                  type="button"
                  aria-pressed={kindFilter === kind}
                  onClick={() => {
                    setKindFilter(kind)
                  }}
                  className={cn(
                    'flex h-6 items-center gap-1.5 rounded px-2 text-xs',
                    kindFilter === kind
                      ? 'bg-elev font-medium text-text'
                      : 'text-muted hover:text-text',
                  )}
                >
                  {kind === 'all' ? 'All' : kindMeta[kind].plural}
                  <span className="text-2xs text-dim">
                    {kind === 'all' ? kindByKey.size : kindCounts[kind]}
                  </span>
                </button>
              ))}
            </div>
            <Button
              variant={grouped ? 'primary' : 'secondary'}
              size="sm"
              aria-pressed={grouped}
              title="Show secrets, variables and files in separate sections"
              onClick={() => {
                const next = !grouped
                setGrouped(next)
                try {
                  window.localStorage.setItem('clavis.secrets.grouped', String(next))
                } catch {
                  // Storage can be unavailable; the toggle still works for this visit.
                }
              }}
            >
              <Layers className="size-3.5" /> Group by kind
            </Button>
            <Button
              variant={revealedCount > 0 ? 'primary' : 'secondary'}
              size="sm"
              disabled={revealingAll}
              title="Show every secret and file value (audited)"
              onClick={() => {
                void revealAll()
              }}
            >
              {revealingAll ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : revealedCount > 0 ? (
                <EyeOff className="size-3.5" />
              ) : (
                <Eye className="size-3.5" />
              )}
              {revealedCount > 0 ? 'Hide all' : 'Reveal all'}
            </Button>
          </>
        )}
        <Button
          variant={wide ? 'primary' : 'secondary'}
          size="sm"
          aria-pressed={wide}
          title="Show longer values in each cell"
          onClick={() => {
            const next = !wide
            setWide(next)
            try {
              window.localStorage.setItem('clavis.secrets.wide', String(next))
            } catch {
              // Storage can be unavailable; the toggle still works for this visit.
            }
          }}
        >
          <MoveHorizontal className="size-3.5" /> Wide values
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
      </div>

      {loadError && <Notice>{loadError}</Notice>}

      {compare && org ? (
        <CompareEnvironments
          orgSlug={org.slug}
          projectSlug={projectSlug}
          envs={envs}
          writableEnvs={writableEnvs}
          filter={filter}
        />
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
                  <span className="ml-auto text-2xs text-dim">{kindByKey.size}</span>
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
              <table
                className={cn(
                  'w-full border-collapse text-left',
                  // Columns share the width; Wide values lets cells grow and scroll instead.
                  !wide && !overlaySlug && 'table-fixed',
                )}
              >
                <caption className="sr-only">Secrets by environment</caption>
                <colgroup>
                  <col className="w-64" />
                </colgroup>
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
                  {sections.map((section) => (
                    <Fragment key={section.kind ?? 'all'}>
                      {section.kind && (
                        <tr className="border-b border-border-soft bg-panel">
                          <th
                            scope="rowgroup"
                            colSpan={envs.length * (overlaySlug ? 2 : 1) + 1}
                            className="sticky left-0 px-4 py-1.5 text-left text-2xs font-semibold uppercase tracking-[0.06em] text-dim"
                          >
                            <span className="inline-flex items-center gap-1.5">
                              {kindMeta[section.kind].icon}
                              {kindMeta[section.kind].plural}
                              <span className="font-normal">{section.keys.length}</span>
                              <span className="font-normal normal-case tracking-normal">
                                · {kindMeta[section.kind].hint}
                              </span>
                            </span>
                          </th>
                        </tr>
                      )}
                      {section.keys.map((key) => (
                        <tr
                          key={key}
                          className="group border-b border-border-soft last:border-0 hover:bg-hover-soft"
                        >
                          <th
                            scope="row"
                            className="sticky left-0 z-10 bg-surface px-4 py-2 text-left font-normal group-hover:bg-[color-mix(in_srgb,var(--surface),var(--text)_2%)]"
                          >
                            <div className="flex min-w-0 items-center gap-2">
                              <span
                                className="shrink-0 text-dim"
                                title={kindMeta[kindByKey.get(key) ?? 'secret'].label}
                              >
                                {kindMeta[kindByKey.get(key) ?? 'secret'].icon}
                              </span>
                              <div className="min-w-0">
                                <button
                                  type="button"
                                  className="block max-w-full truncate font-mono text-xs text-text hover:text-accent-text hover:underline"
                                  title={`${key}: open details and history`}
                                  onClick={() => {
                                    const first =
                                      envs.find((env) =>
                                        secretsByEnv
                                          .get(env.slug)
                                          ?.some((secret) => secret.key === key),
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
                              </div>
                            </div>
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
                                edit.value === ''
                                  ? null
                                  : validateSchemaValue(schemaEntry, edit.value)
                              return (
                                <td key={env.id} className="px-3 py-2">
                                  <div className="space-y-1">
                                    <div className="flex items-center gap-1">
                                      <Input
                                        autoFocus
                                        aria-label={`New value for ${key} in ${env.slug}`}
                                        aria-invalid={validation ? true : undefined}
                                        placeholder={edit.existing ? 'New value' : 'Value'}
                                        autoComplete="off"
                                        spellCheck={false}
                                        className={cn(
                                          'h-7 w-full rounded border border-accent-line bg-bg px-2 font-mono text-xs text-text focus:outline-none',
                                          wide ? 'min-w-96' : 'min-w-36',
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
                                      {secret?.kind !== 'variable' && (
                                        <CellAction
                                          label={`Generate a value for ${key}`}
                                          onClick={() => {
                                            setGenerateFor(id)
                                          }}
                                        >
                                          <Sparkles className="size-3" />
                                        </CellAction>
                                      )}
                                    </div>
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
                              <td key={env.id} className="group/cell relative px-3 py-2">
                                <div className="flex min-w-0 items-center gap-1">
                                  {plain ? (
                                    <span
                                      className={cn(
                                        'inline-flex h-6 items-center truncate rounded border border-transparent px-1 font-mono text-xs text-muted',
                                        cellMax,
                                      )}
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
                                      className={cn(
                                        'inline-flex h-6 items-center gap-1.5 truncate rounded border border-border bg-elev px-2 font-mono text-xs text-muted hover:border-accent-line hover:text-text',
                                        cellMax,
                                      )}
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
                                  {/* Floats over the cell's right edge so showing it never
                                      widens the column (which pushed other environments
                                      out of view with Wide values on). Touch screens
                                      keep it inline. */}
                                  <div className="absolute top-1/2 right-1.5 z-[1] hidden -translate-y-1/2 items-center gap-0.5 rounded-app-sm border border-border bg-surface p-0.5 shadow-sm group-focus-within/cell:flex group-hover/cell:flex [@media(hover:none)]:static [@media(hover:none)]:flex [@media(hover:none)]:translate-y-0 [@media(hover:none)]:border-0 [@media(hover:none)]:p-0 [@media(hover:none)]:shadow-none">
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
                    </Fragment>
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
          folderOptions={folders.map(([path]) => path)}
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

      {generateFor && (
        <GenerateValueDialog
          onClose={() => {
            setGenerateFor(null)
          }}
          onUse={(value) => {
            updateEdit(generateFor, { value })
            setGenerateFor(null)
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
    <span className="mt-0.5 flex gap-1 overflow-hidden whitespace-nowrap empty:hidden">
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
          expires {formatDay(expiresAt)}
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

type NewSecretInput = {
  env: string
  key: string
  value: string
  folder: string
  kind: SecretKind
  message: string
  comment: string
  notes: string
  providerURL: string
  tags: string[]
  expiresAt: string
}

function AddSecretDialog({
  envs,
  schemaKind,
  folderOptions,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  envs: Env[]
  schemaKind: (key: string) => string | undefined
  folderOptions: string[]
  busy: boolean
  error: string | null
  onClose: () => void
  onSubmit: (input: NewSecretInput) => void
}) {
  const [env, setEnv] = useState(
    envs.find((candidate) => candidate.type === 'development')?.slug ?? envs[0]?.slug ?? '',
  )
  const [key, setKey] = useState('')
  const [value, setValue] = useState('')
  const [showValue, setShowValue] = useState(false)
  const [fileName, setFileName] = useState<string | null>(null)
  const [folder, setFolder] = useState('')
  const [kind, setKind] = useState<SecretKind>('secret')
  const [kindTouched, setKindTouched] = useState(false)
  const [message, setMessage] = useState('')
  const [comment, setComment] = useState('')
  const [notes, setNotes] = useState('')
  const [providerURL, setProviderURL] = useState('')
  const [tags, setTags] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const [advanced, setAdvanced] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [localError, setLocalError] = useState<string | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const target = envs.find((candidate) => candidate.slug === env)
  const keyError =
    key !== '' && !keyPattern.test(key)
      ? 'Start with a letter or _; use letters, digits, _ . -'
      : null
  const urlError =
    providerURL !== '' && !/^https?:\/\//.test(providerURL) ? 'Use an http(s) URL' : null

  function pickKind(next: SecretKind) {
    setKind(next)
    setKindTouched(true)
  }

  function loadFile(file: File) {
    if (file.size > 1024 * 1024) {
      setLocalError('Files must be 1 MiB or smaller.')
      return
    }
    setLocalError(null)
    void file.text().then((text) => {
      setValue(text)
      setFileName(`${file.name} · ${String(file.size)} bytes`)
      if (!key)
        setKey(
          file.name
            .toUpperCase()
            .replace(/[^A-Z0-9]+/g, '_')
            .replace(/^_+|_+$/g, ''),
        )
    })
  }

  function submit(event: FormEvent) {
    event.preventDefault()
    if (keyError || urlError) return
    onSubmit({
      env,
      key,
      value,
      folder,
      kind,
      message,
      comment,
      notes,
      providerURL,
      tags: tags
        .split(',')
        .map((tag) => tag.trim())
        .filter(Boolean),
      expiresAt,
    })
  }

  const masked = kind !== 'variable' && !showValue

  return (
    <Modal onClose={onClose} className="max-w-xl">
      <form onSubmit={submit}>
        <ModalHeader
          title="Add a key"
          description="Values are encrypted before they reach the database."
          onClose={onClose}
        />
        <div className="mt-4 space-y-4">
          <div role="radiogroup" aria-label="Kind" className="grid grid-cols-3 gap-2">
            {kindOrder.map((option) => (
              <button
                key={option}
                type="button"
                role="radio"
                aria-checked={kind === option}
                onClick={() => {
                  pickKind(option)
                }}
                className={cn(
                  'rounded-app-sm border px-3 py-2 text-left transition-colors',
                  kind === option
                    ? 'border-accent-line bg-accent-soft'
                    : 'border-border hover:border-accent-line hover:bg-hover',
                )}
              >
                <span className="flex items-center gap-1.5 text-sm font-medium">
                  {kindMeta[option].icon} {kindMeta[option].label}
                </span>
                <span className="mt-0.5 block text-2xs leading-4 text-muted">
                  {kindMeta[option].short}
                </span>
              </button>
            ))}
          </div>

          <div className="grid gap-3 sm:grid-cols-[1fr_180px]">
            <Field label="Key" error={keyError}>
              <Input
                autoFocus
                required
                mono
                autoComplete="off"
                spellCheck={false}
                placeholder={
                  kind === 'file'
                    ? 'TLS_CERT'
                    : kind === 'variable'
                      ? 'LOG_LEVEL'
                      : 'STRIPE_SECRET_KEY'
                }
                value={key}
                onChange={(event) => {
                  const next = event.target.value.toUpperCase().replace(/\s/g, '_')
                  setKey(next)
                  const fromSchema = asSecretKind(schemaKind(next) ?? '')
                  if (schemaKind(next) && !kindTouched) setKind(fromSchema)
                }}
              />
            </Field>
            <Field label="Environment">
              <Select
                value={env}
                onChange={(event) => {
                  setEnv(event.target.value)
                }}
              >
                {envs.map((candidate) => (
                  <option key={candidate.id} value={candidate.slug}>
                    {candidate.name}
                  </option>
                ))}
              </Select>
            </Field>
          </div>

          <div>
            <div className="mb-1 flex items-center gap-1">
              <span className="text-xs font-medium text-muted">Value</span>
              {fileName && kind === 'file' && (
                <span className="ml-1 truncate font-mono text-2xs text-dim">{fileName}</span>
              )}
              <div className="ml-auto flex gap-1">
                {kind !== 'variable' && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setShowValue((current) => !current)
                    }}
                  >
                    {showValue ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                    {showValue ? 'Hide' : 'Show'}
                  </Button>
                )}
                {kind === 'file' && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      fileInput.current?.click()
                    }}
                  >
                    <Upload className="size-3.5" /> Load file
                  </Button>
                )}
                {kind !== 'variable' && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setGenerating(true)
                    }}
                  >
                    <Sparkles className="size-3.5" /> Generate
                  </Button>
                )}
              </div>
            </div>
            <input
              ref={fileInput}
              type="file"
              className="hidden"
              onChange={(event) => {
                const file = event.target.files?.[0]
                if (file) loadFile(file)
                event.target.value = ''
              }}
            />
            {kind === 'variable' ? (
              <Input
                required
                mono
                autoComplete="off"
                spellCheck={false}
                placeholder="info"
                value={value}
                onChange={(event) => {
                  setValue(event.target.value)
                }}
              />
            ) : (
              <div
                className="relative"
                onDragOver={(event) => {
                  if (kind === 'file') event.preventDefault()
                }}
                onDrop={(event) => {
                  if (kind !== 'file') return
                  event.preventDefault()
                  const file = event.dataTransfer.files[0]
                  if (file) loadFile(file)
                }}
              >
                <Textarea
                  required
                  mono
                  rows={kind === 'file' ? 7 : 3}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={
                    kind === 'file'
                      ? 'Paste the contents, drop a file here, or use Load file\n-----BEGIN CERTIFICATE-----'
                      : 'sk_live_…'
                  }
                  className={cn(masked && value && '[-webkit-text-security:disc]')}
                  value={value}
                  onChange={(event) => {
                    setValue(event.target.value)
                    setFileName(null)
                  }}
                />
              </div>
            )}
            <p className="mt-1 text-2xs text-dim">
              {kind === 'file'
                ? 'Stored as text. `clavis run` can write it to a file and pass the path.'
                : kind === 'variable'
                  ? 'Shown in plain text to everyone who can see this environment.'
                  : 'Hidden in the app; every reveal is audited.'}
            </p>
          </div>

          {target?.type === 'production' && (
            <Notice tone="warning">This writes to production and applies immediately.</Notice>
          )}

          <div className="rounded-app-sm border border-border">
            <button
              type="button"
              aria-expanded={advanced}
              className="flex w-full items-center gap-1.5 px-3 py-2 text-left text-xs font-medium text-muted hover:text-text"
              onClick={() => {
                setAdvanced((current) => !current)
              }}
            >
              <ChevronRight
                className={cn('size-3.5 transition-transform', advanced && 'rotate-90')}
              />
              Advanced
              <span className="font-normal text-dim">
                folder, comment, tags, expiry, provider link, notes
              </span>
            </button>
            {advanced && (
              <div className="grid gap-3 border-t border-border-soft p-3 sm:grid-cols-2">
                <datalist id="add-secret-folders">
                  {folderOptions.map((path) => (
                    <option key={path} value={path} />
                  ))}
                </datalist>
                <Field label="Folder">
                  <Input
                    mono
                    list="add-secret-folders"
                    placeholder="/backend/db"
                    value={folder}
                    onChange={(event) => {
                      setFolder(event.target.value)
                    }}
                  />
                </Field>
                <Field label="Expires on" hint="You get a reminder before it lapses.">
                  <DateInput value={expiresAt} onChange={setExpiresAt} />
                </Field>
                <Field label="Comment" className="sm:col-span-2">
                  <Input
                    placeholder="What this is for"
                    value={comment}
                    onChange={(event) => {
                      setComment(event.target.value)
                    }}
                  />
                </Field>
                <Field label="Tags" hint="Comma-separated; the filter searches them.">
                  <Input
                    placeholder="billing, stripe"
                    value={tags}
                    onChange={(event) => {
                      setTags(event.target.value)
                    }}
                  />
                </Field>
                <Field label="Provider URL" error={urlError}>
                  <Input
                    placeholder="https://dashboard.stripe.com/apikeys"
                    value={providerURL}
                    onChange={(event) => {
                      setProviderURL(event.target.value)
                    }}
                  />
                </Field>
                <Field label="Runbook notes" className="sm:col-span-2">
                  <Textarea
                    rows={2}
                    placeholder="How to rotate it, who owns it, what breaks."
                    value={notes}
                    onChange={(event) => {
                      setNotes(event.target.value)
                    }}
                  />
                </Field>
                <Field
                  label="Change message"
                  className="sm:col-span-2"
                  hint="Shown in the key's History (click the key name) next to this first version."
                >
                  <Input
                    placeholder="Added for the Stripe integration"
                    value={message}
                    onChange={(event) => {
                      setMessage(event.target.value)
                    }}
                  />
                </Field>
              </div>
            )}
          </div>
          {(localError ?? error) && <Notice>{localError ?? error}</Notice>}
        </div>
        <ModalFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            variant="primary"
            size="sm"
            disabled={busy || Boolean(keyError) || Boolean(urlError)}
          >
            {busy && <Loader2 className="size-3.5 animate-spin" />} Add to{' '}
            {target?.name ?? 'environment'}
          </Button>
        </ModalFooter>
      </form>
      {generating && (
        <GenerateValueDialog
          onClose={() => {
            setGenerating(false)
          }}
          onUse={(generated, generatedKind) => {
            setValue(generated)
            setFileName(null)
            if (generatedKind === 'file') pickKind('file')
            setGenerating(false)
          }}
        />
      )}
    </Modal>
  )
}

type SecretKind = 'secret' | 'variable' | 'file'
type KindFilter = 'all' | SecretKind
const kindOrder: SecretKind[] = ['secret', 'variable', 'file']

const kindMeta: Record<
  SecretKind,
  { label: string; plural: string; hint: string; short: string; icon: React.ReactNode }
> = {
  secret: {
    label: 'Secret',
    plural: 'Secrets',
    hint: 'hidden until revealed; every reveal is audited',
    short: 'Masked, reveals audited',
    icon: <KeyRound className="size-3.5" />,
  },
  variable: {
    label: 'Variable',
    plural: 'Variables',
    hint: 'plain configuration, shown as is',
    short: 'Plain config, visible',
    icon: <Braces className="size-3.5" />,
  },
  file: {
    label: 'File',
    plural: 'Files',
    hint: 'certificates, keys and other multi-line values written to disk',
    short: 'Certs and keys, from disk',
    icon: <FileText className="size-3.5" />,
  },
}

function asSecretKind(kind: string): SecretKind {
  return kind === 'variable' || kind === 'file' ? kind : 'secret'
}
