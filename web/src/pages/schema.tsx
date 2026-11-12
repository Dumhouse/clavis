import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft, CheckCircle2, Loader2, Plus, Save, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import { api } from '@/api/client'
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
          className="inline-flex items-center gap-1.5 text-[13px] text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> Secrets
        </Link>
        <div className="mt-2 flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-head text-[25px] font-semibold tracking-tight">Schema</h1>
            <p className="mt-1 text-[13px] text-muted">
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

      {!canManage && (
        <div className="rounded-app border border-border bg-surface px-4 py-3 text-[13px] text-muted">
          You can read the schema. An Owner or Admin can edit it.
        </div>
      )}
      {message && (
        <div className="flex items-center gap-2 rounded-app border border-accent-line bg-accent-soft px-4 py-2.5 text-[13px] text-accent-text">
          <CheckCircle2 className="size-4" /> {message}
        </div>
      )}
      {(save.error || schemaQuery.error || projectQuery.error) && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-[13px] text-danger">
          {save.error instanceof Error
            ? save.error.message
            : schemaQuery.error instanceof Error
              ? schemaQuery.error.message
              : projectQuery.error instanceof Error
                ? projectQuery.error.message
                : null}
        </div>
      )}

      <div className="overflow-x-auto rounded-app border border-border bg-surface">
        {busy ? (
          <div className="flex items-center gap-2 px-4 py-10 text-[13px] text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading schema…
          </div>
        ) : (
          <table className="w-full min-w-[1080px] border-collapse text-left">
            <thead>
              <tr className="border-b border-border text-[11.5px] uppercase tracking-[0.06em] text-dim">
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
                    <input
                      className={cn(
                        'h-8 w-44 rounded border border-border bg-bg px-2 font-mono text-[12.5px]',
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
                    <select
                      className="h-8 w-24 rounded border border-border bg-bg px-1.5 text-[12.5px]"
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
                    </select>
                  </td>
                  <td className="px-3 py-2 align-top">
                    <select
                      className="h-8 w-24 rounded border border-border bg-bg px-1.5 text-[12.5px]"
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
                    </select>
                  </td>
                  <td className="px-3 py-2 align-top">
                    <div className="flex flex-wrap gap-2 pt-1.5">
                      {envs.map((env) => (
                        <label
                          key={env.id}
                          className="flex items-center gap-1 text-[11.5px] text-muted"
                        >
                          <input
                            type="checkbox"
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
                    <input
                      className={cn(
                        'h-8 w-40 rounded border border-border bg-bg px-2 font-mono text-[12px]',
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
                    <input
                      className="h-8 w-52 rounded border border-border bg-bg px-2 text-[12.5px]"
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
                      <input
                        className="h-8 w-52 rounded border border-border bg-bg px-2 font-mono text-[12px]"
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
                      <input
                        className="h-8 w-52 rounded border border-border bg-bg px-2 text-[12.5px]"
                        placeholder="example (never a real secret)"
                        value={draft.example}
                        disabled={!canManage}
                        onChange={(event) => {
                          update(index, { example: event.target.value })
                        }}
                      />
                    )}
                    {errors[index] && (
                      <p className="mt-1 max-w-52 text-[11.5px] text-danger">{errors[index]}</p>
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
                  <td colSpan={8} className="px-4 py-10 text-center text-[13px] text-muted">
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

      <p className="text-[12px] text-dim">
        Types: string, url, int, bool, json, enum. Required checks use the environment type, so the
        same key can be optional in development and required in production. Missing optional keys
        produce warnings, not failures.
      </p>
    </div>
  )
}
