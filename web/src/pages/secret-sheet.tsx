import { useQuery } from '@tanstack/react-query'
import {
  Check,
  Copy,
  Download,
  ExternalLink,
  Eye,
  EyeOff,
  History,
  Loader2,
  Lock,
  Plus,
  ShieldAlert,
  Trash2,
  Undo2,
  X,
} from 'lucide-react'
import { useEffect, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { askRevealReason, confirmAction, confirmDialog } from '@/components/ui/dialogs'
import { Field, Input, Notice, Select, Textarea } from '@/components/ui/field'
import { Modal } from '@/components/ui/modal'
import { toast } from '@/components/ui/toast'
import { apiErrorMessage } from '@/lib/api-error'
import { validateSchemaValue, type SchemaEntry } from '@/lib/schema'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']
type Secret = components['schemas']['SecretBody']
type SecretVersion = components['schemas']['SecretVersionBody']

export const envClassNames: Record<string, string> = {
  development: 'env-development',
  preview: 'env-preview',
  staging: 'env-staging',
  production: 'env-production',
}
export const envShort: Record<string, string> = {
  development: 'dev',
  preview: 'preview',
  staging: 'staging',
  production: 'prod',
}

/**
 * SecretSheet is the detail panel for one key: per-environment value,
 * metadata, and history. Values stay masked until the person reveals them
 * (with a reason in production); metadata edits never decrypt the value in
 * the browser.
 */
export function SecretSheet({
  orgSlug,
  projectSlug,
  secretKey,
  envs,
  initialEnv,
  secretIn,
  canWrite,
  schema,
  onClose,
  onChanged,
}: {
  orgSlug: string
  projectSlug: string
  secretKey: string
  envs: Env[]
  initialEnv: string
  secretIn: (env: string) => Secret | undefined
  canWrite: (env: Env) => boolean
  schema: SchemaEntry | undefined
  onClose: () => void
  onChanged: () => void
}) {
  const [envSlug, setEnvSlug] = useState(initialEnv)
  const env = envs.find((candidate) => candidate.slug === envSlug) ?? envs[0]
  const listed = env ? secretIn(env.slug) : undefined

  return (
    <Modal onClose={onClose} variant="sheet" className="max-w-xl">
      <div className="flex items-start justify-between gap-3 border-b border-border px-5 py-4">
        <div className="min-w-0">
          <h2 className="truncate font-mono text-md font-semibold">{secretKey}</h2>
          <p className="mt-0.5 text-xs text-dim">
            {schema ? `${schema.type} · ` : ''}
            {listed ? `${listed.kind} · v${String(listed.version)}` : 'not set here'}
            {listed?.owner_group_name ? ` · owned by ${listed.owner_group_name}` : ''}
          </p>
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
          <X className="size-4" />
        </Button>
      </div>

      <div role="tablist" aria-label="Environment" className="flex gap-1 overflow-x-auto px-5 pt-3">
        {envs.map((candidate) => {
          const present = Boolean(secretIn(candidate.slug))
          const selected = candidate.slug === env?.slug
          return (
            <button
              key={candidate.id}
              type="button"
              role="tab"
              aria-selected={selected}
              onClick={() => {
                setEnvSlug(candidate.slug)
              }}
              className={cn(
                'inline-flex h-8 items-center gap-1.5 rounded-app-sm border px-2.5 text-xs',
                selected
                  ? 'border-accent-line bg-accent-soft text-text'
                  : 'border-transparent text-muted hover:bg-hover hover:text-text',
              )}
            >
              <span className={cn('env-chip', envClassNames[candidate.type])}>
                {candidate.slug}
              </span>
              {candidate.protected && <Lock className="size-3 text-dim" aria-label="protected" />}
              {!present && <span className="text-3xs text-dim">unset</span>}
            </button>
          )
        })}
      </div>

      {env && (
        <EnvPanel
          key={env.slug + ':' + String(listed?.version ?? 0)}
          orgSlug={orgSlug}
          projectSlug={projectSlug}
          env={env}
          secretKey={secretKey}
          listed={listed}
          writable={canWrite(env)}
          schema={schema}
          onClose={onClose}
          onChanged={onChanged}
        />
      )}
    </Modal>
  )
}

function EnvPanel({
  orgSlug,
  projectSlug,
  env,
  secretKey,
  listed,
  writable,
  schema,
  onClose,
  onChanged,
}: {
  orgSlug: string
  projectSlug: string
  env: Env
  secretKey: string
  listed: Secret | undefined
  writable: boolean
  schema: SchemaEntry | undefined
  onClose: () => void
  onChanged: () => void
}) {
  const path = { org: orgSlug, project: projectSlug, env: env.slug, key: secretKey }
  const isProd = env.type === 'production'
  const exists = Boolean(listed) && !listed?.inherited

  // Metadata (no value) — reading it is not a reveal.
  const meta = useQuery({
    queryKey: ['secret-meta', orgSlug, projectSlug, env.slug, secretKey, listed?.version],
    enabled: Boolean(listed),
    queryFn: async () => {
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}',
        { params: { path } },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load details.'))
      return result.data
    },
  })
  const source = meta.data ?? listed

  const [revealed, setRevealed] = useState<string | null>(
    listed?.kind === 'variable' ? (listed.value ?? '') : null,
  )
  const [revealing, setRevealing] = useState(false)
  const [copied, setCopied] = useState(false)
  const [replacing, setReplacing] = useState(!listed)
  const [value, setValue] = useState('')
  const [kind, setKind] = useState(listed?.kind ?? 'secret')
  const [comment, setComment] = useState('')
  const [folder, setFolder] = useState('')
  const [notes, setNotes] = useState('')
  const [providerURL, setProviderURL] = useState('')
  const [ownerGroupID, setOwnerGroupID] = useState('')
  const [tags, setTags] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)

  useEffect(() => {
    if (!source || dirty) return
    setKind(source.kind)
    setComment(source.comment ?? '')
    setFolder(source.folder ?? '')
    setNotes(source.notes ?? '')
    setProviderURL(source.provider_url ?? '')
    setOwnerGroupID(source.owner_group_id ?? '')
    setTags((source.tags ?? []).join(', '))
    setExpiresAt((source.expires_at ?? '').slice(0, 10))
    // Only re-seed when fresh metadata arrives.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [meta.data])

  const groups = useQuery({
    queryKey: ['groups', orgSlug],
    queryFn: async () => {
      const result = await api.GET('/api/v1/orgs/{org}/groups', {
        params: { path: { org: orgSlug } },
      })
      if (result.error) return []
      return result.data?.groups ?? []
    },
  })

  const history = useQuery({
    queryKey: ['versions', orgSlug, projectSlug, env.slug, secretKey, listed?.version],
    enabled: exists,
    queryFn: async () => {
      const result = await api.GET(
        '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/versions',
        { params: { path } },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load history.'))
      return result.data?.versions ?? []
    },
  })

  function touch<T>(setter: (value: T) => void) {
    return (next: T) => {
      setter(next)
      setDirty(true)
    }
  }

  async function reveal() {
    if (revealed !== null) {
      setRevealed(null)
      return
    }
    let reason = 'Viewed in the web UI'
    if (isProd) {
      const entered = await askRevealReason(`${secretKey} in ${env.slug}`, true)
      if (!entered) return
      reason = entered
    }
    setRevealing(true)
    const result = await api.GET(
      '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}',
      { params: { path, query: { reveal: true, reason } } },
    )
    setRevealing(false)
    if (result.error || !result.data) {
      toast(apiErrorMessage(result.error, 'Could not reveal the value.'), { tone: 'error' })
      return
    }
    setRevealed(result.data.value ?? '')
  }

  async function copy() {
    if (revealed === null) return
    await navigator.clipboard.writeText(revealed)
    setCopied(true)
    window.setTimeout(() => {
      setCopied(false)
    }, 1500)
  }

  const validation = replacing && value !== '' ? validateSchemaValue(schema, value) : null

  async function save() {
    if (validation) return
    if (isProd) {
      const ok = await confirmDialog({
        title: `Write to ${env.name}?`,
        body: env.protected
          ? 'This is a protected production environment. The change is audited and applies immediately.'
          : 'This changes production and applies immediately. The change is audited.',
        confirmLabel: 'Save to production',
      })
      if (!ok) return
    }
    setBusy(true)
    setError(null)
    const body = {
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
      message: message || (exists ? 'Updated from the web UI' : 'Created from the web UI'),
    }
    const result = exists
      ? await api.PUT('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}', {
          params: { path },
          body: replacing ? { ...body, value } : { ...body, value: '', keep_value: true },
        })
      : await api.POST('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets', {
          params: { path: { org: orgSlug, project: projectSlug, env: env.slug } },
          body: { ...body, key: secretKey, value },
        })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not save.'))
      return
    }
    toast(
      exists
        ? `Saved ${secretKey} in ${env.slug} (v${String(result.data?.version ?? '')}).`
        : `Added ${secretKey} to ${env.slug}.`,
    )
    onChanged()
  }

  async function remove() {
    if (
      !(await confirmAction(
        `Delete ${secretKey} from ${env.slug}? Its history is kept and it can be restored from a snapshot.`,
      ))
    )
      return
    setBusy(true)
    const result = await api.DELETE(
      '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}',
      { params: { path } },
    )
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not delete.'))
      return
    }
    toast(`Deleted ${secretKey} from ${env.slug}.`)
    onChanged()
    onClose()
  }

  async function restore(version: number) {
    if (
      !(await confirmDialog({
        title: `Restore v${String(version)}?`,
        body: `The value from v${String(version)} becomes a new version of ${secretKey} in ${env.slug}.`,
        confirmLabel: 'Restore',
      }))
    )
      return
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
    toast(`Restored ${secretKey} to v${String(version)} as a new version.`)
    onChanged()
  }

  const canSave = writable && !busy && (replacing ? value !== '' : dirty)

  return (
    <>
      <div className="flex-1 space-y-5 overflow-y-auto px-5 py-4">
        {isProd && (
          <Notice tone="warning" className="flex items-start gap-2">
            <ShieldAlert className="mt-px size-3.5 shrink-0" />
            <span>
              Production. Reveals ask for a reason and every change is audited
              {env.protected ? '; only Admins can write here' : ''}.
            </span>
          </Notice>
        )}
        {!writable && (
          <Notice tone="info">
            You can view this environment but not change it. Ask an Admin, or request access from
            the Requests page.
          </Notice>
        )}
        {listed?.inherited && (
          <Notice tone="info">
            Inherited from <span className="font-mono">{listed.source_environment}</span>. Saving a
            value here creates a local override.
          </Notice>
        )}

        <section aria-label="Value" className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-medium text-muted">Value</h3>
            {listed && kind === 'file' && revealed !== null && (
              <button
                type="button"
                className="inline-flex items-center gap-1 text-2xs text-accent-text hover:underline"
                onClick={() => {
                  const blob = new Blob([revealed], { type: 'application/octet-stream' })
                  const href = URL.createObjectURL(blob)
                  const link = document.createElement('a')
                  link.href = href
                  link.download = secretKey
                  link.click()
                  URL.revokeObjectURL(href)
                }}
              >
                <Download className="size-3" /> Download
              </button>
            )}
          </div>
          {listed && !replacing && (
            <div className="flex items-center gap-2">
              <div
                className={cn(
                  'min-h-9 min-w-0 flex-1 rounded-app-sm border border-border bg-bg px-2.5 py-2 font-mono text-xs break-all',
                  revealed === null ? 'text-dim' : 'text-text',
                )}
              >
                {revealed === null ? '••••••••••••' : revealed || '(empty)'}
              </div>
              {listed.kind !== 'variable' && (
                <Button
                  size="sm"
                  onClick={() => {
                    void reveal()
                  }}
                  disabled={revealing}
                  aria-label={revealed === null ? 'Reveal value' : 'Hide value'}
                >
                  {revealing ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : revealed === null ? (
                    <Eye className="size-3.5" />
                  ) : (
                    <EyeOff className="size-3.5" />
                  )}
                  {revealed === null ? 'Reveal' : 'Hide'}
                </Button>
              )}
              {revealed !== null && (
                <Button
                  size="icon"
                  onClick={() => {
                    void copy()
                  }}
                  aria-label="Copy value"
                >
                  {copied ? (
                    <Check className="size-3.5 text-success" />
                  ) : (
                    <Copy className="size-3.5" />
                  )}
                </Button>
              )}
            </div>
          )}
          {replacing ? (
            <div className="space-y-1.5">
              <Textarea
                mono
                autoFocus={Boolean(listed)}
                rows={kind === 'file' ? 6 : 3}
                placeholder={listed ? 'New value' : `Value for ${env.slug}`}
                aria-label="New value"
                aria-invalid={validation ? true : undefined}
                value={value}
                spellCheck={false}
                autoComplete="off"
                onChange={(event) => {
                  setValue(event.target.value)
                }}
              />
              {validation && <p className="text-2xs text-danger">{validation}</p>}
              {kind === 'file' && (
                <input
                  type="file"
                  className="block w-full text-xs text-muted"
                  onChange={(event) => {
                    const file = event.target.files?.[0]
                    if (!file) return
                    if (file.size > 1024 * 1024) {
                      setError('File secrets must be 1 MiB or smaller.')
                      return
                    }
                    void file.text().then(setValue)
                  }}
                />
              )}
              {listed && (
                <button
                  type="button"
                  className="text-2xs text-muted hover:text-text"
                  onClick={() => {
                    setReplacing(false)
                    setValue('')
                  }}
                >
                  Keep the current value
                </button>
              )}
            </div>
          ) : (
            writable && (
              <button
                type="button"
                className="text-2xs font-medium text-accent-text hover:underline"
                onClick={() => {
                  setReplacing(true)
                }}
              >
                Replace value…
              </button>
            )
          )}
        </section>

        <section aria-label="Details" className="grid gap-3 sm:grid-cols-2">
          <Field label="Kind">
            <Select
              value={kind}
              disabled={!writable}
              onChange={(event) => {
                touch(setKind)(event.target.value)
              }}
            >
              <option value="secret">Secret — masked, reveals audited</option>
              <option value="variable">Variable — plain configuration</option>
              <option value="file">File — masked, downloadable</option>
            </Select>
          </Field>
          <Field label="Owner group">
            <Select
              value={ownerGroupID}
              disabled={!writable}
              onChange={(event) => {
                touch(setOwnerGroupID)(event.target.value)
              }}
            >
              <option value="">No owner</option>
              {(groups.data ?? []).map((group) => (
                <option key={group.id} value={group.id}>
                  {group.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Comment" className="sm:col-span-2">
            <Input
              value={comment}
              readOnly={!writable}
              placeholder="What this is for"
              onChange={(event) => {
                touch(setComment)(event.target.value)
              }}
            />
          </Field>
          <Field label="Folder">
            <Input
              mono
              value={folder}
              readOnly={!writable}
              placeholder="/backend/db"
              onChange={(event) => {
                touch(setFolder)(event.target.value)
              }}
            />
          </Field>
          <Field label="Expires on" hint="You get a reminder before it lapses.">
            <Input
              type="date"
              value={expiresAt}
              readOnly={!writable}
              onChange={(event) => {
                touch(setExpiresAt)(event.target.value)
              }}
            />
          </Field>
          <Field label="Tags" className="sm:col-span-2">
            <Input
              value={tags}
              readOnly={!writable}
              placeholder="billing, stripe"
              onChange={(event) => {
                touch(setTags)(event.target.value)
              }}
            />
          </Field>
          <Field label="Provider URL" className="sm:col-span-2">
            <div className="flex gap-2">
              <Input
                value={providerURL}
                readOnly={!writable}
                placeholder="https://dashboard.stripe.com/apikeys"
                onChange={(event) => {
                  touch(setProviderURL)(event.target.value)
                }}
              />
              {/^https:\/\//.test(providerURL) && (
                <a
                  href={providerURL}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="inline-flex size-9 shrink-0 items-center justify-center rounded-app-sm border border-border text-dim hover:text-text"
                  aria-label="Open provider"
                >
                  <ExternalLink className="size-3.5" />
                </a>
              )}
            </div>
          </Field>
          <Field label="Runbook notes" className="sm:col-span-2">
            <Textarea
              rows={3}
              value={notes}
              readOnly={!writable}
              placeholder="How to rotate it, who owns it, what breaks."
              onChange={(event) => {
                touch(setNotes)(event.target.value)
              }}
            />
          </Field>
        </section>

        {exists && (
          <section aria-label="History">
            <h3 className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-muted">
              <History className="size-3.5" /> History
            </h3>
            <ol className="divide-y divide-border-soft rounded-app-sm border border-border">
              {(history.data ?? []).map((version: SecretVersion) => (
                <li key={version.version} className="flex items-center gap-2 px-2.5 py-1.5 text-xs">
                  <span className="font-mono text-dim">v{version.version}</span>
                  <span className="min-w-0 flex-1 truncate text-muted">
                    {version.change_message || 'No message'}
                  </span>
                  <time className="text-2xs text-dim" dateTime={version.created_at}>
                    {new Date(version.created_at).toLocaleString()}
                  </time>
                  {version.version !== listed?.version && writable && (
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 rounded px-1 py-0.5 text-2xs text-dim hover:bg-hover hover:text-text"
                      onClick={() => {
                        void restore(version.version)
                      }}
                      disabled={busy}
                    >
                      <Undo2 className="size-3" /> Restore
                    </button>
                  )}
                  {version.version === listed?.version && (
                    <span className="rounded bg-accent-soft px-1.5 text-3xs font-semibold text-accent-text">
                      current
                    </span>
                  )}
                </li>
              ))}
              {history.isLoading && <li className="px-2.5 py-2 text-xs text-dim">Loading…</li>}
            </ol>
          </section>
        )}
      </div>

      {writable && (
        <div className="space-y-2 border-t border-border px-5 py-3">
          {(dirty || replacing) && (
            <Input
              placeholder="Change message (optional) — why did this change?"
              value={message}
              onChange={(event) => {
                setMessage(event.target.value)
              }}
            />
          )}
          {error && <Notice>{error}</Notice>}
          <div className="flex items-center justify-between gap-2">
            {exists ? (
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
            ) : (
              <span />
            )}
            <div className="flex gap-2">
              <Button variant="ghost" size="sm" onClick={onClose}>
                Close
              </Button>
              <Button
                variant="primary"
                size="sm"
                disabled={!canSave || Boolean(validation)}
                onClick={() => {
                  void save()
                }}
              >
                {busy ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : exists ? (
                  <Check className="size-3.5" />
                ) : (
                  <Plus className="size-3.5" />
                )}
                {exists ? 'Save' : `Add to ${env.slug}`}
              </Button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
