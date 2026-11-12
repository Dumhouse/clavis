import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft, CheckCircle2, Loader2, Plus, Save, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import { api } from '@/api/client'
import { Cmd, PageIntro } from '@/components/page-intro'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import {
  draftError,
  fromDraft,
  schemaKinds,
  schemaTypes,
  toDraft,
  type SchemaDraft,
} from '@/lib/schema'
import { cn } from '@/lib/utils'
import { Input, Select } from '@/components/ui/field'

export function SchemaPage() {
  const { org } = useCurrentOrg()
  const params = useParams({ strict: false }) as { project?: string }
  const projectSlug = params.project ?? ''
  const queryClient = useQueryClient()
  const [drafts, setDrafts] = useState<SchemaDraft[]>([])
  const [message, setMessage] = useState<string | null>(null)

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

  useEffect(() => {
    setDrafts((schemaQuery.data?.schema ?? []).map(toDraft))
  }, [schemaQuery.data])

  const envs = projectQuery.data?.environments ?? []
  const canManage = org?.role === 'Owner' || org?.role === 'Admin'
  const errors = useMemo(() => drafts.map((draft) => draftError(draft, drafts)), [drafts])
  const hasErrors = errors.some((error) => error !== null)
  const dirty = useMemo(() => {
    const saved = (schemaQuery.data?.schema ?? []).map((entry) => JSON.stringify(toDraft(entry)))
    return JSON.stringify(drafts.map((entry) => JSON.stringify(entry))) !== JSON.stringify(saved)
  }, [drafts, schemaQuery.data])
  const busy = schemaQuery.isLoading || projectQuery.isLoading

  const save = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.PUT('/api/v1/orgs/{org}/projects/{project}/schema', {
        params: { path: { org: org.slug, project: projectSlug } },
        body: { schema: drafts.map(fromDraft) },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not save the schema.'))
      return result.data
    },
    onSuccess: async () => {
      setMessage('Schema saved. New writes are validated immediately.')
      await queryClient.invalidateQueries({ queryKey: ['schema', org?.slug, projectSlug] })
      await queryClient.invalidateQueries({ queryKey: ['secrets', org?.slug, projectSlug] })
    },
  })

  function addEntry() {
    setDrafts((current) => [
      ...current,
      {
        key: '',
        type: 'string',
        kind: 'secret',
        required: {},
        pattern: '',
        description: '',
        example: '',
        enum: [],
      },
    ])
  }

  function update(index: number, patch: Partial<SchemaDraft>) {
    setDrafts((current) =>
      current.map((entry, candidate) => (candidate === index ? { ...entry, ...patch } : entry)),
    )
  }

  function remove(index: number) {
    setDrafts((current) => current.filter((_, candidate) => candidate !== index))
  }

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/projects/$project"
          params={{ project: projectSlug }}
          className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> Secrets
        </Link>
        <div className="mt-2 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-head text-2xl font-semibold tracking-tight">Schema</h1>
            <p className="mt-1 text-sm text-muted">
              The typed contract for <span className="font-mono">{projectSlug}</span>. Values are
              checked on every write and by <span className="font-mono">clavis check</span>.
            </p>
          </div>
          {canManage && (
            <div className="flex items-center gap-2">
              <Button variant="secondary" size="md" onClick={addEntry}>
                <Plus className="size-4" /> Add key
              </Button>
              <Button
                variant="primary"
                size="md"
                disabled={!dirty || hasErrors || save.isPending}
                onClick={() => {
                  setMessage(null)
                  save.mutate()
                }}
              >
                {save.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Save className="size-4" />
                )}
                Save schema
              </Button>
            </div>
          )}
        </div>
      </div>

      <PageIntro
        id="schema"
        summary={
          <>
            The schema is a contract for your keys: what kind of value each one holds, where it must
            exist, and what it must look like. Saves that break it are rejected, and CI can fail
            before a deploy instead of after.
          </>
        }
        steps={[
          {
            title: 'Describe each key',
            body: 'Pick its type and tick the environment types where it is required, for example required in production.',
          },
          {
            title: 'Add rules if useful',
            body: 'A pattern or a fixed list of allowed values catches mistakes like a test API key in production.',
          },
          {
            title: 'Check it in CI',
            body: (
              <>
                <Cmd>clavis check</Cmd> fails the build when a required key is missing or invalid.
              </>
            ),
          },
        ]}
        when="a missing or malformed value has broken a deploy before."
        footer={
          <p className="text-xs text-muted">
            Start from what you have: <Cmd>clavis schema infer</Cmd> guesses types from existing
            keys without copying any values.
          </p>
        }
      />

      {!canManage && (
        <div className="rounded-app border border-border bg-surface px-4 py-3 text-sm text-muted">
          You can read the schema. An Owner or Admin can edit it.
        </div>
      )}
      {message && (
        <div className="flex items-center gap-2 rounded-app border border-accent-line bg-accent-soft px-4 py-2.5 text-sm text-accent-text">
          <CheckCircle2 className="size-4" /> {message}
        </div>
      )}
      {(save.error || schemaQuery.error || projectQuery.error) && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-sm text-danger">
          {save.error instanceof Error
            ? save.error.message
            : schemaQuery.error instanceof Error
              ? schemaQuery.error.message
              : projectQuery.error instanceof Error
                ? projectQuery.error.message
                : null}
        </div>
      )}

      <div
        className="overflow-x-auto rounded-app border border-border bg-surface"
        role="region"
        aria-label="Schema entries"
        tabIndex={0}
      >
        {busy ? (
          <div className="flex items-center gap-2 px-4 py-10 text-sm text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading schema…
          </div>
        ) : (
          <table className="w-full min-w-[1080px] border-collapse text-left">
            <thead>
              <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
                <th className="px-3 py-2.5 font-semibold">Key</th>
                <th className="px-3 py-2.5 font-semibold">Type</th>
                <th className="px-3 py-2.5 font-semibold">Kind</th>
                <th className="px-3 py-2.5 font-semibold">Required in</th>
                <th className="px-3 py-2.5 font-semibold">Pattern</th>
                <th className="px-3 py-2.5 font-semibold">Description</th>
                <th className="px-3 py-2.5 font-semibold">Example / enum</th>
                <th className="w-10 px-3 py-2.5" />
              </tr>
            </thead>
            <tbody>
              {drafts.map((draft, index) => (
                <tr key={index} className="border-b border-border-soft last:border-0">
                  <td className="px-3 py-2 align-top">
                    <Input
                      aria-label={`Key ${String(index + 1)}`}
                      className={cn(
                        'h-9 w-44 rounded border border-border bg-bg px-2 font-mono text-xs',
                        errors[index]?.startsWith('Key') && 'border-danger',
                      )}
                      placeholder="DATABASE_URL"
                      value={draft.key}
                      disabled={!canManage}
                      onChange={(event) => {
                        update(index, { key: event.target.value })
                      }}
                    />
                  </td>
                  <td className="px-3 py-2 align-top">
                    <Select
                      aria-label={`Type ${String(index + 1)}`}
                      className="w-24 rounded px-1.5 text-xs"
                      value={draft.type}
                      disabled={!canManage}
                      onChange={(event) => {
                        const type = event.target.value as SchemaDraft['type']
                        update(index, {
                          type,
                          enum: type === 'enum' ? (draft.enum.length > 0 ? draft.enum : ['']) : [],
                        })
                      }}
                    >
                      {schemaTypes.map((type) => (
                        <option key={type} value={type}>
                          {type}
                        </option>
                      ))}
                    </Select>
                  </td>
                  <td className="px-3 py-2 align-top">
                    <Select
                      aria-label={`Kind ${String(index + 1)}`}
                      className="w-24 rounded px-1.5 text-xs"
                      value={draft.kind}
                      disabled={!canManage}
                      onChange={(event) => {
                        update(index, { kind: event.target.value as SchemaDraft['kind'] })
                      }}
                    >
                      {schemaKinds.map((kind) => (
                        <option key={kind} value={kind}>
                          {kind}
                        </option>
                      ))}
                    </Select>
                  </td>
                  <td className="px-3 py-2 align-top">
                    <div className="flex flex-wrap gap-2 pt-1.5">
                      {envs.map((env) => (
                        <label key={env.id} className="flex items-center gap-1 text-2xs text-muted">
                          <input
                            type="checkbox"
                            aria-label={`Required in ${env.type} for key ${String(index + 1)}`}
                            checked={Boolean(draft.required[env.type])}
                            disabled={!canManage}
                            onChange={(event) => {
                              update(index, {
                                required: { ...draft.required, [env.type]: event.target.checked },
                              })
                            }}
                          />
                          {env.type}
                        </label>
                      ))}
                    </div>
                  </td>
                  <td className="px-3 py-2 align-top">
                    <Input
                      aria-label={`Pattern ${String(index + 1)}`}
                      className={cn(
                        'h-9 w-40 rounded border border-border bg-bg px-2 font-mono text-xs',
                        errors[index]?.startsWith('Pattern') && 'border-danger',
                      )}
                      placeholder="^postgres://"
                      value={draft.pattern}
                      disabled={!canManage}
                      onChange={(event) => {
                        update(index, { pattern: event.target.value })
                      }}
                    />
                  </td>
                  <td className="px-3 py-2 align-top">
                    <Input
                      aria-label={`Description ${String(index + 1)}`}
                      className="h-9 w-52 rounded text-xs"
                      placeholder="What this key is for"
                      value={draft.description}
                      disabled={!canManage}
                      onChange={(event) => {
                        update(index, { description: event.target.value })
                      }}
                    />
                  </td>
                  <td className="px-3 py-2 align-top">
                    {draft.type === 'enum' ? (
                      <Input
                        aria-label={`Enum values ${String(index + 1)}`}
                        className="h-9 w-52 rounded font-mono text-xs"
                        placeholder="debug, info, warn, error"
                        value={draft.enum.join(', ')}
                        disabled={!canManage}
                        onChange={(event) => {
                          update(index, {
                            enum: event.target.value
                              .split(',')
                              .map((value) => value.trim())
                              .filter((value) => value.length > 0),
                          })
                        }}
                      />
                    ) : (
                      <Input
                        aria-label={`Example ${String(index + 1)}`}
                        className="h-9 w-52 rounded text-xs"
                        placeholder="example (never a real secret)"
                        value={draft.example}
                        disabled={!canManage}
                        onChange={(event) => {
                          update(index, { example: event.target.value })
                        }}
                      />
                    )}
                    {errors[index] && (
                      <p className="mt-1 max-w-52 text-2xs text-danger">{errors[index]}</p>
                    )}
                  </td>
                  <td className="px-3 py-2 align-top">
                    {canManage && (
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label="Remove schema entry"
                        onClick={() => {
                          remove(index)
                        }}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
              {drafts.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-4 py-10 text-center text-sm text-muted">
                    No schema yet. Add a key, or run{' '}
                    <span className="font-mono">clavis schema infer</span> to draft one from the
                    current environment.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        )}
      </div>

      <p className="text-xs text-dim">
        Types: string, url, int, bool, json, enum. Required checks use the environment type, so the
        same key can be optional in development and required in production. Missing optional keys
        produce warnings, not failures.
      </p>
    </div>
  )
}
