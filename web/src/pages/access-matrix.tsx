import { useQuery } from '@tanstack/react-query'
import { Check, Loader2, ShieldCheck, X } from 'lucide-react'
import { useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
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

export function AccessMatrixPage() {
  const { org } = useCurrentOrg()
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

  const matrix = useQuery({
    queryKey: ['access-matrix', org?.slug, project, environment],
    enabled: Boolean(org),
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

  return (
    <div className="space-y-5">
      <header>
        <div className="flex items-center gap-2">
          <ShieldCheck className="size-5 text-muted" />
          <h1 className="font-head text-[24px] font-semibold tracking-tight">Access matrix</h1>
        </div>
        <p className="mt-1 text-[13px] text-muted">
          Effective permissions for every member and group. Click a cell to see why.
        </p>
      </header>

      <section className="flex flex-wrap items-end gap-3 rounded-app border border-border bg-surface px-4 py-3">
        <label className="block">
          <span className="mb-1 block text-[11.5px] font-medium text-muted">Project</span>
          <select
            className="h-8 rounded-app-sm border border-border bg-bg px-2 text-[12px] text-text focus:border-accent-line focus:outline-none"
            value={project}
            onChange={(event) => {
              setProject(event.target.value)
            }}
          >
            <option value="">All projects (org-wide)</option>
            {(projects.data ?? []).map((item) => (
              <option key={item.id} value={item.slug}>
                {item.name}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="mb-1 block text-[11.5px] font-medium text-muted">Environment glob</span>
          <input
            className="h-8 rounded-app-sm border border-border bg-bg px-2 font-mono text-[12px] text-text focus:border-accent-line focus:outline-none"
            value={environment}
            onChange={(event) => {
              setEnvironment(event.target.value)
            }}
            placeholder="*"
          />
        </label>
        {explain && (
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto"
            onClick={() => {
              setExplain(null)
            }}
          >
            <X className="size-3.5" /> Clear explanation
          </Button>
        )}
      </section>

      {explain && (
        <section className="rounded-app border border-accent-line bg-accent-soft p-4">
          <div className="flex flex-wrap items-center gap-2">
            <p className="text-[12.5px] font-semibold text-accent-text">
              {explain.name} · {permissionShort[explain.permission] ?? explain.permission}
            </p>
            {explanation.isFetching && <Loader2 className="size-3.5 animate-spin text-muted" />}
          </div>
          {explanation.data && (
            <>
              <p className="mt-1 text-[12px]">
                <span
                  className={
                    explanation.data.allowed
                      ? 'font-semibold text-accent-text'
                      : 'font-semibold text-danger'
                  }
                >
                  {explanation.data.allowed ? 'Allowed' : 'Denied'}
                </span>
                <span className="text-muted"> — {explanation.data.reason}</span>
              </p>
              {explanation.data.matched && (
                <p className="mt-1 text-[11.5px] text-muted">
                  Matched role <strong>{explanation.data.matched.role}</strong> (
                  {explanation.data.matched.source}) · {explanation.data.matched.environment || '*'}{' '}
                  · {explanation.data.matched.path || '/'}
                </p>
              )}
              <div className="mt-2 overflow-x-auto">
                <table className="w-full min-w-[560px] border-collapse text-left">
                  <thead>
                    <tr className="border-b border-border text-[10px] uppercase tracking-[0.06em] text-dim">
                      <th className="py-1.5 pr-3 font-semibold">Role</th>
                      <th className="py-1.5 pr-3 font-semibold">Source</th>
                      <th className="py-1.5 pr-3 font-semibold">Environment</th>
                      <th className="py-1.5 pr-3 font-semibold">Path</th>
                      <th className="py-1.5 font-semibold">Grants</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(explanation.data.bindings ?? []).map((binding: Binding, index) => (
                      <tr key={`${binding.role}-${String(index)}`} className="text-[11.5px]">
                        <td className="py-1 pr-3">{binding.role}</td>
                        <td className="py-1 pr-3 text-muted">{binding.source}</td>
                        <td className="py-1 pr-3 font-mono">{binding.environment || '*'}</td>
                        <td className="py-1 pr-3 font-mono">{binding.path || '/'}</td>
                        <td className="py-1">
                          {binding.grants ? (
                            <Check className="size-3.5 text-accent-text" />
                          ) : (
                            <X className="size-3.5 text-dim" />
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
          {explanation.isError && (
            <p className="mt-1 text-[11.5px] text-danger">
              {explanation.error instanceof Error
                ? explanation.error.message
                : 'Could not explain.'}
            </p>
          )}
        </section>
      )}

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        {matrix.isLoading && (
          <p className="flex items-center gap-2 px-4 py-6 text-[12px] text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading access…
          </p>
        )}
        {matrix.isError && (
          <p className="px-4 py-4 text-[11.5px] text-danger">
            {matrix.error instanceof Error ? matrix.error.message : 'Could not load the matrix.'}
          </p>
        )}
        {!matrix.isLoading && !matrix.isError && (
          <table className="w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-border text-[10px] uppercase tracking-[0.02em] text-dim">
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
                        <span className="rounded-full bg-elev px-1.5 py-0.5 text-[9.5px] font-semibold uppercase text-muted">
                          {row.subject_kind}
                        </span>
                        <span className="whitespace-nowrap text-[11.5px] font-medium">
                          {row.name || row.email || row.subject_id}
                        </span>
                        {row.role && (
                          <span className="whitespace-nowrap text-[10.5px] text-dim">
                            {row.role}
                          </span>
                        )}
                        {row.subject_kind === 'group' && (
                          <span className="whitespace-nowrap text-[10px] text-dim">
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
          <p className="px-4 py-5 text-[12px] text-muted">No members or groups to show.</p>
        )}
      </section>
    </div>
  )
}
