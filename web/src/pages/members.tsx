import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Loader2, Mail, Trash2, UserMinus, UserPlus, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Groups } from '@/components/groups'
import { PageIntro } from '@/components/page-intro'
import { Roles } from '@/components/roles'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth } from '@/lib/auth'
import { useCurrentOrg } from '@/lib/org'
import { ensureReauth } from '@/lib/reauth'
import { confirmAction } from '@/components/ui/dialogs'
import { Input, Select } from '@/components/ui/field'
import { useShortcuts } from '@/lib/use-shortcuts'

type Member = components['schemas']['MemberBody']
type Invite = components['schemas']['InviteBody']

const ROLES = ['Owner', 'Admin', 'Developer', 'Viewer']

export function MembersPage() {
  const { user } = useAuth()
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [inviteLink, setInviteLink] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [offboarding, setOffboarding] = useState<Member | null>(null)

  const members = useQuery({
    // Own key: groups and access cache just the member array under ['members', org].
    queryKey: ['members', org?.slug, 'with-invites'],
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
      if (!(await confirmAction(`Remove ${member.name || member.email} from ${org.name}?`)))
        return null
      const reauthToken = await ensureReauth()
      const result = await api.DELETE('/api/v1/orgs/{org}/members/{user}', {
        params: { path: { org: org.slug, user: member.user_id } },
        headers: { 'X-Clavis-Reauth': reauthToken },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not remove the member.'))
      return member
    },
    onSuccess: async (removed) => {
      await invalidate()
      // The checklist of revealed secrets matters most once the person is gone.
      if (removed) setOffboarding(removed)
    },
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
        <h1 className="font-head text-xl font-semibold">Members</h1>
        <p className="mt-1 text-sm text-muted">
          You need the Admin or Owner role to manage members.
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="font-head text-2xl font-semibold tracking-tight">Members</h1>
          <p className="mt-1 text-base text-muted">
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

      <PageIntro
        id="members"
        summary="Invite people, set their role, and take their access away when they leave."
        steps={[
          {
            title: 'Invite',
            body: 'Create an invite link and pick a role. The link expires and can be revoked.',
          },
          {
            title: 'Offboard',
            body: 'Opens a checklist of every secret the person has revealed, so you can rotate them. It does not remove the person; use it before removing, or for someone who stays.',
          },
          {
            title: 'Remove',
            body: 'Takes the person out of the organization right away and signs them out. Their tokens and grants are revoked. The last Owner cannot be removed.',
          },
        ]}
        when="Someone leaves: offboard first, rotate what the checklist lists, then remove."
      />

      {inviteLink && (
        <section className="rounded-app border border-accent-line bg-accent-soft p-4">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="text-sm font-semibold text-accent-text">Invite link created</p>
              <p className="mt-0.5 text-xs text-muted">
                Share this once. It expires automatically and can be revoked.
              </p>
              <code className="mt-2 block truncate rounded-app-sm border border-border bg-bg px-2.5 py-1.5 font-mono text-xs text-text">
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
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-sm text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading members…
        </div>
      )}
      {members.isError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-sm text-danger">
          {members.error instanceof Error ? members.error.message : 'Could not load members.'}
        </div>
      )}

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[640px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
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
                    <span className="flex size-7 items-center justify-center rounded-full bg-elev text-2xs font-semibold text-muted">
                      {(member.name || member.email).slice(0, 2).toUpperCase()}
                    </span>
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">
                        {member.name || member.email}
                        {member.user_id === user?.id && (
                          <span className="ml-2 rounded-full bg-accent-soft px-1.5 py-0.5 text-2xs font-semibold text-accent-text">
                            you
                          </span>
                        )}
                      </p>
                      <p className="truncate text-xs text-dim">{member.email}</p>
                    </div>
                  </div>
                </td>
                <td className="px-4 py-2.5">
                  <Select
                    className="h-7 px-1.5 text-xs"
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
                  </Select>
                </td>
                <td className="px-4 py-2.5 text-xs text-dim">{formatDate(member.joined_at)}</td>
                <td className="px-4 py-2.5 text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={`Offboard ${member.email}`}
                    title="Review the secrets this person revealed so you can rotate them. Does not remove them."

                    onClick={() => {
                      setOffboarding(member)
                    }}
                  >
                    <UserMinus className="size-3.5" /> Offboard
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Remove ${member.email}`}
                    title="Remove from the organization and sign them out"

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
          <p className="px-4 py-3 text-xs text-danger">
            {errorMessage(updateRole.error) || errorMessage(removeMember.error)}
          </p>
        )}
      </section>

      {offboarding && org && (
        <OffboardingDialog
          orgSlug={org.slug}
          member={offboarding}
          onClose={() => {
            setOffboarding(null)
          }}
        />
      )}

      <Roles />

      <Groups />

      <section className="rounded-app border border-border bg-surface">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <Mail className="size-4 text-muted" />
          <h2 className="text-base font-semibold">Pending invites</h2>
          <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">
            {members.data?.invites?.length ?? 0}
          </span>
        </div>
        <div className="divide-y divide-border-soft">
          {(members.data?.invites ?? []).map((invite) => (
            <div key={invite.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">{invite.email || 'Anyone with the link'}</p>
                <p className="text-xs text-dim">
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
            <p className="px-4 py-5 text-sm text-muted">No pending invites.</p>
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
  useShortcuts({
    n: () => {
      setOpen(true)
    },
  })
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
        <Modal
          onClose={() => {
            setOpen(false)
          }}
          className="max-w-md"
        >
          <div className="flex items-start justify-between gap-3">
            <div>
              <h2 className="font-head text-lg font-semibold">Invite member</h2>
              <p className="mt-0.5 text-sm text-muted">
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
              <span className="mb-1 block text-xs font-medium text-muted">Email (optional)</span>
              <Input
                type="email"
                className="h-9"
                placeholder="teammate@example.com"
                value={email}
                onChange={(event) => {
                  setEmail(event.target.value)
                }}
              />
            </label>
            <div className="grid grid-cols-2 gap-3">
              <label className="block">
                <span className="mb-1 block text-xs font-medium text-muted">Role</span>
                <Select
                  className="h-9"
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
                </Select>
              </label>
              <label className="block">
                <span className="mb-1 block text-xs font-medium text-muted">Expires in</span>
                <Select
                  className="h-9"
                  value={days}
                  onChange={(event) => {
                    setDays(Number(event.target.value))
                  }}
                >
                  <option value={1}>1 day</option>
                  <option value={7}>7 days</option>
                  <option value={14}>14 days</option>
                  <option value={30}>30 days</option>
                </Select>
              </label>
            </div>
            {error && (
              <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
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
        </Modal>
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

function OffboardingDialog({
  orgSlug,
  member,
  onClose,
}: {
  orgSlug: string
  member: Member
  onClose: () => void
}) {
  const queryClient = useQueryClient()
  const [started, setStarted] = useState(false)
  const report = useQuery({
    queryKey: ['offboarding', orgSlug, member.user_id],
    queryFn: async () => {
      const result = await api.GET('/api/v1/orgs/{org}/members/{user}/offboarding', {
        params: { path: { org: orgSlug, user: member.user_id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load offboarding review.'))
      return result.data
    },
  })
  const start = useMutation({
    mutationFn: async () => {
      const result = await api.POST('/api/v1/orgs/{org}/members/{user}/offboarding', {
        params: { path: { org: orgSlug, user: member.user_id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not start offboarding.'))
      return result.data
    },
    onSuccess: async () => {
      setStarted(true)
      await queryClient.invalidateQueries({ queryKey: ['offboarding', orgSlug, member.user_id] })
    },
  })
  const mark = useMutation({
    mutationFn: async ({ id, status }: { id: string; status: 'safe' | 'rotated' }) => {
      const result = await api.POST('/api/v1/orgs/{org}/members/{user}/offboarding/{id}', {
        params: { path: { org: orgSlug, user: member.user_id, id } },
        body: { status },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not update the task.'))
      return result.data
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['offboarding', orgSlug, member.user_id] })
    },
  })
  return (
    <Modal onClose={onClose} className="max-w-2xl">
      <div className="flex items-start justify-between">
        <div>
          <h2 className="font-head text-md font-semibold">Offboarding review</h2>
          <p className="text-2xs text-muted">
            {member.email} ·{' '}
            {report.data?.removed
              ? 'removed from the organization; rotate what they revealed'
              : 'revealed secrets to rotate or mark safe'}
          </p>
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
          <X className="size-4" />
        </Button>
      </div>
      {report.data && (
        <p className="mt-3 text-xs text-muted">
          {report.data.done}/{report.data.total} reviewed · {report.data.exported} export reveal(s)
        </p>
      )}
      {report.isError && (
        <p className="mt-3 text-xs text-danger">
          {report.error instanceof Error ? report.error.message : 'Could not load review.'}
        </p>
      )}
      <div className="mt-3 max-h-72 overflow-auto rounded-app-sm border border-border">
        <table className="w-full min-w-[620px] text-left text-2xs">
          <thead className="sticky top-0 bg-surface">
            <tr className="border-b border-border text-3xs uppercase tracking-[0.05em] text-dim">
              <th className="px-3 py-2">Key</th>
              <th className="px-3 py-2">Location</th>
              <th className="px-3 py-2">Status</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody>
            {(report.data?.tasks ?? []).map((task) => (
              <tr key={task.id} className="border-b border-border-soft last:border-0">
                <td className="px-3 py-2 font-mono">{task.key}</td>
                <td className="px-3 py-2 text-muted">
                  {task.project}/{task.environment}
                </td>
                <td className="px-3 py-2">{task.status}</td>
                <td className="px-3 py-2 text-right">
                  {task.status === 'pending' && (
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={mark.isPending}
                        onClick={() => {
                          mark.mutate({ id: task.id, status: 'rotated' })
                        }}
                      >
                        Rotated
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={mark.isPending}
                        onClick={() => {
                          mark.mutate({ id: task.id, status: 'safe' })
                        }}
                      >
                        Safe
                      </Button>
                    </div>
                  )}
                </td>
              </tr>
            ))}
            {(report.data?.tasks ?? []).length === 0 && !report.isLoading && (
              <tr>
                <td colSpan={4} className="px-3 py-6 text-center text-xs text-muted">
                  No reveal history yet. Start a review to create tasks.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {start.error && (
        <p className="mt-2 text-2xs text-danger">
          {start.error instanceof Error ? start.error.message : 'Could not start.'}
        </p>
      )}
      {mark.error && (
        <p className="mt-2 text-2xs text-danger">
          {mark.error instanceof Error ? mark.error.message : 'Could not update.'}
        </p>
      )}
      <div className="mt-4 flex justify-end gap-2">
        {!started && (report.data?.tasks ?? []).length === 0 && (
          <Button
            variant="primary"
            size="sm"
            disabled={start.isPending}
            onClick={() => {
              start.mutate()
            }}
          >
            {start.isPending && <Loader2 className="size-3.5 animate-spin" />} Start review
          </Button>
        )}
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
    </Modal>
  )
}
