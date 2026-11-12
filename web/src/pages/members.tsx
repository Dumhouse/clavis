import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Loader2, Mail, Trash2, UserPlus, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Groups } from '@/components/groups'
import { Roles } from '@/components/roles'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth } from '@/lib/auth'
import { useCurrentOrg } from '@/lib/org'
import { ensureReauth } from '@/lib/reauth'

type Member = components['schemas']['MemberBody']
type Invite = components['schemas']['InviteBody']

const ROLES = ['Owner', 'Admin', 'Developer', 'Viewer']

export function MembersPage() {
  const { user } = useAuth()
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [inviteLink, setInviteLink] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const members = useQuery({
    queryKey: ['members', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/members', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load members.'))
      return result.data
    },
  })

  const rolesQuery = useQuery({
    queryKey: ['roles', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/roles', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load roles.'))
      return result.data?.roles ?? []
    },
  })
  const roleNames = [
    ...ROLES,
    ...(rolesQuery.data ?? []).filter((role) => !role.builtin).map((role) => role.name),
  ]

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['members', org?.slug] })
  }

  const updateRole = useMutation({
    mutationFn: async (input: { userID: string; role: string }) => {
      if (!org) throw new Error('No organization.')
      const result = await api.PATCH('/api/v1/orgs/{org}/members/{user}', {
        params: { path: { org: org.slug, user: input.userID } },
        body: { role: input.role },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not change the role.'))
    },
    onSuccess: invalidate,
  })

  const removeMember = useMutation({
    mutationFn: async (member: Member) => {
      if (!org) throw new Error('No organization.')
      if (!window.confirm(`Remove ${member.name || member.email} from ${org.name}?`)) return
      const reauthToken = await ensureReauth()
      const result = await api.DELETE('/api/v1/orgs/{org}/members/{user}', {
        params: { path: { org: org.slug, user: member.user_id } },
        headers: { 'X-Clavis-Reauth': reauthToken },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not remove the member.'))
    },
    onSuccess: invalidate,
  })

  const revokeInvite = useMutation({
    mutationFn: async (invite: Invite) => {
      if (!org) throw new Error('No organization.')
      const result = await api.DELETE('/api/v1/orgs/{org}/invites/{id}', {
        params: { path: { org: org.slug, id: invite.id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not revoke the invite.'))
    },
    onSuccess: invalidate,
  })

  async function copyLink() {
    if (!inviteLink) return
    await navigator.clipboard.writeText(inviteLink)
    setCopied(true)
    window.setTimeout(() => {
      setCopied(false)
    }, 1500)
  }

  const canManage = org?.role === 'Owner' || org?.role === 'Admin'

  if (!canManage) {
    return (
      <div className="rounded-app border border-dashed border-border bg-panel p-8">
        <h1 className="font-head text-[20px] font-semibold">Members</h1>
        <p className="mt-1 text-[12.5px] text-muted">
          You need the Admin or Owner role to manage members.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="font-head text-[24px] font-semibold tracking-tight">Members</h1>
          <p className="mt-1 text-[13px] text-muted">
            People with access to {org?.name ?? 'this organization'}.
          </p>
        </div>
        <InviteForm
          orgSlug={org?.slug ?? ''}
          actorRole={org?.role ?? 'Developer'}
          roles={roleNames}
          onCreated={(url) => {
            setInviteLink(url)
          }}
          onDone={() => {
            void invalidate()
          }}
        />
      </header>

      {inviteLink && (
        <section className="rounded-app border border-accent-line bg-accent-soft p-4">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="text-[12px] font-semibold text-accent-text">Invite link created</p>
              <p className="mt-0.5 text-[11.5px] text-muted">
                Share this once. It expires automatically and can be revoked.
              </p>
              <code className="mt-2 block truncate rounded-app-sm border border-border bg-bg px-2.5 py-1.5 font-mono text-[11px] text-text">
                {inviteLink}
              </code>
            </div>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                void copyLink()
              }}
            >
              {copied ? (
                <Check className="size-3.5 text-accent-text" />
              ) : (
                <Copy className="size-3.5" />
              )}
              {copied ? 'Copied' : 'Copy'}
            </Button>
          </div>
        </section>
      )}

      {members.isLoading && (
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-[12px] text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading members…
        </div>
      )}
      {members.isError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-[12px] text-danger">
          {members.error instanceof Error ? members.error.message : 'Could not load members.'}
        </div>
      )}

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[640px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-[10.5px] uppercase tracking-[0.06em] text-dim">
              <th className="px-4 py-2.5 font-semibold">Member</th>
              <th className="px-4 py-2.5 font-semibold">Role</th>
              <th className="px-4 py-2.5 font-semibold">Joined</th>
              <th className="px-4 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {(members.data?.members ?? []).map((member) => (
              <tr key={member.user_id} className="border-b border-border-soft last:border-0">
                <td className="px-4 py-2.5">
                  <div className="flex items-center gap-2.5">
                    <span className="flex size-7 items-center justify-center rounded-full bg-elev text-[10px] font-semibold text-muted">
                      {(member.name || member.email).slice(0, 2).toUpperCase()}
                    </span>
                    <div className="min-w-0">
                      <p className="truncate text-[12px] font-medium">
                        {member.name || member.email}
                        {member.user_id === user?.id && (
                          <span className="ml-2 rounded-full bg-accent-soft px-1.5 py-0.5 text-[10px] font-semibold text-accent-text">
                            you
                          </span>
                        )}
                      </p>
                      <p className="truncate text-[11px] text-dim">{member.email}</p>
                    </div>
                  </div>
                </td>
                <td className="px-4 py-2.5">
                  <select
                    className="h-7 rounded-app-sm border border-border bg-bg px-1.5 text-[11.5px] text-text focus:border-accent-line focus:outline-none"
                    value={member.role}
                    onChange={(event) => {
                      updateRole.mutate({ userID: member.user_id, role: event.target.value })
                    }}
                    disabled={updateRole.isPending}
                  >
                    {roleNames
                      .filter((role) => role !== 'Owner' || org?.role === 'Owner')
                      .map((role) => (
                        <option key={role} value={role}>
                          {role}
                        </option>
                      ))}
                  </select>
                </td>
                <td className="px-4 py-2.5 text-[11.5px] text-dim">
                  {formatDate(member.joined_at)}
                </td>
                <td className="px-4 py-2.5 text-right">
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Remove ${member.email}`}
                    onClick={() => {
                      removeMember.mutate(member)
                    }}
                    disabled={removeMember.isPending}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {(updateRole.error || removeMember.error) && (
          <p className="px-4 py-3 text-[11.5px] text-danger">
            {errorMessage(updateRole.error) || errorMessage(removeMember.error)}
          </p>
        )}
      </section>

      <Roles />

      <Groups />

      <section className="rounded-app border border-border bg-surface">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <Mail className="size-4 text-muted" />
          <h2 className="text-[13px] font-semibold">Pending invites</h2>
          <span className="rounded-full bg-elev px-1.5 text-[10.5px] text-muted">
            {members.data?.invites?.length ?? 0}
          </span>
        </div>
        <div className="divide-y divide-border-soft">
          {(members.data?.invites ?? []).map((invite) => (
            <div key={invite.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="text-[12px] font-medium">{invite.email || 'Anyone with the link'}</p>
                <p className="text-[11px] text-dim">
                  {invite.role} · expires {formatDate(invite.expires_at)}
                </p>
              </div>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  revokeInvite.mutate(invite)
                }}
                disabled={revokeInvite.isPending}
              >
                <X className="size-3.5" /> Revoke
              </Button>
            </div>
          ))}
          {(members.data?.invites?.length ?? 0) === 0 && (
            <p className="px-4 py-5 text-[12px] text-muted">No pending invites.</p>
          )}
        </div>
      </section>
    </div>
  )
}

function InviteForm({
  orgSlug,
  actorRole,
  roles,
  onCreated,
  onDone,
}: {
  orgSlug: string
  actorRole: string
  roles: string[]
  onCreated: (url: string) => void
  onDone: () => void
}) {
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState('')
  const [role, setRole] = useState('Developer')
  const [days, setDays] = useState(7)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/orgs/{org}/invites', {
      params: { path: { org: orgSlug } },
      body: { email, role, expires_in_days: days },
    })
    setBusy(false)
    if (result.error || !result.data) {
      setError(apiErrorMessage(result.error, 'Could not create the invite.'))
      return
    }
    setOpen(false)
    setEmail('')
    onCreated(result.data.url ?? '')
    onDone()
  }

  return (
    <>
      <Button
        variant="primary"
        size="md"
        onClick={() => {
          setOpen(true)
        }}
      >
        <UserPlus className="size-4" /> Invite member
      </Button>
      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4">
          <div className="w-full max-w-md rounded-app border border-border bg-surface p-5 shadow-app">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 className="font-head text-[17px] font-semibold">Invite member</h2>
                <p className="mt-0.5 text-[12px] text-muted">
                  Creates a single-use link. Email delivery is optional.
                </p>
              </div>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => {
                  setOpen(false)
                }}
                aria-label="Close"
              >
                <X className="size-4" />
              </Button>
            </div>
            <form
              onSubmit={(event) => {
                void submit(event)
              }}
              className="mt-4 space-y-3"
            >
              <label className="block">
                <span className="mb-1 block text-[11.5px] font-medium text-muted">
                  Email (optional)
                </span>
                <input
                  type="email"
                  className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
                  placeholder="teammate@example.com"
                  value={email}
                  onChange={(event) => {
                    setEmail(event.target.value)
                  }}
                />
              </label>
              <div className="grid grid-cols-2 gap-3">
                <label className="block">
                  <span className="mb-1 block text-[11.5px] font-medium text-muted">Role</span>
                  <select
                    className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
                    value={role}
                    onChange={(event) => {
                      setRole(event.target.value)
                    }}
                  >
                    {roles
                      .filter((candidate) => candidate !== 'Owner' || actorRole === 'Owner')
                      .map((candidate) => (
                        <option key={candidate} value={candidate}>
                          {candidate}
                        </option>
                      ))}
                  </select>
                </label>
                <label className="block">
                  <span className="mb-1 block text-[11.5px] font-medium text-muted">
                    Expires in
                  </span>
                  <select
                    className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none"
                    value={days}
                    onChange={(event) => {
                      setDays(Number(event.target.value))
                    }}
                  >
                    <option value={1}>1 day</option>
                    <option value={7}>7 days</option>
                    <option value={14}>14 days</option>
                    <option value={30}>30 days</option>
                  </select>
                </label>
              </div>
              {error && (
                <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[11.5px] text-danger">
                  {error}
                </p>
              )}
              <div className="flex justify-end gap-2 pt-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setOpen(false)
                  }}
                >
                  Cancel
                </Button>
                <Button type="submit" variant="primary" size="sm" disabled={busy}>
                  {busy && <Loader2 className="size-3.5 animate-spin" />} Create link
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  )
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : ''
}
