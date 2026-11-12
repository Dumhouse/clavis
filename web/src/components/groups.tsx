import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, Trash2, UserPlus, Users, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { confirmAction } from '@/components/ui/dialogs'
import { Input, Select } from '@/components/ui/field'

type Group = components['schemas']['GroupBody']
type Member = components['schemas']['MemberBody']

export function Groups() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)

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

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['groups', org?.slug] })
  }

  const createGroup = useMutation({
    mutationFn: async (groupName: string) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/groups', {
        params: { path: { org: org.slug } },
        body: { name: groupName },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not create the group.'))
    },
    onSuccess: async () => {
      setName('')
      await invalidate()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not create the group.')
    },
  })

  const deleteGroup = useMutation({
    mutationFn: async (group: Group) => {
      if (!org) throw new Error('No organization.')
      if (
        !(await confirmAction(
          `Delete the group “${group.name}”? Its project grants are removed too.`,
        ))
      )
        return
      const result = await api.DELETE('/api/v1/orgs/{org}/groups/{id}', {
        params: { path: { org: org.slug, id: group.id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not delete the group.'))
    },
    onSuccess: invalidate,
  })

  const addMember = useMutation({
    mutationFn: async (input: { groupID: string; userID: string }) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/groups/{id}/members', {
        params: { path: { org: org.slug, id: input.groupID } },
        body: { user_id: input.userID },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not add the member.'))
    },
    onSuccess: invalidate,
  })

  const removeMember = useMutation({
    mutationFn: async (input: { groupID: string; userID: string }) => {
      if (!org) throw new Error('No organization.')
      const result = await api.DELETE('/api/v1/orgs/{org}/groups/{id}/members/{user}', {
        params: { path: { org: org.slug, id: input.groupID, user: input.userID } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not remove the member.'))
    },
    onSuccess: invalidate,
  })

  function submit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    const trimmed = name.trim()
    if (!trimmed) return
    createGroup.mutate(trimmed)
  }

  return (
    <section className="rounded-app border border-border bg-surface">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <Users className="size-4 text-muted" />
        <h2 className="text-base font-semibold">Groups</h2>
        <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">
          {groups.data?.length ?? 0}
        </span>
        <form onSubmit={submit} className="ml-auto flex items-center gap-2">
          <Input
            className="h-8 w-40"
            placeholder="New group name"
            value={name}
            onChange={(event) => {
              setName(event.target.value)
            }}
            maxLength={100}
          />
          <Button
            type="submit"
            variant="secondary"
            size="sm"
            disabled={createGroup.isPending || name.trim() === ''}
          >
            {createGroup.isPending ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <Plus className="size-3.5" />
            )}
            Create
          </Button>
        </form>
      </div>

      <p className="border-b border-border-soft px-4 py-2 text-xs text-dim">
        Groups collect people so you can grant a project role once. Members inherit the group’s
        grants.
      </p>

      {groups.isError && (
        <p className="px-4 py-3 text-xs text-danger">
          {groups.error instanceof Error ? groups.error.message : 'Could not load groups.'}
        </p>
      )}
      {error && <p className="px-4 py-3 text-xs text-danger">{error}</p>}

      <div className="divide-y divide-border-soft">
        {(groups.data ?? []).map((group) => (
          <GroupRow
            key={group.id}
            group={group}
            members={members.data ?? []}
            onAdd={(userID) => {
              addMember.mutate({ groupID: group.id, userID })
            }}
            onRemove={(userID) => {
              removeMember.mutate({ groupID: group.id, userID })
            }}
            onDelete={() => {
              deleteGroup.mutate(group)
            }}
            busy={addMember.isPending || removeMember.isPending || deleteGroup.isPending}
          />
        ))}
        {(groups.data?.length ?? 0) === 0 && !groups.isLoading && (
          <p className="px-4 py-5 text-sm text-muted">
            No groups yet. Create one, then grant it project access.
          </p>
        )}
        {groups.isLoading && (
          <p className="flex items-center gap-2 px-4 py-5 text-sm text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading groups…
          </p>
        )}
      </div>
    </section>
  )
}

function GroupRow({
  group,
  members,
  onAdd,
  onRemove,
  onDelete,
  busy,
}: {
  group: Group
  members: Member[]
  onAdd: (userID: string) => void
  onRemove: (userID: string) => void
  onDelete: () => void
  busy: boolean
}) {
  const [selected, setSelected] = useState('')
  const inGroup = new Set((group.members ?? []).map((member) => member.user_id))
  const candidates = members.filter((member) => !inGroup.has(member.user_id))

  return (
    <div className="px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-sm font-medium text-text">{group.name}</p>
        <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">
          {group.members?.length ?? 0}
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="ml-auto"
          aria-label={`Delete ${group.name}`}
          disabled={busy}
          onClick={onDelete}
        >
          <Trash2 className="size-3.5" />
        </Button>
      </div>

      <div className="mt-2 flex flex-wrap gap-1.5">
        {(group.members ?? []).map((member) => (
          <span
            key={member.user_id}
            className="inline-flex items-center gap-1 rounded-full border border-border bg-bg py-0.5 pl-2 pr-1 text-xs text-muted"
          >
            {member.name || member.email}
            <button
              type="button"
              className="rounded-full p-0.5 text-dim hover:bg-elev hover:text-text"
              aria-label={`Remove ${member.email} from ${group.name}`}
              disabled={busy}
              onClick={() => {
                onRemove(member.user_id)
              }}
            >
              <X className="size-3" />
            </button>
          </span>
        ))}
        {(group.members?.length ?? 0) === 0 && (
          <span className="text-xs text-dim">No members yet.</span>
        )}
      </div>

      {candidates.length > 0 && (
        <div className="mt-2 flex items-center gap-2">
          <Select
            className="h-7 px-1.5 text-xs"
            value={selected}
            onChange={(event) => {
              setSelected(event.target.value)
            }}
          >
            <option value="">Add a member…</option>
            {candidates.map((member) => (
              <option key={member.user_id} value={member.user_id}>
                {member.name || member.email}
              </option>
            ))}
          </Select>
          <Button
            variant="secondary"
            size="sm"
            disabled={busy || selected === ''}
            onClick={() => {
              if (!selected) return
              onAdd(selected)
              setSelected('')
            }}
          >
            <UserPlus className="size-3.5" /> Add
          </Button>
        </div>
      )}
    </div>
  )
}
