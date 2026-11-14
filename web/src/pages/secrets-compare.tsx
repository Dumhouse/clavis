import { useQueries, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Copy, Loader2 } from 'lucide-react'
import { useMemo, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Menu, type MenuItem } from '@/components/ui/menu'
import { toast } from '@/components/ui/toast'
import { apiErrorMessage } from '@/lib/api-error'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']
type Secret = components['schemas']['SecretBody']

/**
 * Every environment side by side, with the keys whose value differs or is
 * missing somewhere. Each value can be copied to another environment.
 * Loading this view reveals values, which is audited like any reveal.
 */
export function CompareEnvironments({
  orgSlug,
  projectSlug,
  envs,
  writableEnvs,
  filter,
}: {
  orgSlug: string
  projectSlug: string
  envs: Env[]
  writableEnvs: Env[]
  filter: string
}) {
  const queryClient = useQueryClient()
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const [showSame, setShowSame] = useState(false)
  const [copying, setCopying] = useState<string | null>(null)
  const shown = envs.filter((env) => !hidden.has(env.slug))

  const queries = useQueries({
    queries: shown.map((env) => ({
      queryKey: ['secrets-compare', orgSlug, projectSlug, env.slug],
      queryFn: async (): Promise<Secret[]> => {
        const result = await api.GET(
          '/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets',
          {
            params: {
              path: { org: orgSlug, project: projectSlug, env: env.slug },
              query: { reveal: true, channel: 'ui' },
            },
          },
        )
        if (result.error)
          throw new Error(apiErrorMessage(result.error, `Could not load ${env.slug}.`))
        return result.data?.secrets ?? []
      },
    })),
  })
  const loading = queries.some((query) => query.isLoading)
  const failed = queries.find((query) => query.error)?.error

  const byEnv = useMemo(() => {
    const map = new Map<string, Map<string, Secret>>()
    shown.forEach((env, index) => {
      map.set(env.slug, new Map((queries[index]?.data ?? []).map((secret) => [secret.key, secret])))
    })
    return map
  }, [shown, queries])

  const rows = useMemo(() => {
    const keys = new Set<string>()
    for (const secrets of byEnv.values()) for (const key of secrets.keys()) keys.add(key)
    const needle = filter.trim().toLowerCase()
    return [...keys]
      .filter((key) => !needle || key.toLowerCase().includes(needle))
      .sort((a, b) => a.localeCompare(b))
      .map((key) => {
        const cells = shown.map((env) => byEnv.get(env.slug)?.get(key))
        const values = new Set(cells.map((cell) => (cell ? (cell.value ?? '') : null)))
        return { key, cells, same: values.size === 1 && !values.has(null) }
      })
  }, [byEnv, shown, filter])
  const visible = showSame ? rows : rows.filter((row) => !row.same)
  const differing = rows.filter((row) => !row.same).length

  async function copy(key: string, from: Env, to: Env) {
    const source = byEnv.get(from.slug)?.get(key)
    if (!source) return
    setCopying(`${key}:${to.slug}`)
    const exists = byEnv.get(to.slug)?.has(key) ?? false
    const body = {
      value: source.value ?? '',
      kind: (source.kind === 'variable' || source.kind === 'file' ? source.kind : 'secret') as
        'secret' | 'variable' | 'file',
      folder: source.inherited ? '' : (source.folder ?? ''),
      message: `Copied from ${from.slug}`,
    }
    const result = exists
      ? await api.PUT('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}', {
          params: { path: { org: orgSlug, project: projectSlug, env: to.slug, key } },
          body,
        })
      : await api.POST('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets', {
          params: { path: { org: orgSlug, project: projectSlug, env: to.slug } },
          body: { key, ...body },
        })
    setCopying(null)
    if (result.error) {
      toast(`${key}: ${apiErrorMessage(result.error, `could not copy to ${to.slug}`)}`, {
        tone: 'error',
      })
      return
    }
    toast(`Copied ${key} from ${from.slug} to ${to.slug}.`)
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['secrets-compare', orgSlug, projectSlug] }),
      queryClient.invalidateQueries({ queryKey: ['secrets', orgSlug, projectSlug] }),
    ])
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
        <span>Environments:</span>
        {envs.map((env) => {
          const on = !hidden.has(env.slug)
          return (
            <button
              key={env.id}
              type="button"
              aria-pressed={on}
              disabled={on && shown.length <= 2}
              title={on && shown.length <= 2 ? 'Compare at least two environments' : undefined}
              onClick={() => {
                setHidden((current) => {
                  const next = new Set(current)
                  if (on) next.add(env.slug)
                  else next.delete(env.slug)
                  return next
                })
              }}
              className={cn(
                'rounded-full border px-2.5 py-0.5',
                on
                  ? 'border-accent-line bg-accent-soft text-text'
                  : 'border-border text-dim hover:text-text',
              )}
            >
              {env.name}
            </button>
          )
        })}
        <label className="ml-auto flex items-center gap-1.5">
          <input
            type="checkbox"
            checked={showSame}
            onChange={(event) => {
              setShowSame(event.target.checked)
            }}
          />
          Show identical keys
        </label>
        <span className="text-dim">
          {differing} of {rows.length} keys differ
        </span>
      </div>

      {failed instanceof Error && (
        <p className="rounded-app-sm bg-danger-bg px-3 py-2 text-xs text-danger">
          {failed.message}
        </p>
      )}

      <div
        className="overflow-x-auto rounded-app border border-border bg-surface"
        role="region"
        aria-label="Environment comparison"
        tabIndex={0}
      >
        <table className="w-full min-w-[640px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
              <th scope="col" className="w-56 px-4 py-2.5 font-semibold">
                Key
              </th>
              {shown.map((env) => (
                <th key={env.id} scope="col" className="px-4 py-2.5 font-semibold">
                  {env.name}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {visible.map((row) => {
              const first = row.cells.find(Boolean)?.value ?? null
              return (
                <tr key={row.key} className="border-b border-border-soft last:border-0">
                  <td className="px-4 py-2 font-mono text-xs text-text">{row.key}</td>
                  {row.cells.map((cell, index) => {
                    const env = shown[index]
                    if (!env) return null
                    const targets = writableEnvs.filter(
                      (target) =>
                        target.slug !== env.slug &&
                        !hidden.has(target.slug) &&
                        byEnv.get(target.slug)?.get(row.key)?.value !== cell?.value,
                    )
                    const items: MenuItem[] = targets.map((target) => ({
                      label: `Copy to ${target.name}`,
                      icon: <ArrowRight className="size-3.5" />,
                      description: byEnv.get(target.slug)?.has(row.key)
                        ? 'Replaces its value (new version)'
                        : 'Adds the key there',
                      onSelect: () => {
                        void copy(row.key, env, target)
                      },
                    }))
                    const busy = copying?.startsWith(`${row.key}:`)
                    return (
                      <td key={env.id} className="group px-4 py-1.5 font-mono text-xs">
                        <div className="flex items-center gap-1.5">
                          {cell ? (
                            <span
                              className={cn(
                                'min-w-0 max-w-72 truncate',
                                !row.same && (cell.value ?? '') !== first
                                  ? 'text-amber-fg'
                                  : 'text-muted',
                              )}
                              title={cell.value ?? ''}
                            >
                              {cell.value === '' ? '(empty)' : cell.value}
                              {cell.inherited && (
                                <span className="ml-1.5 font-sans text-2xs text-dim">
                                  from {cell.source_environment}
                                </span>
                              )}
                            </span>
                          ) : (
                            <span className="text-danger">missing</span>
                          )}
                          {cell && items.length > 0 && (
                            <Menu
                              label={`Copy ${row.key} from ${env.name}`}
                              menuClassName="min-w-56"
                              triggerClassName="ml-auto rounded p-1 text-dim opacity-0 hover:bg-hover hover:text-text focus-visible:opacity-100 group-hover:opacity-100"
                              trigger={
                                busy ? (
                                  <Loader2 className="size-3.5 animate-spin" />
                                ) : (
                                  <Copy className="size-3.5" />
                                )
                              }
                              items={items}
                            />
                          )}
                        </div>
                      </td>
                    )
                  })}
                </tr>
              )
            })}
            {!loading && visible.length === 0 && (
              <tr>
                <td
                  colSpan={shown.length + 1}
                  className="px-4 py-10 text-center text-sm text-muted"
                >
                  {rows.length === 0
                    ? 'No keys yet.'
                    : 'Every key has the same value in these environments.'}
                </td>
              </tr>
            )}
            {loading && (
              <tr>
                <td colSpan={shown.length + 1} className="px-4 py-6 text-center text-sm text-muted">
                  <Loader2 className="mr-2 inline size-3.5 animate-spin" /> Comparing…
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-dim">
        Hover a value and use the copy button to copy it to another environment. To move many keys
        at once (with review and a snapshot), use Promote.
      </p>
    </div>
  )
}
