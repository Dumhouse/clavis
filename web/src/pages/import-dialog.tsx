import { useMemo, useState } from 'react'
import { Loader2, X } from 'lucide-react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { parseDotenv } from '@/lib/dotenv'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']
type Secret = components['schemas']['SecretBody']
type Kind = 'secret' | 'variable' | 'file'

type ParsedEntry = { key: string; value: string }
type Edit = { key: string; folder: string; kind: Kind }

const inputClass =
  'h-8 w-full rounded border border-border bg-bg px-2 text-[12.5px] text-text focus:border-accent-line focus:outline-none'

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
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const parsed = useMemo(() => parseImportText(text), [text])
  const rows = useMemo(
    () =>
      parsed.entries.map((entry) => {
        const edit = edits[entry.key]
        return {
          source: entry.key,
          value: entry.value,
          key: edit?.key ?? entry.key,
          folder: edit?.folder ?? '',
          kind: edit?.kind ?? 'secret',
        }
      }),
    [parsed.entries, edits],
  )
  const existing = useMemo(() => {
    const map = new Map<string, string>()
    for (const secret of secretsForEnv(env)) map.set(secret.key, secret.value ?? '')
    return map
  }, [env, secretsForEnv])

  const diff = useMemo(() => {
    let added = 0
    let changed = 0
    let unchanged = 0
    for (const row of rows) {
      if (!existing.has(row.key)) added++
      else if (existing.get(row.key) !== row.value) changed++
      else unchanged++
    }
    return { added, changed, unchanged }
  }, [rows, existing])

  function edit(source: string, patch: Partial<Edit>) {
    setEdits((current) => ({
      ...current,
      [source]: {
        key: current[source]?.key ?? source,
        folder: current[source]?.folder ?? '',
        kind: current[source]?.kind ?? 'secret',
        ...patch,
      },
    }))
  }

  async function apply() {
    if (parsed.error || rows.length === 0 || !env) return
    const seen = new Set<string>()
    for (const row of rows) {
      if (!/^[A-Za-z_][A-Za-z0-9_.-]*$/.test(row.key)) {
        setError(`${row.source}: target key is not valid.`)
        return
      }
      if (seen.has(row.key)) {
        setError(`${row.key}: target key is duplicated.`)
        return
      }
      seen.add(row.key)
    }
    setBusy(true)
    setError(null)
    let written = 0
    for (const row of rows) {
      const path = { org: orgSlug, project: projectSlug, env, key: row.key }
      const payload = {
        value: row.value,
        kind: row.kind,
        folder: row.folder,
        message: 'Imported from the mapping preview',
      }
      const result = existing.has(row.key)
        ? await api.PUT('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}', {
            params: { path },
            body: payload,
          })
        : await api.POST('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets', {
            params: { path: { org: orgSlug, project: projectSlug, env } },
            body: { key: row.key, ...payload },
          })
      if (result.error) {
        setError(`${row.key}: ${apiErrorMessage(result.error, 'import failed')}`)
        setBusy(false)
        return
      }
      written++
    }
    setBusy(false)
    onDone(`Imported ${String(written)} keys into ${env}.`)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="flex max-h-[90vh] w-full max-w-3xl flex-col rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[18px] font-semibold">Import secrets</h2>
            <p className="mt-0.5 text-[13px] text-muted">
              Paste .env or a flat JSON object. Map keys, folders, and kinds locally, then review
              the dry-run diff before writing.
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <X className="size-4" />
          </Button>
        </div>

        <div className="mt-3 grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">
              Target environment
            </span>
            <select
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
              value={env}
              onChange={(event) => {
                setEnv(event.target.value)
              }}
            >
              {envs.map((candidate) => (
                <option key={candidate.id} value={candidate.slug}>
                  {candidate.name} ({candidate.slug})
                </option>
              ))}
            </select>
          </label>
          <span className="text-[12px] text-dim">
            {diff.added} new · {diff.changed} changed · {diff.unchanged} unchanged
          </span>
        </div>

        <textarea
          className="mt-3 min-h-32 w-full resize-y rounded-app-sm border border-border bg-bg px-2.5 py-2 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
          placeholder={'DATABASE_URL=postgres://…\n# or {"DATABASE_URL":"postgres://…"}'}
          value={text}
          onChange={(event) => {
            setText(event.target.value)
            setEdits({})
          }}
          spellCheck={false}
        />

        {parsed.error && (
          <p className="mt-2 rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger">
            {parsed.error}
          </p>
        )}
        {error && (
          <p className="mt-2 rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger">
            {error}
          </p>
        )}

        {rows.length > 0 && !parsed.error && (
          <div className="mt-3 max-h-64 overflow-auto rounded-app-sm border border-border">
            <table className="w-full min-w-[640px] text-left text-[12.5px]">
              <thead className="sticky top-0 bg-surface">
                <tr className="border-b border-border text-[11px] uppercase tracking-[0.05em] text-dim">
                  <th className="px-2.5 py-1.5 font-semibold">Source</th>
                  <th className="px-2.5 py-1.5 font-semibold">Target key</th>
                  <th className="px-2.5 py-1.5 font-semibold">Folder</th>
                  <th className="px-2.5 py-1.5 font-semibold">Kind</th>
                  <th className="px-2.5 py-1.5 font-semibold">Diff</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const status = !existing.has(row.key)
                    ? 'new'
                    : existing.get(row.key) === row.value
                      ? 'same'
                      : 'changed'
                  return (
                    <tr key={row.source} className="border-b border-border-soft last:border-0">
                      <td className="px-2.5 py-1.5 font-mono text-dim">{row.source}</td>
                      <td className="px-2.5 py-1.5">
                        <input
                          className={inputClass}
                          value={row.key}
                          onChange={(event) => {
                            edit(row.source, { key: event.target.value })
                          }}
                        />
                      </td>
                      <td className="px-2.5 py-1.5">
                        <input
                          className={inputClass}
                          placeholder="/"
                          value={row.folder}
                          onChange={(event) => {
                            edit(row.source, { folder: event.target.value })
                          }}
                        />
                      </td>
                      <td className="px-2.5 py-1.5">
                        <select
                          className={inputClass}
                          value={row.kind}
                          onChange={(event) => {
                            edit(row.source, { kind: event.target.value as Kind })
                          }}
                        >
                          <option value="secret">secret</option>
                          <option value="variable">variable</option>
                          <option value="file">file</option>
                        </select>
                      </td>
                      <td className="px-2.5 py-1.5">
                        <span
                          className={cn(
                            'rounded px-1.5 py-0.5 text-[11px] font-medium',
                            status === 'new' && 'bg-success-bg text-success',
                            status === 'changed' && 'bg-amber-bg text-amber-fg',
                            status === 'same' && 'bg-elev text-dim',
                          )}
                        >
                          {status}
                        </span>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={busy || rows.length === 0 || Boolean(parsed.error) || !env}
            onClick={() => {
              void apply()
            }}
          >
            {busy && <Loader2 className="size-3.5 animate-spin" />}
            Apply {diff.added + diff.changed} changes
          </Button>
        </div>
      </div>
    </div>
  )
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
