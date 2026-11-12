import { useQueries, useQuery } from '@tanstack/react-query'
import { Check, Loader2, ShieldCheck, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Field, Notice, Select } from '@/components/ui/field'
import { Modal } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type Row = components['schemas']['MatrixRowBody']
type Binding = components['schemas']['BindingViewBody']

const permissionShort: Record<string, string> = {
  'secrets:read-names': 'keys',
  'secrets:read-values': 'reveal',
  'secrets:use': 'use',
  'secrets:write': 'write',
  'secrets:delete': 'delete',
  'envs:read': 'envs',
  'envs:manage': 'envs+',
  'org:read': 'org',
  'projects:read': 'projects',
  'projects:manage': 'projects+',
  'members:manage': 'members',
  'roles:manage': 'roles',
  'syncs:manage': 'syncs',
  'audit:read': 'audit',
  'settings:manage': 'settings',
  'tokens:manage': 'tokens',
  'org:delete': 'delete org',
}

type Env = components['schemas']['EnvBody']

/** Access levels, from least to most, derived from effective permissions. */
const levels = [
  {
    id: 'none',
    label: 'No access',
    short: '—',
    permission: 'secrets:read-names',
    tone: 'text-dim',
  },
  {
    id: 'names',
    label: 'Sees key names',
    short: 'Names',
    permission: 'secrets:read-names',
    tone: 'bg-elev text-muted',
  },
  {
    id: 'use',
    label: 'Injects at runtime (clavis run) but cannot reveal',
    short: 'Use',
    permission: 'secrets:use',
    tone: 'bg-blue-bg text-blue-fg',
  },
  {
    id: 'read',
    label: 'Reveals values',
    short: 'Read',
    permission: 'secrets:read-values',
    tone: 'bg-amber-bg text-amber-fg',
  },
  {
    id: 'write',
    label: 'Changes values',
    short: 'Write',
    permission: 'secrets:write',
    tone: 'bg-danger-bg text-danger',
  },
  {
    id: 'admin',
    label: 'Manages environments and access',
    short: 'Admin',
    permission: 'envs:manage',
    tone: 'bg-violet-bg text-violet-fg',
  },
] as const

type Level = (typeof levels)[number]

function levelOf(permissions: string[] | null | undefined): Level {
  const set = new Set(permissions ?? [])
  const pick = (id: Level['id']) => levels.find((level) => level.id === id) ?? levels[0]
  if (set.has('envs:manage') && set.has('secrets:write')) return pick('admin')
  if (set.has('secrets:write')) return pick('write')
  if (set.has('secrets:read-values')) return pick('read')
  if (set.has('secrets:use')) return pick('use')
  if (set.has('secrets:read-names')) return pick('names')
  return pick('none')
}

const envOrder: Record<string, number> = { development: 0, preview: 1, staging: 2, production: 3 }

export function AccessMatrixPage() {
  const { org } = useCurrentOrg()
  const [view, setView] = useState<'levels' | 'permissions'>('levels')
  const [project, setProject] = useState('')
  const [environment, setEnvironment] = useState('*')
  const [explain, setExplain] = useState<{
    kind: string
    id: string
    name: string
    permission: string
  } | null>(null)

  const projects = useQuery({
    queryKey: ['projects', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load projects.'))
      return result.data?.projects ?? []
    },
  })

  useEffect(() => {
    if (!project && projects.data?.[0]) setProject(projects.data[0].slug)
  }, [project, projects.data])

  const projectDetail = useQuery({
    queryKey: ['project', org?.slug, project],
    enabled: Boolean(org && project),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}', {
        params: { path: { org: org.slug, project } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load the project.'))
      return result.data
    },
  })
  const envs: Env[] = useMemo(
    () =>
      [...(projectDetail.data?.environments ?? [])].sort(
        (a, b) => (envOrder[a.type] ?? 9) - (envOrder[b.type] ?? 9),
      ),
    [projectDetail.data],
  )

  const perEnv = useQueries({
    queries: envs.map((env) => ({
      queryKey: ['access-matrix', org?.slug, project, env.slug],
      enabled: Boolean(org && project && view === 'levels'),
      queryFn: async () => {
        if (!org) throw new Error('No organization.')
        const result = await api.GET('/api/v1/orgs/{org}/access/matrix', {
          params: { path: { org: org.slug }, query: { project, environment: env.slug } },
        })
        if (result.error)
          throw new Error(apiErrorMessage(result.error, 'Could not load the access matrix.'))
        return result.data
      },
    })),
  })

  const levelRows = useMemo(() => {
    const subjects = new Map<string, { row: Row; levels: Map<string, Level> }>()
    envs.forEach((env, index) => {
      for (const row of perEnv[index]?.data?.rows ?? []) {
        const id = `${row.subject_kind ?? 'user'}:${row.subject_id ?? ''}`
        const entry = subjects.get(id) ?? { row, levels: new Map<string, Level>() }
        entry.levels.set(env.slug, levelOf(row.permissions))
        subjects.set(id, entry)
      }
    })
    return [...subjects.values()]
  }, [envs, perEnv])

  const prodReaders = useMemo(() => {
    const prod = envs.filter((env) => env.type === 'production')
    return levelRows.filter((entry) =>
      prod.some((env) => {
        const level = entry.levels.get(env.slug)
        return level && ['read', 'write', 'admin'].includes(level.id)
      }),
    )
  }, [envs, levelRows])

  const matrix = useQuery({
    queryKey: ['access-matrix', org?.slug, project, environment],
    enabled: Boolean(org) && view === 'permissions',
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const query: { project?: string; environment?: string } = {}
      if (project) query.project = project
      if (environment) query.environment = environment
      const result = await api.GET('/api/v1/orgs/{org}/access/matrix', {
        params: { path: { org: org.slug }, query },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load the access matrix.'))
      return result.data
    },
  })

  const explanation = useQuery({
    queryKey: ['access-explain', org?.slug, explain, project, environment],
    enabled: Boolean(org && explain),
    queryFn: async () => {
      if (!org || !explain) throw new Error('No selection.')
      const query: {
        subject_kind?: 'user' | 'group'
        subject_id?: string
        permission?: string
        project?: string
        environment?: string
      } = {
        subject_kind: explain.kind as 'user' | 'group',
        subject_id: explain.id,
        permission: explain.permission,
      }
      if (project) query.project = project
      if (environment) query.environment = environment
      const result = await api.GET('/api/v1/orgs/{org}/access/explain', {
        params: { path: { org: org.slug }, query },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not explain this access.'))
      return result.data
    },
  })

  const permissions = matrix.data?.permissions ?? []
  const rows: Row[] = matrix.data?.rows ?? []

  const subjectName = (row: Row) => row.name || row.email || row.subject_id || ''
  const levelsLoading = perEnv.some((query) => query.isLoading)

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <ShieldCheck className="size-5 text-muted" />
            <h1 className="font-head text-2xl font-semibold tracking-tight">Access</h1>
          </div>
          <p className="mt-1 text-base text-muted">
            Who can do what, per environment. Click any cell to see which role grants it.
          </p>
        </div>
        <div
          role="tablist"
          aria-label="View"
          className="flex rounded-app-sm border border-border p-0.5"
        >
          {(
            [
              ['levels', 'By environment'],
              ['permissions', 'All permissions'],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              type="button"
              role="tab"
              aria-selected={view === value}
              onClick={() => {
                setView(value)
              }}
              className={
                view === value
                  ? 'h-7 rounded-[6px] bg-elev px-2.5 text-xs font-medium text-text'
                  : 'h-7 rounded-[6px] px-2.5 text-xs text-muted hover:text-text'
              }
            >
              {label}
            </button>
          ))}
        </div>
      </header>

      <section className="flex flex-wrap items-end gap-3">
        <Field label="Project">
          <Select
            className="min-w-48"
            value={project}
            onChange={(event) => {
              setProject(event.target.value)
              setEnvironment('*')
            }}
          >
            {view === 'permissions' && <option value="">All projects (org-wide)</option>}
            {(projects.data ?? []).map((item) => (
              <option key={item.id} value={item.slug}>
                {item.name}
              </option>
            ))}
          </Select>
        </Field>
        {view === 'permissions' && (
          <Field label="Environment">
            <Select
              className="min-w-40"
              value={environment}
              onChange={(event) => {
                setEnvironment(event.target.value)
              }}
            >
              <option value="*">Any environment</option>
              {envs.map((env) => (
                <option key={env.id} value={env.slug}>
                  {env.name}
                </option>
              ))}
            </Select>
          </Field>
        )}
      </section>

      {view === 'levels' && (
        <>
          {envs.some((env) => env.type === 'production') && !levelsLoading && (
            <section
              aria-label="Production readers"
              className="rounded-app border border-border bg-surface px-4 py-3"
            >
              <p className="text-sm">
                <span className="font-semibold text-text">
                  {prodReaders.length}{' '}
                  {prodReaders.length === 1 ? 'person or group' : 'people and groups'}
                </span>{' '}
                <span className="text-muted">can reveal or change production values.</span>
              </p>
              {prodReaders.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {prodReaders.map(({ row }) => (
                    <span
                      key={`${row.subject_kind ?? ''}-${row.subject_id ?? ''}`}
                      className="rounded-full bg-amber-bg px-2 py-0.5 text-2xs font-medium text-amber-fg"
                    >
                      {subjectName(row)}
                      {row.subject_kind === 'group' ? ` (${String(row.member_count ?? 0)})` : ''}
                    </span>
                  ))}
                </div>
              )}
            </section>
          )}

          <section className="overflow-x-auto rounded-app border border-border bg-surface">
            {levelsLoading ? (
              <p className="flex items-center gap-2 px-4 py-6 text-sm text-muted">
                <Loader2 className="size-4 animate-spin" /> Loading access…
              </p>
            ) : (
              <table className="w-full min-w-[520px] border-collapse text-left">
                <caption className="sr-only">Access level per environment</caption>
                <thead>
                  <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
                    <th scope="col" className="px-4 py-2.5 font-semibold">
                      Who
                    </th>
                    {envs.map((env) => (
                      <th key={env.id} scope="col" className="px-3 py-2.5 font-semibold">
                        <span className={`env-chip env-${env.type}`}>{env.slug}</span>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {levelRows.map(({ row, levels: byEnv }) => (
                    <tr
                      key={`${row.subject_kind ?? ''}-${row.subject_id ?? ''}`}
                      className="border-b border-border-soft last:border-0"
                    >
                      <th scope="row" className="px-4 py-2 text-left font-normal">
                        <span className="text-sm font-medium text-text">{subjectName(row)}</span>
                        <span className="ml-2 text-2xs text-dim">
                          {row.subject_kind === 'group'
                            ? `group · ${String(row.member_count ?? 0)} members`
                            : row.role}
                        </span>
                      </th>
                      {envs.map((env) => {
                        const level = byEnv.get(env.slug) ?? levels[0]
                        return (
                          <td key={env.id} className="px-3 py-2">
                            <button
                              type="button"
                              title={level.label}
                              aria-label={`${subjectName(row)} in ${env.slug}: ${level.label}. Explain`}
                              onClick={() => {
                                setEnvironment(env.slug)
                                setExplain({
                                  kind: row.subject_kind ?? 'user',
                                  id: row.subject_id ?? '',
                                  name: subjectName(row),
                                  permission:
                                    level.id === 'none' ? 'secrets:read-names' : level.permission,
                                })
                              }}
                              className={`inline-flex h-6 min-w-14 items-center justify-center rounded px-2 text-2xs font-semibold hover:ring-1 hover:ring-accent-line ${level.tone}`}
                            >
                              {level.short}
                            </button>
                          </td>
                        )
                      })}
                    </tr>
                  ))}
                  {levelRows.length === 0 && (
                    <tr>
                      <td
                        colSpan={envs.length + 1}
                        className="px-4 py-8 text-center text-sm text-muted"
                      >
                        {project ? 'Nobody has access to this project yet.' : 'Pick a project.'}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            )}
          </section>
          <dl className="flex flex-wrap gap-x-4 gap-y-1 text-2xs text-dim">
            {levels.slice(1).map((level) => (
              <div key={level.id} className="flex items-center gap-1.5">
                <dt className={`rounded px-1.5 font-semibold ${level.tone}`}>{level.short}</dt>
                <dd>{level.label}</dd>
              </div>
            ))}
          </dl>
        </>
      )}

      {explain && (
        <Modal
          variant="sheet"
          className="max-w-lg"
          onClose={() => {
            setExplain(null)
          }}
        >
          <div className="flex items-start justify-between gap-3 border-b border-border px-5 py-4">
            <div>
              <h2 className="font-head text-md font-semibold">
                Why does {explain.name} have this?
              </h2>
              <p className="mt-0.5 font-mono text-2xs text-dim">
                {explain.permission} · {project || 'org'} / {environment || '*'}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Close"
              onClick={() => {
                setExplain(null)
              }}
            >
              <X className="size-4" />
            </Button>
          </div>
          <div className="flex-1 space-y-3 overflow-y-auto px-5 py-4">
            {explanation.isFetching && (
              <p className="flex items-center gap-2 text-sm text-muted">
                <Loader2 className="size-3.5 animate-spin" /> Checking…
              </p>
            )}
            {explanation.data && (
              <>
                <Notice tone={explanation.data.allowed ? 'success' : 'danger'}>
                  <strong>{explanation.data.allowed ? 'Allowed' : 'Denied'}</strong> —{' '}
                  {explanation.data.reason}
                </Notice>
                {explanation.data.matched && (
                  <p className="text-xs text-muted">
                    Granted by role{' '}
                    <strong className="text-text">{explanation.data.matched.role}</strong> (
                    {explanation.data.matched.source}) on{' '}
                    <span className="font-mono">{explanation.data.matched.environment || '*'}</span>
                    {explanation.data.matched.path ? (
                      <>
                        {' '}
                        at <span className="font-mono">{explanation.data.matched.path}</span>
                      </>
                    ) : null}
                    .
                  </p>
                )}
                <table className="w-full border-collapse text-left">
                  <caption className="mb-1 text-left text-xs font-medium text-muted">
                    All bindings considered
                  </caption>
                  <thead>
                    <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
                      <th scope="col" className="py-1.5 pr-3 font-semibold">
                        Role
                      </th>
                      <th scope="col" className="py-1.5 pr-3 font-semibold">
                        Source
                      </th>
                      <th scope="col" className="py-1.5 pr-3 font-semibold">
                        Env
                      </th>
                      <th scope="col" className="py-1.5 font-semibold">
                        Grants
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {(explanation.data.bindings ?? []).map((binding: Binding, index) => (
                      <tr key={`${binding.role}-${String(index)}`} className="text-xs">
                        <td className="py-1 pr-3">{binding.role}</td>
                        <td className="py-1 pr-3 text-muted">{binding.source}</td>
                        <td className="py-1 pr-3 font-mono">{binding.environment || '*'}</td>
                        <td className="py-1">
                          {binding.grants ? (
                            <Check className="size-3.5 text-accent-text" aria-label="yes" />
                          ) : (
                            <X className="size-3.5 text-dim" aria-label="no" />
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </>
            )}
            {explanation.isError && (
              <Notice>
                {explanation.error instanceof Error
                  ? explanation.error.message
                  : 'Could not explain.'}
              </Notice>
            )}
          </div>
        </Modal>
      )}

      {view === 'permissions' && (
        <section className="overflow-x-auto rounded-app border border-border bg-surface">
          {matrix.isLoading && (
            <p className="flex items-center gap-2 px-4 py-6 text-sm text-muted">
              <Loader2 className="size-4 animate-spin" /> Loading access…
            </p>
          )}
          {matrix.isError && (
            <p className="px-4 py-4 text-xs text-danger">
              {matrix.error instanceof Error ? matrix.error.message : 'Could not load the matrix.'}
            </p>
          )}
          {!matrix.isLoading && !matrix.isError && (
            <table className="w-full border-collapse text-left">
              <thead>
                <tr className="border-b border-border text-2xs uppercase tracking-[0.02em] text-dim">
                  <th className="sticky left-0 z-10 bg-surface px-3 py-2 font-semibold">Subject</th>
                  {permissions.map((permission) => (
                    <th
                      key={permission}
                      className="px-1.5 py-2 text-center font-semibold"
                      title={permission}
                    >
                      {permissionShort[permission] ?? permission}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => {
                  const allowed = new Set(row.permissions)
                  return (
                    <tr
                      key={`${row.subject_kind}-${row.subject_id}`}
                      className="border-b border-border-soft last:border-0"
                    >
                      <td className="sticky left-0 z-10 bg-surface px-3 py-2">
                        <div className="flex items-center gap-2">
                          <span className="rounded-full bg-elev px-1.5 py-0.5 text-3xs font-semibold uppercase text-muted">
                            {row.subject_kind}
                          </span>
                          <span className="whitespace-nowrap text-xs font-medium">
                            {row.name || row.email || row.subject_id}
                          </span>
                          {row.role && (
                            <span className="whitespace-nowrap text-2xs text-dim">{row.role}</span>
                          )}
                          {row.subject_kind === 'group' && (
                            <span className="whitespace-nowrap text-2xs text-dim">
                              {row.member_count ?? 0} members
                            </span>
                          )}
                        </div>
                      </td>
                      {permissions.map((permission) => {
                        const active = allowed.has(permission)
                        return (
                          <td key={permission} className="px-1 py-1.5 text-center">
                            <button
                              type="button"
                              className={
                                active
                                  ? 'inline-flex size-5 items-center justify-center rounded-[4px] bg-accent-soft text-accent-text hover:ring-1 hover:ring-accent-line'
                                  : 'inline-flex size-5 items-center justify-center rounded-[4px] text-transparent hover:bg-elev hover:text-dim'
                              }
                              aria-label={`${row.name || row.email || row.subject_id}: ${permission}`}
                              onClick={() => {
                                setExplain({
                                  kind: row.subject_kind ?? 'user',
                                  id: row.subject_id ?? '',
                                  name: row.name || row.email || row.subject_id || '',
                                  permission,
                                })
                              }}
                            >
                              {active ? <Check className="size-3" /> : '-'}
                            </button>
                          </td>
                        )
                      })}
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
          {!matrix.isLoading && !matrix.isError && rows.length === 0 && (
            <p className="px-4 py-5 text-sm text-muted">No members or groups to show.</p>
          )}
        </section>
      )}
    </div>
  )
}
