import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Loader2, ShieldCheck, Trash2, UserPlus } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Link, useParams } from '@tanstack/react-router'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type Grant = components['schemas']['GrantBody']
type Group = components['schemas']['GroupBody']
type Member = components['schemas']['MemberBody']

const ROLES = ['Admin', 'Developer', 'Viewer'] as const

export function ProjectAccessPage() {
  const { org } = useCurrentOrg()
  const params = useParams({ strict: false }) as { project?: string }
  const projectSlug = params.project ?? ''
  const queryClient = useQueryClient()
  const [subjectKind, setSubjectKind] = useState<'user' | 'group'>('user')
  const [subjectID, setSubjectID] = useState('')
  const [role, setRole] = useState('Developer')
  const [environment, setEnvironment] = useState('*')
  const [path, setPath] = useState('/**')
  const [days, setDays] = useState(0)
  const [error, setError] = useState<string | null>(null)

  const grants = useQuery({
    queryKey: ['access', org?.slug, projectSlug],
    enabled: Boolean(org && projectSlug),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}/access', {
        params: { path: { org: org.slug, project: projectSlug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load project access.'))
      return result.data?.grants ?? []
    },
  })

  const members = useQuery({
    queryKey: ['members', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/members', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load members.'))
      return result.data?.members ?? []
    },
  })

  const groups = useQuery({
    queryKey: ['groups', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/groups', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load groups.'))
      return result.data?.groups ?? []
    },
  })

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['access', org?.slug, projectSlug] })
  }

  const grant = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/access', {
        params: { path: { org: org.slug, project: projectSlug } },
        body: {
          subject_kind: subjectKind,
          subject_id: subjectID,
          role: role as (typeof ROLES)[number],
          environment: environment.trim() || '*',
          path: path.trim() || '/**',
          expires_in_days: days,
        },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not grant access.'))
    },
    onSuccess: async () => {
      setSubjectID('')
      setError(null)
      await invalidate()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not grant access.')
    },
  })

  const revoke = useMutation({
    mutationFn: async (item: Grant) => {
      if (!org) throw new Error('No organization.')
      const result = await api.DELETE('/api/v1/orgs/{org}/projects/{project}/access/{id}', {
        params: { path: { org: org.slug, project: projectSlug, id: item.id } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not revoke access.'))
    },
    onSuccess: invalidate,
  })

  function submit(event: FormEvent) {
    event.preventDefault()
    if (!subjectID) {
      setError('Choose a user or group first.')
      return
    }
    grant.mutate()
  }

  const candidates: { id: string; label: string; hint: string }[] =
    subjectKind === 'user'
      ? (members.data ?? []).map((member: Member) => ({
          id: member.user_id,
          label: member.name || member.email,
          hint: member.email,
        }))
      : (groups.data ?? []).map((group: Group) => ({
          id: group.id,
          label: group.name,
          hint: `${String(group.members?.length ?? 0)} members`,
        }))

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/projects/$project"
          params={{ project: projectSlug }}
          className="inline-flex items-center gap-1.5 text-[13px] text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> {projectSlug}
        </Link>
        <div className="mt-2 flex items-center gap-2">
          <ShieldCheck className="size-5 text-muted" />
          <h1 className="font-head text-[25px] font-semibold tracking-tight">Project access</h1>
        </div>
        <p className="mt-1 text-[14px] text-muted">
          Scoped roles for people and groups. Environment and path globs narrow each grant.
        </p>
      </div>

      <section className="rounded-app border border-border bg-surface">
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-[14px] font-semibold">Grant access</h2>
          <p className="text-[12px] text-dim">
            You can only grant permissions you already hold yourself.
          </p>
        </div>
        <form onSubmit={submit} className="grid gap-3 p-4 sm:grid-cols-2 lg:grid-cols-6">
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Subject</span>
            <select
              className="h-8 w-full rounded-app-sm border border-border bg-bg px-2 text-[13px] text-text focus:border-accent-line focus:outline-none"
              value={subjectKind}
              onChange={(event) => {
                setSubjectKind(event.target.value as 'user' | 'group')
                setSubjectID('')
              }}
            >
              <option value="user">User</option>
              <option value="group">Group</option>
            </select>
          </label>
          <label className="block lg:col-span-2">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">
              {subjectKind === 'user' ? 'Member' : 'Group'}
            </span>
            <select
              className="h-8 w-full rounded-app-sm border border-border bg-bg px-2 text-[13px] text-text focus:border-accent-line focus:outline-none"
              value={subjectID}
              onChange={(event) => {
                setSubjectID(event.target.value)
              }}
            >
              <option value="">Choose…</option>
              {candidates.map((candidate) => (
                <option key={candidate.id} value={candidate.id}>
                  {candidate.label} — {candidate.hint}
                </option>
              ))}
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Role</span>
            <select
              className="h-8 w-full rounded-app-sm border border-border bg-bg px-2 text-[13px] text-text focus:border-accent-line focus:outline-none"
              value={role}
              onChange={(event) => {
                setRole(event.target.value)
              }}
            >
              {ROLES.map((candidate) => (
                <option key={candidate} value={candidate}>
                  {candidate}
                </option>
              ))}
            </select>
          </label>
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Environment</span>
            <input
              className="h-8 w-full rounded-app-sm border border-border bg-bg px-2 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
              value={environment}
              onChange={(event) => {
                setEnvironment(event.target.value)
              }}
              placeholder="* or prod"
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Path glob</span>
            <input
              className="h-8 w-full rounded-app-sm border border-border bg-bg px-2 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
              value={path}
              onChange={(event) => {
                setPath(event.target.value)
              }}
              placeholder="/**"
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Expires</span>
            <select
              className="h-8 w-full rounded-app-sm border border-border bg-bg px-2 text-[13px] text-text focus:border-accent-line focus:outline-none"
              value={days}
              onChange={(event) => {
                setDays(Number(event.target.value))
              }}
            >
              <option value={0}>Never</option>
              <option value={1}>1 day</option>
              <option value={7}>7 days</option>
              <option value={30}>30 days</option>
              <option value={90}>90 days</option>
            </select>
          </label>
          <div className="flex items-end lg:col-span-2">
            <Button type="submit" variant="primary" size="sm" disabled={grant.isPending}>
              {grant.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <UserPlus className="size-3.5" />
              )}
              Grant access
            </Button>
          </div>
          {error && (
            <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger lg:col-span-6">
              {error}
            </p>
          )}
        </form>
      </section>

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <h2 className="text-[14px] font-semibold">Current grants</h2>
          <span className="rounded-full bg-elev px-1.5 text-[11.5px] text-muted">
            {grants.data?.length ?? 0}
          </span>
        </div>
        {grants.isError && (
          <p className="px-4 py-3 text-[12.5px] text-danger">
            {grants.error instanceof Error ? grants.error.message : 'Could not load grants.'}
          </p>
        )}
        <table className="w-full min-w-[720px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-[11.5px] uppercase tracking-[0.06em] text-dim">
              <th className="px-4 py-2.5 font-semibold">Subject</th>
              <th className="px-4 py-2.5 font-semibold">Role</th>
              <th className="px-4 py-2.5 font-semibold">Scope</th>
              <th className="px-4 py-2.5 font-semibold">Expires</th>
              <th className="px-4 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {(grants.data ?? []).map((item) => (
              <tr key={item.id} className="border-b border-border-soft last:border-0">
                <td className="px-4 py-2.5">
                  <div className="flex items-center gap-2">
                    <span
                      className={
                        item.subject_kind === 'group'
                          ? 'rounded-full bg-accent-soft px-1.5 py-0.5 text-[11px] font-semibold text-accent-text'
                          : 'rounded-full bg-elev px-1.5 py-0.5 text-[11px] font-semibold text-muted'
                      }
                    >
                      {item.subject_kind === 'group' ? 'group' : 'user'}
                    </span>
                    <span className="text-[13px] font-medium">
                      {item.subject_name || item.email || item.subject_id}
                    </span>
                  </div>
                  {item.email && item.subject_name && (
                    <p className="mt-0.5 text-[12px] text-dim">{item.email}</p>
                  )}
                </td>
                <td className="px-4 py-2.5 text-[13px]">{item.role}</td>
                <td className="px-4 py-2.5 font-mono text-[12px] text-muted">
                  {item.environment} · {item.path}
                </td>
                <td className="px-4 py-2.5 text-[12.5px] text-dim">
                  {item.expires_at ? formatDate(item.expires_at) : 'Never'}
                </td>
                <td className="px-4 py-2.5 text-right">
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label="Revoke access"
                    disabled={revoke.isPending}
                    onClick={() => {
                      revoke.mutate(item)
                    }}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {(grants.data?.length ?? 0) === 0 && !grants.isLoading && (
          <p className="px-4 py-5 text-[13px] text-muted">
            No scoped grants yet. Org-wide roles already apply to everyone.
          </p>
        )}
      </section>
    </div>
  )
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}
