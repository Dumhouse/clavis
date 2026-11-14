import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import {
  AlertTriangle,
  ArrowLeft,
  Eye,
  Loader2,
  PenLine,
  ShieldCheck,
  Trash2,
  User,
  UserPlus,
  Users,
  type LucideIcon,
} from 'lucide-react'
import { useMemo, useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Cmd, PageIntro } from '@/components/page-intro'
import { Button } from '@/components/ui/button'
import { confirmAction } from '@/components/ui/dialogs'
import { Input, Notice, Select } from '@/components/ui/field'
import { toast } from '@/components/ui/toast'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'
import { formatDate as formatDay } from '@/lib/format'

type Grant = components['schemas']['GrantBody']
type Member = components['schemas']['MemberBody']

type RoleName = 'Viewer' | 'Developer' | 'Admin'

const roles: { name: RoleName; icon: LucideIcon; can: string; detail: string }[] = [
  {
    name: 'Viewer',
    icon: Eye,
    can: 'see key names',
    detail: 'Sees the project and which keys exist. Cannot read or change values.',
  },
  {
    name: 'Developer',
    icon: PenLine,
    can: 'read and change values',
    detail:
      'Reads and edits values, runs apps with them, and manages syncs. Protected environments still need an Admin to change values.',
  },
  {
    name: 'Admin',
    icon: ShieldCheck,
    can: 'manage everything in the project',
    detail: 'Everything a Developer can, plus environments, access, and project settings.',
  },
]

const roleRank: Record<string, number> = { Viewer: 1, Developer: 2, Admin: 3, Owner: 4 }

const durations: [number, string][] = [
  [0, 'No end date'],
  [1, '1 day'],
  [7, '7 days'],
  [30, '30 days'],
  [90, '90 days'],
]

export function ProjectAccessPage() {
  const { org } = useCurrentOrg()
  const params = useParams({ strict: false }) as { project?: string }
  const projectSlug = params.project ?? ''
  const queryClient = useQueryClient()

  const [subjectKind, setSubjectKind] = useState<'user' | 'group'>('user')
  const [subjectID, setSubjectID] = useState('')
  const [role, setRole] = useState<RoleName>('Developer')
  const [allEnvs, setAllEnvs] = useState(true)
  const [envs, setEnvs] = useState<string[]>([])
  const [path, setPath] = useState('/**')
  const [days, setDays] = useState(0)
  const [error, setError] = useState<string | null>(null)

  const project = useQuery({
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

  const projectEnvs = useMemo(
    () => [...(project.data?.environments ?? [])].sort((a, b) => a.position - b.position),
    [project.data],
  )
  const projectName = project.data?.name ?? projectSlug

  const selectedMember: Member | undefined =
    subjectKind === 'user'
      ? (members.data ?? []).find((member) => member.user_id === subjectID)
      : undefined
  const selectedGroup =
    subjectKind === 'group' ? (groups.data ?? []).find((g) => g.id === subjectID) : undefined
  const subjectLabel = selectedMember
    ? selectedMember.name || selectedMember.email
    : (selectedGroup?.name ?? '')

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['access', org?.slug, projectSlug] })
  }

  const grant = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const targets = allEnvs ? ['*'] : envs
      for (const environment of targets) {
        const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/access', {
          params: { path: { org: org.slug, project: projectSlug } },
          body: {
            subject_kind: subjectKind,
            subject_id: subjectID,
            role,
            environment,
            path: path.trim() || '/**',
            expires_in_days: days,
          },
        })
        if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not grant access.'))
      }
    },
    onSuccess: async () => {
      toast(`Granted ${role} access to ${subjectLabel || 'the selection'}.`, { tone: 'success' })
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
    onError: (cause: unknown) => {
      toast(cause instanceof Error ? cause.message : 'Could not revoke access.', { tone: 'error' })
    },
  })

  // A project grant only adds access. If the org role already covers it, the
  // grant changes nothing, which is the most common source of confusion.
  const redundant =
    selectedMember !== undefined && (roleRank[selectedMember.role] ?? 0) >= (roleRank[role] ?? 0)

  const roleInfo = roles.find((candidate) => candidate.name === role)
  const whereText = allEnvs
    ? 'every environment'
    : envs.length === 0
      ? 'no environments yet'
      : envs.join(', ')
  const whenText =
    days === 0 ? 'with no end date' : `for ${durations.find(([d]) => d === days)?.[1] ?? ''}`

  function submit(event: FormEvent) {
    event.preventDefault()
    if (!subjectID) {
      setError(`Choose a ${subjectKind === 'user' ? 'person' : 'group'} first.`)
      return
    }
    if (!allEnvs && envs.length === 0) {
      setError('Pick at least one environment, or choose all environments.')
      return
    }
    grant.mutate()
  }

  // Group grants by who they are for, so each person reads as one line.
  const grouped = useMemo(() => {
    const map = new Map<string, { label: string; kind: string; email?: string; items: Grant[] }>()
    for (const item of grants.data ?? []) {
      const key = `${item.subject_kind}:${item.subject_id}`
      const entry = map.get(key) ?? {
        label: item.subject_name || item.email || item.subject_id,
        kind: item.subject_kind,
        ...(item.email && item.subject_name ? { email: item.email } : {}),
        items: [],
      }
      entry.items.push(item)
      map.set(key, entry)
    }
    return [...map.values()]
  }, [grants.data])

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/projects/$project"
          params={{ project: projectSlug }}
          className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> {projectName}
        </Link>
        <div className="mt-2 flex items-center gap-2">
          <ShieldCheck className="size-5 text-muted" />
          <h1 className="font-head text-2xl font-semibold tracking-tight">Project access</h1>
        </div>
        <p className="mt-1 text-base text-muted">
          Give people or groups access to {projectName}, optionally only in some environments or for
          a limited time.
        </p>
      </div>

      <PageIntro
        id="project-access"
        summary={
          <>
            Everyone in the organization has an <b>org role</b> (set on Members) that applies to
            every project. Grants here <b>add</b> access for this project only; they never take
            access away.
          </>
        }
        steps={[
          {
            title: 'Keep the org role low',
            body: (
              <>
                To limit someone to certain projects, make them <b>Viewer</b> on{' '}
                <Link to="/members" className="text-accent-text hover:underline">
                  Members
                </Link>
                . A Developer org role already reaches every project.
              </>
            ),
          },
          {
            title: 'Grant what they need here',
            body: 'Pick a role and the environments it applies to, for example Developer in dev and staging only.',
          },
          {
            title: 'Add an end date for temporary help',
            body: 'Contractors and incident helpers get access that expires on its own.',
          },
        ]}
        footer={
          <p className="text-xs text-muted">
            Check the result any time on{' '}
            <Link to="/access" className="text-accent-text hover:underline">
              Who has access
            </Link>
            , or from the terminal with <Cmd>clavis access explain EMAIL</Cmd>. Need access for a
            few hours instead? Ask with <Cmd>clavis access request</Cmd>.
          </p>
        }
      />

      <form
        onSubmit={submit}
        className="rounded-app border border-border bg-surface"
        aria-labelledby="grant-heading"
      >
        <div className="border-b border-border px-4 py-3">
          <h2 id="grant-heading" className="text-base font-semibold">
            Grant access
          </h2>
          <p className="text-xs text-dim">You can only grant permissions you hold yourself.</p>
        </div>

        <div className="divide-y divide-border-soft">
          {/* 1. Who */}
          <Step number={1} title="Who">
            <div className="flex flex-wrap items-center gap-2">
              <Segmented
                value={subjectKind}
                onChange={(value) => {
                  setSubjectKind(value)
                  setSubjectID('')
                }}
                options={[
                  ['user', 'A person', User],
                  ['group', 'A group', Users],
                ]}
              />
              <Select
                aria-label={subjectKind === 'user' ? 'Person' : 'Group'}
                className="min-w-64 flex-1"
                value={subjectID}
                onChange={(event) => {
                  setSubjectID(event.target.value)
                }}
              >
                <option value="">
                  {subjectKind === 'user' ? 'Choose a person…' : 'Choose a group…'}
                </option>
                {subjectKind === 'user'
                  ? (members.data ?? []).map((member) => (
                      <option key={member.user_id} value={member.user_id}>
                        {member.name || member.email} — {member.role} in the organization
                      </option>
                    ))
                  : (groups.data ?? []).map((group) => (
                      <option key={group.id} value={group.id}>
                        {group.name} — {String(group.members?.length ?? 0)} members
                      </option>
                    ))}
              </Select>
            </div>
            {subjectKind === 'group' && (groups.data?.length ?? 0) === 0 && (
              <p className="mt-2 text-xs text-dim">
                No groups yet. Create one on{' '}
                <Link to="/members" className="text-accent-text hover:underline">
                  Members
                </Link>{' '}
                to grant a whole team at once.
              </p>
            )}
          </Step>

          {/* 2. What */}
          <Step number={2} title="What they can do">
            <div role="radiogroup" aria-label="Role" className="grid gap-2 sm:grid-cols-3">
              {roles.map((candidate) => (
                <label
                  key={candidate.name}
                  className={cn(
                    'flex cursor-pointer gap-2.5 rounded-app-sm border px-3 py-2.5 transition-colors',
                    'has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-accent-line',
                    role === candidate.name
                      ? 'border-accent-line bg-accent-soft/40'
                      : 'border-border hover:bg-hover',
                  )}
                >
                  <input
                    type="radio"
                    name="role"
                    className="sr-only"
                    checked={role === candidate.name}
                    onChange={() => {
                      setRole(candidate.name)
                    }}
                  />
                  <candidate.icon
                    className={cn(
                      'mt-0.5 size-4 shrink-0',
                      role === candidate.name ? 'text-accent-text' : 'text-dim',
                    )}
                  />
                  <span className="min-w-0">
                    <span className="block text-sm font-medium">{candidate.name}</span>
                    <span className="mt-0.5 block text-xs leading-snug text-muted">
                      {candidate.detail}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </Step>

          {/* 3. Where */}
          <Step number={3} title="Where">
            <div className="flex flex-wrap gap-1.5">
              <Chip
                active={allEnvs}
                onClick={() => {
                  setAllEnvs(true)
                  setEnvs([])
                }}
              >
                All environments
              </Chip>
              {projectEnvs.map((env) => {
                const active = !allEnvs && envs.includes(env.slug)
                return (
                  <Chip
                    key={env.id}
                    active={active}
                    tone={env.type === 'production' ? 'prod' : undefined}
                    onClick={() => {
                      setAllEnvs(false)
                      setEnvs((current) =>
                        current.includes(env.slug)
                          ? current.filter((slug) => slug !== env.slug)
                          : [...current, env.slug],
                      )
                    }}
                  >
                    {env.name}
                  </Chip>
                )
              })}
            </div>
            <details className="mt-3 text-xs text-muted">
              <summary className="cursor-pointer select-none hover:text-text">
                Only some folders (advanced)
              </summary>
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <Input
                  mono
                  aria-label="Folder pattern"
                  className="h-8 w-56"
                  value={path}
                  onChange={(event) => {
                    setPath(event.target.value)
                  }}
                  placeholder="/**"
                />
                <span>
                  <Cmd>/**</Cmd> is every key. <Cmd>/backend/**</Cmd> limits to keys in the backend
                  folder.
                </span>
              </div>
            </details>
          </Step>

          {/* 4. How long */}
          <Step number={4} title="For how long">
            <div className="flex flex-wrap gap-1.5">
              {durations.map(([value, label]) => (
                <Chip
                  key={value}
                  active={days === value}
                  onClick={() => {
                    setDays(value)
                  }}
                >
                  {label}
                </Chip>
              ))}
            </div>
          </Step>
        </div>

        <div className="space-y-3 border-t border-border px-4 py-3">
          {redundant && selectedMember && (
            <Notice tone="warning">
              <span className="flex items-start gap-2">
                <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
                <span>
                  {selectedMember.name || selectedMember.email} is already{' '}
                  <b>{selectedMember.role}</b> across the whole organization, so this grant adds
                  nothing. To limit them to this project, change their org role to Viewer on{' '}
                  <Link to="/members" className="underline">
                    Members
                  </Link>{' '}
                  first.
                </span>
              </span>
            </Notice>
          )}
          {error && <Notice>{error}</Notice>}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-muted">
              {subjectLabel ? <b className="text-text">{subjectLabel}</b> : 'They'} will be able to{' '}
              <b className="text-text">{roleInfo?.can}</b> in{' '}
              <b className="text-text">{whereText}</b> of {projectName}, {whenText}.
            </p>
            <Button type="submit" variant="primary" size="sm" disabled={grant.isPending}>
              {grant.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <UserPlus className="size-3.5" />
              )}
              Grant access
            </Button>
          </div>
        </div>
      </form>

      <section className="rounded-app border border-border bg-surface">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <h2 className="text-base font-semibold">Who has extra access here</h2>
          <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">{grouped.length}</span>
        </div>
        {grants.isError && (
          <p className="px-4 py-3 text-xs text-danger">
            {grants.error instanceof Error ? grants.error.message : 'Could not load grants.'}
          </p>
        )}
        {grouped.length === 0 && !grants.isLoading ? (
          <p className="px-4 py-6 text-sm text-muted">
            No project grants yet, so everyone has exactly their org role here.
          </p>
        ) : (
          <ul className="divide-y divide-border-soft">
            {grouped.map((entry) => (
              <li key={`${entry.kind}:${entry.label}`} className="px-4 py-3">
                <div className="flex items-center gap-2">
                  {entry.kind === 'group' ? (
                    <Users className="size-4 text-accent-text" />
                  ) : (
                    <User className="size-4 text-dim" />
                  )}
                  <span className="text-sm font-medium">{entry.label}</span>
                  {entry.email && <span className="text-xs text-dim">{entry.email}</span>}
                </div>
                <ul className="mt-1.5 space-y-1 pl-6">
                  {entry.items.map((item) => (
                    <li key={item.id} className="group flex flex-wrap items-center gap-x-2 text-sm">
                      <span className="rounded-full bg-elev px-2 py-0.5 text-2xs font-semibold text-text">
                        {item.role}
                      </span>
                      <span className="text-muted">
                        {item.environment === '*' ? 'all environments' : item.environment}
                        {item.path !== '/**' && item.path !== '/' && (
                          <>
                            {' '}
                            · <span className="font-mono text-xs">{item.path}</span>
                          </>
                        )}
                      </span>
                      <span className="text-xs text-dim">
                        · {item.expires_at ? `until ${formatDate(item.expires_at)}` : 'no end date'}
                      </span>
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="ml-auto opacity-60 group-hover:opacity-100"
                        aria-label={`Remove ${item.role} access for ${entry.label}`}
                        disabled={revoke.isPending}
                        onClick={() => {
                          void (async () => {
                            const env =
                              item.environment === '*' ? 'all environments' : item.environment
                            if (
                              await confirmAction(
                                `Remove ${entry.label}'s ${item.role} access in ${env}? They keep their org role.`,
                                'Remove access',
                              )
                            )
                              revoke.mutate(item)
                          })()
                        }}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}

function Step({
  number,
  title,
  children,
}: {
  number: number
  title: string
  children: React.ReactNode
}) {
  return (
    <div
      role="group"
      aria-labelledby={`grant-step-${String(number)}`}
      className="grid items-start gap-2 px-4 py-3.5 sm:grid-cols-[9rem_minmax(0,1fr)] sm:gap-4"
    >
      <span
        id={`grant-step-${String(number)}`}
        className="flex items-center gap-2 pt-1.5 text-sm font-medium"
      >
        <span className="flex size-5 items-center justify-center rounded-full bg-elev font-mono text-2xs text-muted">
          {number}
        </span>
        {title}
      </span>
      <div className="min-w-0">{children}</div>
    </div>
  )
}

function Chip({
  active,
  onClick,
  tone,
  children,
}: {
  active: boolean
  onClick: () => void
  tone?: 'prod' | undefined
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-sm transition-colors',
        active
          ? 'border-accent-line bg-accent-soft text-accent-text'
          : 'border-border text-muted hover:bg-hover hover:text-text',
      )}
    >
      {tone === 'prod' && <span className="size-1.5 rounded-full bg-prod" />}
      {children}
    </button>
  )
}

function Segmented<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T
  onChange: (value: T) => void
  options: [T, string, LucideIcon][]
}) {
  return (
    <div role="radiogroup" className="inline-flex rounded-app-sm border border-border bg-bg p-0.5">
      {options.map(([option, label, Icon]) => (
        <button
          key={option}
          type="button"
          role="radio"
          aria-checked={value === option}
          onClick={() => {
            onChange(option)
          }}
          className={cn(
            'inline-flex h-7 items-center gap-1.5 rounded px-2.5 text-sm transition-colors',
            value === option ? 'bg-elev text-text shadow-sm' : 'text-muted hover:text-text',
          )}
        >
          <Icon className="size-3.5" />
          {label}
        </button>
      ))}
    </div>
  )
}

function formatDate(value: string): string {
  return formatDay(value, value)
}
