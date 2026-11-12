import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Loader2, Pencil, Plus, Shield, Trash2, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { confirmAction } from '@/components/ui/dialogs'
import { Input } from '@/components/ui/field'

type Role = components['schemas']['RoleBody']

const permissionGroups: { label: string; permissions: [string, string][] }[] = [
  {
    label: 'Secrets',
    permissions: [
      ['secrets:read-names', 'See key names'],
      ['secrets:read-values', 'Reveal values'],
      ['secrets:use', 'Use without revealing'],
      ['secrets:write', 'Create and update'],
      ['secrets:delete', 'Delete'],
    ],
  },
  {
    label: 'Projects',
    permissions: [
      ['projects:read', 'View projects'],
      ['projects:manage', 'Create and edit projects'],
      ['envs:read', 'View environments'],
      ['envs:manage', 'Create and edit environments'],
    ],
  },
  {
    label: 'Organization',
    permissions: [
      ['org:read', 'View organization'],
      ['members:manage', 'Manage members and groups'],
      ['roles:manage', 'Manage roles and grants'],
      ['syncs:manage', 'Manage syncs'],
      ['audit:read', 'Read the audit log'],
      ['settings:manage', 'Change security settings'],
      ['tokens:manage', 'Manage identity tokens'],
      ['org:delete', 'Delete the organization'],
    ],
  },
]

const permissionLabels = new Map(
  permissionGroups.flatMap((group) => group.permissions.map(([value, label]) => [value, label])),
)

export function Roles() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<Role | 'new' | null>(null)
  const [error, setError] = useState<string | null>(null)

  const roles = useQuery({
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

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['roles', org?.slug] })
  }

  const remove = useMutation({
    mutationFn: async (role: Role) => {
      if (!org) throw new Error('No organization.')
      if (!(await confirmAction(`Delete the role “${role.name}”?`))) return
      const result = await api.DELETE('/api/v1/orgs/{org}/roles/{id}', {
        params: { path: { org: org.slug, id: role.id } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not delete the role.'))
    },
    onSuccess: invalidate,
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not delete the role.')
    },
  })

  const builtin = (roles.data ?? []).filter((role) => role.builtin)
  const custom = (roles.data ?? []).filter((role) => !role.builtin)

  return (
    <section className="rounded-app border border-border bg-surface">
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <Shield className="size-4 text-muted" />
        <h2 className="text-base font-semibold">Roles</h2>
        <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">
          {roles.data?.length ?? 0}
        </span>
        <Button
          variant="secondary"
          size="sm"
          className="ml-auto"
          onClick={() => {
            setEditing('new')
            setError(null)
          }}
        >
          <Plus className="size-3.5" /> Custom role
        </Button>
      </div>

      {error && <p className="px-4 py-3 text-xs text-danger">{error}</p>}

      {editing && (
        <RoleEditor
          role={editing === 'new' ? null : editing}
          orgSlug={org?.slug ?? ''}
          onClose={() => {
            setEditing(null)
          }}
          onSaved={() => {
            setEditing(null)
            void invalidate()
          }}
        />
      )}

      <div className="divide-y divide-border-soft">
        {(roles.data ?? []).map((role) => (
          <div key={role.id} className="px-4 py-3">
            <div className="flex flex-wrap items-center gap-2">
              <p className="text-sm font-medium">{role.name}</p>
              <span className="rounded-full bg-elev px-1.5 py-0.5 text-2xs font-semibold text-muted">
                {role.builtin ? 'built-in' : 'custom'}
              </span>
              {!role.builtin && (
                <span className="ml-auto flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Edit ${role.name}`}
                    onClick={() => {
                      setEditing(role)
                      setError(null)
                    }}
                  >
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={`Delete ${role.name}`}
                    disabled={remove.isPending}
                    onClick={() => {
                      remove.mutate(role)
                    }}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </span>
              )}
            </div>
            <p className="mt-1 text-xs text-dim">
              {(role.permissions ?? []).length === 0
                ? 'No permissions'
                : (role.permissions ?? [])
                    .map((permission) => permissionLabels.get(permission) ?? permission)
                    .join(' · ')}
            </p>
          </div>
        ))}
        {roles.isLoading && (
          <p className="flex items-center gap-2 px-4 py-5 text-sm text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading roles…
          </p>
        )}
        {!roles.isLoading && (roles.data?.length ?? 0) === 0 && (
          <p className="px-4 py-5 text-sm text-muted">
            Only built-in roles exist. Create a custom role to fine-tune access.
          </p>
        )}
      </div>

      {builtin.length + custom.length > 0 && (
        <p className="border-t border-border-soft px-4 py-2 text-xs text-dim">
          Custom roles can never grant a permission you do not hold yourself.
        </p>
      )}
    </section>
  )
}

function RoleEditor({
  role,
  orgSlug,
  onClose,
  onSaved,
}: {
  role: Role | null
  orgSlug: string
  onClose: () => void
  onSaved: () => void
}) {
  const [name, setName] = useState(role?.name ?? '')
  const [permissions, setPermissions] = useState<string[]>(role?.permissions ?? [])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  function toggle(value: string) {
    setPermissions((current) =>
      current.includes(value) ? current.filter((item) => item !== value) : [...current, value],
    )
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const body = { name: name.trim(), permissions }
    const result = role
      ? await api.PATCH('/api/v1/orgs/{org}/roles/{id}', {
          params: { path: { org: orgSlug, id: role.id } },
          body,
        })
      : await api.POST('/api/v1/orgs/{org}/roles', {
          params: { path: { org: orgSlug } },
          body,
        })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not save the role.'))
      return
    }
    onSaved()
  }

  return (
    <form
      onSubmit={(event) => {
        void submit(event)
      }}
      className="border-b border-border bg-panel p-4"
    >
      <div className="flex items-center gap-2">
        <h3 className="text-sm font-semibold">{role ? `Edit ${role.name}` : 'New custom role'}</h3>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="ml-auto"
          aria-label="Close editor"
          onClick={onClose}
        >
          <X className="size-3.5" />
        </Button>
      </div>
      <label className="mt-3 block max-w-xs">
        <span className="mb-1 block text-xs font-medium text-muted">Role name</span>
        <Input
          className="h-8"
          value={name}
          onChange={(event) => {
            setName(event.target.value)
          }}
          maxLength={60}
          required
        />
      </label>
      <div className="mt-3 grid gap-3 sm:grid-cols-3">
        {permissionGroups.map((group) => (
          <div key={group.label}>
            <p className="text-2xs font-semibold uppercase tracking-[0.06em] text-dim">
              {group.label}
            </p>
            <div className="mt-1.5 space-y-1">
              {group.permissions.map(([value, label]) => (
                <label key={value} className="flex items-center gap-2 text-xs">
                  <input
                    type="checkbox"
                    checked={permissions.includes(value)}
                    onChange={() => {
                      toggle(value)
                    }}
                  />
                  {label}
                </label>
              ))}
            </div>
          </div>
        ))}
      </div>
      {error && (
        <p className="mt-3 rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
      )}
      <div className="mt-3 flex items-center gap-2">
        <Button
          type="submit"
          variant="primary"
          size="sm"
          disabled={busy || permissions.length === 0}
        >
          {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Check className="size-3.5" />}
          {role ? 'Save role' : 'Create role'}
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={onClose}>
          Cancel
        </Button>
      </div>
    </form>
  )
}
