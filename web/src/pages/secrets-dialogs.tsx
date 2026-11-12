import { useQuery } from '@tanstack/react-query'
import { Check, Loader2, Trash2, Undo2, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { parseDotenv } from '@/lib/dotenv'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']
type Secret = components['schemas']['SecretBody']
type SecretVersion = components['schemas']['SecretVersionBody']

const inputClass =
  'h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none'

export function PasteEnvDialog({
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
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const parsed = useMemo(() => parseDotenv(text), [text])
  const existing = useMemo(() => {
    const map = new Map<string, string>()
    for (const secret of secretsForEnv(env)) map.set(secret.key, secret.value ?? '')
    return map
  }, [env, secretsForEnv])

  const diff = useMemo(() => {
    const added: string[] = []
    const changed: string[] = []
    const unchanged: string[] = []
    for (const entry of parsed.entries) {
      if (!existing.has(entry.key)) added.push(entry.key)
      else if (existing.get(entry.key) !== entry.value) changed.push(entry.key)
      else unchanged.push(entry.key)
    }
    return { added, changed, unchanged }
  }, [parsed.entries, existing])

  async function apply() {
    if (parsed.error || parsed.entries.length === 0 || !env) return
    setBusy(true)
    setError(null)
    let written = 0
    for (const entry of parsed.entries) {
      const path = { org: orgSlug, project: projectSlug, env, key: entry.key }
      const payload = {
        value: entry.value,
        kind: 'secret' as const,
        message: 'Pasted from .env',
      }
      if (existing.has(entry.key)) {
        const result = await api.PUT(
          '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}',
          { params: { path }, body: payload },
        )
        if (result.error) {
          setError(`${entry.key}: ${apiErrorMessage(result.error, 'update failed')}`)
          setBusy(false)
          return
        }
      } else {
        const result = await api.POST(
          '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets',
          {
            params: { path: { org: orgSlug, project: projectSlug, env } },
            body: { key: entry.key, ...payload },
          },
        )
        if (result.error) {
          setError(`${entry.key}: ${apiErrorMessage(result.error, 'create failed')}`)
          setBusy(false)
          return
        }
      }
      written++
    }
    setBusy(false)
    onDone(`Applied ${String(written)} keys to ${env}.`)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="flex max-h-[90vh] w-full max-w-2xl flex-col rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-head text-[18px] font-semibold">Paste .env</h2>
            <p className="mt-0.5 text-[13px] text-muted">
              Parsed locally. Nothing is written until you review the diff and apply.
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
              className={inputClass}
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
            {diff.added.length} new · {diff.changed.length} changed · {diff.unchanged.length}{' '}
            unchanged
          </span>
        </div>

        <textarea
          className="mt-3 min-h-40 w-full flex-1 resize-y rounded-app-sm border border-border bg-bg px-2.5 py-2 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
          placeholder={
            'DATABASE_URL=postgres://…\nexport STRIPE_KEY="sk_live_…"\n# comments are ignored'
          }
          value={text}
          onChange={(event) => {
            setText(event.target.value)
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

        {parsed.entries.length > 0 && !parsed.error && (
          <div className="mt-3 max-h-40 overflow-y-auto rounded-app-sm border border-border">
            <table className="w-full text-left text-[12.5px]">
              <tbody>
                {parsed.entries.map((entry) => {
                  const status = !existing.has(entry.key)
                    ? 'new'
                    : existing.get(entry.key) === entry.value
                      ? 'same'
                      : 'changed'
                  return (
                    <tr key={entry.key} className="border-b border-border-soft last:border-0">
                      <td className="px-2.5 py-1.5 font-mono text-text">{entry.key}</td>
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
                      <td className="max-w-64 truncate px-2.5 py-1.5 font-mono text-dim">
                        {entry.value}
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
            disabled={busy || parsed.entries.length === 0 || Boolean(parsed.error) || !env}
            onClick={() => {
              void apply()
            }}
          >
            {busy && <Loader2 className="size-3.5 animate-spin" />}
            Apply {diff.added.length + diff.changed.length} changes
          </Button>
        </div>
      </div>
    </div>
  )
}

export function EditSecretDialog({
  orgSlug,
  projectSlug,
  env,
  secret,
  onClose,
  onDone,
  onDeleted,
  readOnly = false,
}: {
  orgSlug: string
  projectSlug: string
  env: string
  secret: Secret
  onClose: () => void
  onDone: (message: string) => void
  onDeleted?: (key: string, value: string) => void
  readOnly?: boolean
}) {
  const [value, setValue] = useState('')
  const [kind, setKind] = useState(secret.kind)
  const [comment, setComment] = useState(secret.comment ?? '')
  const [folder, setFolder] = useState(secret.folder ?? '')
  const [notes, setNotes] = useState(secret.notes ?? '')
  const [providerURL, setProviderURL] = useState(secret.provider_url ?? '')
  const [ownerGroupID, setOwnerGroupID] = useState(secret.owner_group_id ?? '')
  const [tags, setTags] = useState((secret.tags ?? []).join(', '))
  const [expiresAt, setExpiresAt] = useState((secret.expires_at ?? '').slice(0, 10))
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const path = { org: orgSlug, project: projectSlug, env, key: secret.key }

  useEffect(() => {
    let active = true
    void (async () => {
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}',
        { params: { path, query: { reveal: true, reason: 'Editing in the web UI' } } },
      )
      if (!active) return
      if (result.error || !result.data) {
        setError(apiErrorMessage(result.error, 'Could not load the value.'))
      } else {
        setValue(result.data.value ?? '')
        setKind(result.data.kind)
        setComment(result.data.comment ?? '')
        setFolder(result.data.folder ?? '')
        setNotes(result.data.notes ?? '')
        setProviderURL(result.data.provider_url ?? '')
        setOwnerGroupID(result.data.owner_group_id ?? '')
        setTags((result.data.tags ?? []).join(', '))
        setExpiresAt((result.data.expires_at ?? '').slice(0, 10))
      }
      setLoaded(true)
    })()
    return () => {
      active = false
    }
    // path fields are stable for the dialog's lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const groups = useQuery({
    queryKey: ['groups', orgSlug],
    queryFn: async () => {
      const result = await api.GET('/api/v1/orgs/{org}/groups', {
        params: { path: { org: orgSlug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load groups.'))
      return result.data?.groups ?? []
    },
  })

  const history = useQuery({
    queryKey: ['versions', orgSlug, projectSlug, env, secret.key],
    queryFn: async () => {
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/versions',
        { params: { path } },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load history.'))
      return result.data?.versions ?? []
    },
  })

  async function save() {
    setBusy(true)
    setError(null)
    const result = await api.PUT(
      '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}',
      {
        params: { path },
        body: {
          value,
          kind: kind as 'secret' | 'variable' | 'file',
          comment,
          folder,
          notes,
          provider_url: providerURL,
          owner_group_id: ownerGroupID,
          tags: tags
            .split(',')
            .map((tag) => tag.trim())
            .filter((tag) => tag.length > 0),
          ...(expiresAt ? { expires_at: `${expiresAt}T00:00:00Z` } : {}),
          message: message || 'Updated from the web UI',
        },
      },
    )
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not save the secret.'))
      return
    }
    onDone(`Saved ${secret.key} (v${String(result.data?.version ?? '')}).`)
  }

  async function remove() {
    if (!window.confirm(`Delete ${secret.key} from ${env}? History is kept.`)) return
    setBusy(true)
    const result = await api.DELETE(
      '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}',
      { params: { path } },
    )
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not delete the secret.'))
      return
    }
    onDeleted?.(secret.key, value)
    onDone(`Deleted ${secret.key}.`)
  }

  async function restore(version: number) {
    setBusy(true)
    const result = await api.POST(
      '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/rollback',
      { params: { path }, body: { version } },
    )
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not restore the version.'))
      return
    }
    onDone(`Restored ${secret.key} to v${String(version)} as a new version.`)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
      <div className="flex max-h-[90vh] w-full max-w-xl flex-col rounded-app border border-border bg-surface p-5 shadow-app">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="font-mono text-[16px] font-semibold">{secret.key}</h2>
            <p className="mt-0.5 text-[12.5px] text-dim">
              {secret.kind} · {env} · version {secret.version}
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <X className="size-4" />
          </Button>
        </div>

        {!loaded ? (
          <div className="mt-4 flex items-center gap-2 text-[13px] text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading value…
          </div>
        ) : (
          <div className="mt-4 space-y-3 overflow-y-auto">
            <label className="block">
              <span className="mb-1 flex items-center justify-between gap-2 text-[12.5px] font-medium text-muted">
                Value
                {kind === 'file' && value !== '' && (
                  <button
                    type="button"
                    className="text-[11.5px] font-normal text-accent-text hover:underline"
                    onClick={() => {
                      const blob = new Blob([value], { type: 'application/octet-stream' })
                      const href = URL.createObjectURL(blob)
                      const link = document.createElement('a')
                      link.href = href
                      link.download = secret.key
                      link.click()
                      URL.revokeObjectURL(href)
                    }}
                  >
                    Download
                  </button>
                )}
              </span>
              <textarea
                rows={kind === 'file' ? 6 : 3}
                className="w-full rounded-app-sm border border-border bg-bg px-2.5 py-2 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
                value={value}
                onChange={(event) => {
                  setValue(event.target.value)
                }}
              />
              {kind === 'file' && (
                <input
                  type="file"
                  className="mt-1.5 block w-full text-[12.5px] text-muted"
                  onChange={(event) => {
                    const file = event.target.files?.[0]
                    if (!file) return
                    if (file.size > 1024 * 1024) {
                      setError('File secrets must be 1 MiB or smaller.')
                      return
                    }
                    void file.text().then((content) => {
                      setValue(content)
                    })
                  }}
                />
              )}
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Comment</span>
              <input
                className={inputClass}
                value={comment}
                onChange={(event) => {
                  setComment(event.target.value)
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Kind</span>
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
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Owner group</span>
              <select
                className={inputClass}
                value={ownerGroupID}
                onChange={(event) => {
                  setOwnerGroupID(event.target.value)
                }}
              >
                <option value="">No owner group</option>
                {(groups.data ?? []).map((group) => (
                  <option key={group.id} value={group.id}>
                    {group.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Expires on</span>
              <input
                type="date"
                className={inputClass}
                value={expiresAt}
                readOnly={readOnly}
                onChange={(event) => {
                  setExpiresAt(event.target.value)
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Provider URL</span>
              <input
                className={inputClass}
                placeholder="https://dashboard.stripe.com/apikeys"
                value={providerURL}
                onChange={(event) => {
                  setProviderURL(event.target.value)
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Runbook notes</span>
              <textarea
                rows={3}
                readOnly={readOnly}
                className="w-full rounded-app-sm border border-border bg-bg px-2.5 py-2 text-[13px] text-text focus:border-accent-line focus:outline-none read-only:text-muted"
                placeholder="How to rotate this secret, who owns it, and what breaks."
                value={notes}
                onChange={(event) => {
                  setNotes(event.target.value)
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Folder</span>
              <input
                className={inputClass}
                placeholder="/backend/db"
                value={folder}
                onChange={(event) => {
                  setFolder(event.target.value)
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Tags</span>
              <input
                className={inputClass}
                placeholder="billing, stripe, payments"
                value={tags}
                onChange={(event) => {
                  setTags(event.target.value)
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">
                Change message
              </span>
              <input
                className={inputClass}
                placeholder="Why did this change?"
                value={message}
                onChange={(event) => {
                  setMessage(event.target.value)
                }}
              />
            </label>

            <div>
              <p className="mb-1 text-[12.5px] font-medium text-muted">History</p>
              <div className="max-h-36 divide-y divide-border-soft overflow-y-auto rounded-app-sm border border-border">
                {(history.data ?? []).map((version: SecretVersion) => (
                  <div
                    key={version.version}
                    className="flex items-center gap-2 px-2.5 py-1.5 text-[12px]"
                  >
                    <span className="font-mono text-dim">v{version.version}</span>
                    <span className="min-w-0 flex-1 truncate text-muted">
                      {version.change_message || 'No message'}
                    </span>
                    <span className="text-dim">
                      {new Date(version.created_at).toLocaleDateString()}
                    </span>
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 rounded px-1 py-0.5 text-dim hover:bg-hover hover:text-text"
                      onClick={() => {
                        void restore(version.version)
                      }}
                      disabled={busy || readOnly || version.version === secret.version}
                    >
                      <Undo2 className="size-3" /> restore
                    </button>
                  </div>
                ))}
                {history.isLoading && (
                  <p className="px-2.5 py-2 text-[12px] text-dim">Loading history…</p>
                )}
              </div>
            </div>
          </div>
        )}

        {error && (
          <p className="mt-3 rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger">
            {error}
          </p>
        )}

        <div className="mt-4 flex items-center justify-between gap-2">
          <Button
            variant="danger"
            size="sm"
            onClick={() => {
              void remove()
            }}
            disabled={busy}
          >
            <Trash2 className="size-3.5" /> Delete
          </Button>
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button
              variant="primary"
              size="sm"
              onClick={() => {
                void save()
              }}
              disabled={busy || !loaded}
            >
              {busy ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Check className="size-3.5" />
              )}{' '}
              Save
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
