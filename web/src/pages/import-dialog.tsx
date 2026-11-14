import { useQuery } from '@tanstack/react-query'
import { Eye, EyeOff, FileUp, FolderOpen, Loader2 } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/field'
import { Modal, ModalHeader } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { parseDotenv } from '@/lib/dotenv'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']
type Secret = components['schemas']['SecretBody']
type Kind = 'secret' | 'variable' | 'file'
type Status = 'new' | 'changed' | 'same' | 'exists' | 'checking'
type Filter = 'all' | 'new' | 'changed' | 'same'

type ParsedEntry = { key: string; value: string }
type Edit = { key?: string; folder?: string; kind?: Kind; include?: boolean }

const keyPattern = /^[A-Za-z_][A-Za-z0-9_.-]*$/

/**
 * Import a .env or JSON object into one environment. The server compares the
 * pasted values with the stored ones (the secret list carries no secret
 * values), so unchanged keys are recognised and skipped instead of getting a
 * new version. Existing keys keep their kind and folder unless changed here.
 */
export function ImportSecretsDialog({
  orgSlug,
  projectSlug,
  envs,
  secretsForEnv,
  onClose,
  onDone,
}: {
  orgSlug: string
  projectSlug: string
  envs: Env[]
  secretsForEnv: (env: string) => Secret[]
  onClose: () => void
  onDone: (message: string) => void
}) {
  const [env, setEnv] = useState(envs[0]?.slug ?? '')
  const [text, setText] = useState('')
  const [edits, setEdits] = useState<Record<string, Edit>>({})
  const [filter, setFilter] = useState<Filter>('all')
  const [showValues, setShowValues] = useState(false)
  const [bulkFolder, setBulkFolder] = useState('')
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const folderListId = useId()

  const parsed = useMemo(() => parseImportText(text), [text])
  const existing = useMemo(() => {
    const map = new Map<string, Secret>()
    for (const secret of secretsForEnv(env)) map.set(secret.key, secret)
    return map
  }, [env, secretsForEnv])

  // Wait for typing to settle before asking the server.
  const [settled, setSettled] = useState(parsed.entries)
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setSettled(parsed.entries)
    }, 350)
    return () => {
      window.clearTimeout(timer)
    }
  }, [parsed.entries])

  const compare = useQuery({
    queryKey: ['import-compare', orgSlug, projectSlug, env, settled],
    enabled: Boolean(env) && settled.length > 0,
    staleTime: 30_000,
    retry: false,
    queryFn: async () => {
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets:compare',
        {
          params: { path: { org: orgSlug, project: projectSlug, env } },
          body: { entries: settled.map((entry) => ({ key: entry.key, value: entry.value })) },
        },
      )
      // Without permission to read values, keys that exist show as "exists".
      if (result.error) return null
      return result.data.statuses
    },
  })

  const folders = useQuery({
    queryKey: ['folders', orgSlug, projectSlug, env],
    enabled: Boolean(env),
    queryFn: async () => {
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/folders',
        { params: { path: { org: orgSlug, project: projectSlug, env } } },
      )
      return (result.data?.folders ?? []).map((folder) => folder.path)
    },
  })
  const folderOptions = useMemo(() => {
    const all = new Set<string>(folders.data ?? [])
    for (const secret of existing.values()) if (secret.folder) all.add(secret.folder)
    for (const edit of Object.values(edits)) if (edit.folder) all.add(edit.folder)
    return [...all].sort()
  }, [folders.data, existing, edits])

  const rows = useMemo(
    () =>
      parsed.entries.map((entry) => {
        const edit = edits[entry.key] ?? {}
        const key = edit.key ?? entry.key
        const current = existing.get(key)
        // Existing keys keep their kind and folder; new keys get a guess.
        const kind: Kind = edit.kind ?? asKind(current?.kind) ?? guessKind(key, entry.value)
        const folder = edit.folder ?? current?.folder ?? ''
        const status = rowStatus({
          current,
          kind,
          folder,
          serverStatus: compare.data?.[key],
          checking: compare.isFetching || settled !== parsed.entries,
          compared: compare.data !== null,
        })
        const include = edit.include ?? (status !== 'same' && status !== 'checking')
        return {
          source: entry.key,
          value: entry.value,
          key,
          kind,
          folder,
          status,
          include,
          current,
        }
      }),
    [parsed.entries, edits, existing, compare.data, compare.isFetching, settled],
  )

  const counts = useMemo(() => {
    const out = { new: 0, changed: 0, same: 0, exists: 0, checking: 0 }
    for (const row of rows) out[row.status]++
    return out
  }, [rows])
  const included = rows.filter((row) => row.include)
  const visible = rows.filter((row) =>
    filter === 'all'
      ? true
      : filter === 'changed'
        ? row.status === 'changed' || row.status === 'exists'
        : row.status === filter,
  )

  function edit(source: string, patch: Edit) {
    setEdits((current) => ({ ...current, [source]: { ...current[source], ...patch } }))
  }

  function editIncluded(patch: Edit) {
    setEdits((current) => {
      const next = { ...current }
      for (const row of included) next[row.source] = { ...next[row.source], ...patch }
      return next
    })
  }

  async function loadFile(file: File) {
    setText(await file.text())
    setEdits({})
  }

  async function apply() {
    if (parsed.error || included.length === 0 || !env) return
    const seen = new Set<string>()
    for (const row of included) {
      if (!keyPattern.test(row.key)) {
        setError(`${row.source}: “${row.key}” is not a valid key.`)
        return
      }
      if (seen.has(row.key)) {
        setError(`${row.key}: the same key is imported twice.`)
        return
      }
      seen.add(row.key)
    }
    setBusy(true)
    setError(null)
    setProgress(0)
    let written = 0
    for (const row of included) {
      const payload = {
        value: row.value,
        kind: row.kind,
        folder: row.folder,
        message: 'Imported from .env',
      }
      const result = row.current
        ? await api.PUT('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}', {
            params: { path: { org: orgSlug, project: projectSlug, env, key: row.key } },
            body: payload,
          })
        : await api.POST('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets', {
            params: { path: { org: orgSlug, project: projectSlug, env } },
            body: { key: row.key, ...payload },
          })
      if (result.error) {
        setError(
          `${row.key}: ${apiErrorMessage(result.error, 'import failed')} (${String(written)} written before it)`,
        )
        setBusy(false)
        return
      }
      written++
      setProgress(written)
    }
    setBusy(false)
    const skipped = rows.length - written
    onDone(
      `Imported ${String(written)} ${written === 1 ? 'key' : 'keys'} into ${env}` +
        (skipped > 0 ? `, skipped ${String(skipped)} unchanged or unchecked.` : '.'),
    )
  }

  const filters: { id: Filter; label: string; count: number }[] = [
    { id: 'all', label: 'All', count: rows.length },
    { id: 'new', label: 'New', count: counts.new },
    { id: 'changed', label: 'Changed', count: counts.changed + counts.exists },
    { id: 'same', label: 'Unchanged', count: counts.same },
  ]

  return (
    <Modal onClose={onClose} className="h-[calc(100dvh-2rem)] max-w-[1200px] overflow-hidden p-0">
      <div className="border-b border-border px-5 py-4">
        <ModalHeader
          title="Import .env"
          description="Paste a .env or a flat JSON object, or load a file. Unchanged keys are skipped; nothing is written until you apply."
          onClose={onClose}
        />
      </div>

      <div className="grid min-h-0 flex-1 lg:grid-cols-[minmax(280px,2fr)_5fr]">
        {/* Source */}
        <div className="flex min-h-0 flex-col gap-3 border-b border-border p-4 lg:border-r lg:border-b-0">
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted">Into environment</span>
            <Select
              value={env}
              onChange={(event) => {
                setEnv(event.target.value)
                setEdits({})
              }}
            >
              {envs.map((candidate) => (
                <option key={candidate.id} value={candidate.slug}>
                  {candidate.name}
                </option>
              ))}
            </Select>
          </label>
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-muted">Contents</span>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                fileInput.current?.click()
              }}
            >
              <FileUp className="size-3.5" />
              Load file
            </Button>
            <input
              ref={fileInput}
              type="file"
              className="hidden"
              accept=".env,.json,text/plain,application/json"
              onChange={(event) => {
                const file = event.target.files?.[0]
                if (file) void loadFile(file)
                event.target.value = ''
              }}
            />
          </div>
          <textarea
            className="min-h-40 w-full flex-1 resize-none rounded-app-sm border border-border bg-bg px-2.5 py-2 font-mono text-xs leading-5 text-text focus:border-accent-line focus:outline-none"
            placeholder={
              'DATABASE_URL=postgres://…\nREDIS_URL=redis://…\nLOG_LEVEL=info\n\n# or {"DATABASE_URL": "postgres://…"}'
            }
            value={text}
            onChange={(event) => {
              setText(event.target.value)
            }}
            spellCheck={false}
            autoFocus
            data-autofocus=""
          />
          {parsed.error && (
            <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">
              {parsed.error}
            </p>
          )}
        </div>

        {/* Mapping */}
        <div className="flex min-h-0 flex-col">
          {rows.length === 0 ? (
            <div className="flex flex-1 flex-col items-center justify-center gap-2 p-8 text-center text-sm text-dim">
              <FolderOpen className="size-6" />
              Keys appear here as you paste. You can rename them, pick a folder and choose secret,
              variable or file before importing.
            </div>
          ) : (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2.5">
                <div className="flex rounded-app-sm border border-border p-0.5">
                  {filters.map((option) => (
                    <button
                      key={option.id}
                      type="button"
                      onClick={() => {
                        setFilter(option.id)
                      }}
                      className={cn(
                        'rounded px-2 py-1 text-xs',
                        filter === option.id
                          ? 'bg-elev font-medium text-text'
                          : 'text-muted hover:text-text',
                      )}
                    >
                      {option.label} <span className="text-dim">{option.count}</span>
                    </button>
                  ))}
                </div>
                {counts.checking > 0 && (
                  <span className="flex items-center gap-1 text-xs text-dim">
                    <Loader2 className="size-3 animate-spin" /> Comparing…
                  </span>
                )}
                <Button
                  variant="ghost"
                  size="sm"
                  className="ml-auto"
                  onClick={() => {
                    setShowValues((current) => !current)
                  }}
                >
                  {showValues ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                  {showValues ? 'Hide values' : 'Show values'}
                </Button>
              </div>

              <div className="flex flex-wrap items-center gap-2 border-b border-border-soft bg-panel px-4 py-2 text-xs">
                <span className="text-muted">
                  For the {included.length} checked {included.length === 1 ? 'key' : 'keys'}:
                </span>
                <Input
                  className="h-7 w-48 text-xs"
                  mono
                  list={folderListId}
                  placeholder="folder, e.g. /backend"
                  value={bulkFolder}
                  onChange={(event) => {
                    setBulkFolder(event.target.value)
                  }}
                />
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={included.length === 0}
                  onClick={() => {
                    editIncluded({ folder: normalizeFolder(bulkFolder) })
                  }}
                >
                  Move to folder
                </Button>
                <span className="text-dim">·</span>
                <Select
                  size="sm"
                  value=""
                  disabled={included.length === 0}
                  onChange={(event) => {
                    if (event.target.value) editIncluded({ kind: event.target.value as Kind })
                  }}
                >
                  <option value="">Set kind…</option>
                  <option value="secret">secret</option>
                  <option value="variable">variable</option>
                  <option value="file">file</option>
                </Select>
              </div>

              <datalist id={folderListId}>
                {folderOptions.map((folder) => (
                  <option key={folder} value={folder} />
                ))}
              </datalist>

              <div className="min-h-0 flex-1 overflow-auto">
                <table className="w-full min-w-[720px] text-left text-xs">
                  <thead className="sticky top-0 z-10 bg-surface">
                    <tr className="border-b border-border text-2xs uppercase tracking-[0.05em] text-dim">
                      <th className="w-8 px-3 py-2">
                        <input
                          type="checkbox"
                          aria-label="Check all shown"
                          checked={visible.length > 0 && visible.every((row) => row.include)}
                          onChange={(event) => {
                            const on = event.target.checked
                            setEdits((current) => {
                              const next = { ...current }
                              for (const row of visible)
                                next[row.source] = { ...next[row.source], include: on }
                              return next
                            })
                          }}
                        />
                      </th>
                      <th className="px-2 py-2 font-semibold">Key</th>
                      <th className="px-2 py-2 font-semibold">Value</th>
                      <th className="px-2 py-2 font-semibold">Folder</th>
                      <th className="px-2 py-2 font-semibold">Kind</th>
                      <th className="px-3 py-2 font-semibold">Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {visible.map((row) => (
                      <tr
                        key={row.source}
                        className={cn(
                          'border-b border-border-soft last:border-0',
                          !row.include && 'opacity-55',
                        )}
                      >
                        <td className="px-3 py-1.5">
                          <input
                            type="checkbox"
                            aria-label={`Import ${row.key}`}
                            checked={row.include}
                            onChange={(event) => {
                              edit(row.source, { include: event.target.checked })
                            }}
                          />
                        </td>
                        <td className="px-2 py-1.5">
                          <Input
                            mono
                            className="h-7 text-xs"
                            value={row.key}
                            aria-invalid={!keyPattern.test(row.key)}
                            onChange={(event) => {
                              edit(row.source, { key: event.target.value })
                            }}
                          />
                        </td>
                        <td className="max-w-[260px] px-2 py-1.5">
                          <span
                            className="block truncate font-mono text-muted"
                            title={showValues ? row.value : undefined}
                          >
                            {row.value === ''
                              ? '(empty)'
                              : showValues
                                ? row.value.replace(/\n/g, '⏎')
                                : `${'•'.repeat(Math.min(12, row.value.length))} ${String(row.value.length)} chars`}
                          </span>
                        </td>
                        <td className="px-2 py-1.5">
                          <Input
                            mono
                            className="h-7 w-40 text-xs"
                            list={folderListId}
                            placeholder="/"
                            value={row.folder}
                            onChange={(event) => {
                              edit(row.source, { folder: event.target.value })
                            }}
                            onBlur={(event) => {
                              edit(row.source, { folder: normalizeFolder(event.target.value) })
                            }}
                          />
                        </td>
                        <td className="px-2 py-1.5">
                          <Select
                            size="sm"
                            value={row.kind}
                            onChange={(event) => {
                              edit(row.source, { kind: event.target.value as Kind })
                            }}
                          >
                            <option value="secret">secret</option>
                            <option value="variable">variable</option>
                            <option value="file">file</option>
                          </Select>
                        </td>
                        <td className="px-3 py-1.5">
                          <StatusBadge status={row.status} inherited={row.current?.inherited} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3 border-t border-border px-5 py-3">
        <span className="text-xs text-dim">
          {counts.new} new · {counts.changed + counts.exists} changed · {counts.same} unchanged
          {compare.data === null && rows.length > 0 && (
            <> · you can't read values here, so existing keys can't be compared</>
          )}
        </span>
        {error && <span className="text-xs text-danger">{error}</span>}
        <div className="ml-auto flex gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={busy || included.length === 0 || Boolean(parsed.error) || !env}
            onClick={() => {
              void apply()
            }}
          >
            {busy && <Loader2 className="size-3.5 animate-spin" />}
            {busy
              ? `Importing ${String(progress)}/${String(included.length)}`
              : `Import ${String(included.length)} ${included.length === 1 ? 'key' : 'keys'}`}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

function StatusBadge({ status, inherited }: { status: Status; inherited?: boolean | undefined }) {
  const label =
    status === 'same'
      ? 'unchanged'
      : status === 'exists'
        ? 'exists'
        : status === 'checking'
          ? '…'
          : status
  return (
    <span
      className={cn(
        'whitespace-nowrap rounded px-1.5 py-0.5 text-2xs font-medium',
        status === 'new' && 'bg-success-bg text-success',
        (status === 'changed' || status === 'exists') && 'bg-amber-bg text-amber-fg',
        (status === 'same' || status === 'checking') && 'bg-elev text-dim',
      )}
      title={
        inherited && status === 'changed'
          ? 'Inherited here; importing sets a value in this environment that wins over it.'
          : undefined
      }
    >
      {label}
      {inherited && status === 'changed' ? ' · overrides' : ''}
    </span>
  )
}

function rowStatus({
  current,
  kind,
  folder,
  serverStatus,
  checking,
  compared,
}: {
  current: Secret | undefined
  kind: Kind
  folder: string
  serverStatus: string | undefined
  checking: boolean
  compared: boolean
}): Status {
  if (!current) return 'new'
  if (serverStatus === undefined) {
    if (checking) return 'checking'
    return compared ? 'checking' : 'exists'
  }
  if (serverStatus === 'new') return 'new'
  if (serverStatus === 'changed') return 'changed'
  // Same value: still a change when the kind or folder moves (own keys only;
  // inherited keys would just be copied here).
  if (!current.inherited && (current.kind !== kind || (current.folder ?? '') !== folder)) {
    return 'changed'
  }
  return 'same'
}

function asKind(kind: string | undefined): Kind | undefined {
  return kind === 'secret' || kind === 'variable' || kind === 'file' ? kind : undefined
}

const variableKeys =
  /(^|_)(HOST|HOSTNAME|PORT|ENV|ENVIRONMENT|MODE|REGION|LOG_LEVEL|LEVEL|DEBUG|ENABLED|DISABLED|TIMEOUT|TTL|NAME|VERSION|LOCALE|TZ)$|^(NODE_ENV|APP_ENV|RAILS_ENV|PUBLIC_|NEXT_PUBLIC_|VITE_|REACT_APP_|EXPO_PUBLIC_)/

/** A first guess for a new key; existing keys keep their kind. */
export function guessKind(key: string, value: string): Kind {
  if (value.includes('\n') || value.startsWith('-----BEGIN')) return 'file'
  // URLs with credentials stay secret.
  if (/:\/\/[^/\s]*@/.test(value)) return 'secret'
  if (variableKeys.test(key.toUpperCase())) return 'variable'
  return 'secret'
}

function normalizeFolder(folder: string): string {
  const trimmed = folder.trim().replace(/\/+$/, '')
  if (trimmed === '' || trimmed === '/') return ''
  return trimmed.startsWith('/') ? trimmed : `/${trimmed}`
}

function parseImportText(text: string): { entries: ParsedEntry[]; error: string | null } {
  const trimmed = text.trim()
  if (trimmed.startsWith('{')) {
    try {
      const raw = JSON.parse(trimmed) as Record<string, unknown>
      const entries: ParsedEntry[] = []
      for (const [key, value] of Object.entries(raw)) {
        if (value === null || value === undefined) entries.push({ key, value: '' })
        else if (typeof value === 'string') entries.push({ key, value })
        else if (typeof value === 'number' || typeof value === 'boolean')
          entries.push({ key, value: String(value) })
        else entries.push({ key, value: JSON.stringify(value) })
      }
      return { entries, error: null }
    } catch (error) {
      return { entries: [], error: error instanceof Error ? error.message : 'Invalid JSON.' }
    }
  }
  const parsed = parseDotenv(text)
  return { entries: parsed.entries, error: parsed.error }
}
