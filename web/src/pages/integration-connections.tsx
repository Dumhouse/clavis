import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Loader2, Lock, Plus, RotateCw, Stethoscope, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { confirmAction } from '@/components/ui/dialogs'
import { Field, Input, Notice, Select } from '@/components/ui/field'
import { Modal, ModalFooter, ModalHeader } from '@/components/ui/modal'
import { toast } from '@/components/ui/toast'
import { apiErrorMessage } from '@/lib/api-error'
import {
  connectionFields,
  defaultConnectionName,
  ProviderMark,
  providers,
  SpecField,
  supportsConnections,
  TokenHelp,
  type Provider,
} from '@/pages/integration-create'

type Connection = components['schemas']['ConnectionBody']

const connectable = providers.filter(supportsConnections)

const authLabels: Record<string, string> = {
  token: 'token',
  github_app: 'GitHub App',
  aws_keys: 'access keys',
  service_account: 'service account',
}

function usageText(connection: Connection) {
  const syncs = connection.syncs ?? []
  if (connection.sync_count === 0) return 'Not used by any sync'
  const names = syncs.map((usage) => `${usage.project}/${usage.environment}`)
  const label = connection.sync_count === 1 ? '1 sync' : `${String(connection.sync_count)} syncs`
  return names.length > 0 ? `Used by ${label}: ${names.join(', ')}` : `Used by ${label}`
}

/** The org's connections (no credentials). */
export function useConnections(orgSlug: string | undefined) {
  return useQuery({
    queryKey: ['connections', orgSlug],
    enabled: Boolean(orgSlug),
    queryFn: async () => {
      if (!orgSlug) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/connections', {
        params: { path: { org: orgSlug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load connections.'))
      return result.data?.connections ?? []
    },
  })
}

/** Org-wide provider credentials that syncs share. */
export function ConnectionsSection({ orgSlug }: { orgSlug: string }) {
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<Connection | 'new' | null>(null)
  const connections = useConnections(orgSlug)
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['connections', orgSlug] })

  const test = useMutation({
    mutationFn: async (connection: Connection) => {
      const result = await api.POST('/api/v1/orgs/{org}/connections/{id}/test', {
        params: { path: { org: orgSlug, id: connection.id } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not test it.'))
      return result.data
    },
    onSuccess: (data, connection) => {
      toast(`${connection.name}: ${data.detail}`, { tone: data.ok ? 'success' : 'error' })
    },
    onError: (cause: unknown) => {
      toast(cause instanceof Error ? cause.message : 'Could not test it.', { tone: 'error' })
    },
  })

  const remove = useMutation({
    mutationFn: async (connection: Connection) => {
      if (
        !(await confirmAction(
          `Delete connection “${connection.name}”? Its credential is erased from Clavis; the token itself stays valid at the provider.`,
        ))
      )
        return
      const result = await api.DELETE('/api/v1/orgs/{org}/connections/{id}', {
        params: { path: { org: orgSlug, id: connection.id } },
      })
      if (result.error) {
        // 409 connection_in_use lists the syncs as problem errors.
        const problem = result.error as { errors?: Array<{ detail?: string }> | null }
        const syncs = (problem.errors ?? []).map((entry) => entry.detail ?? '').filter(Boolean)
        const base = apiErrorMessage(result.error, 'Could not delete the connection.')
        throw new Error(syncs.length > 0 ? `${base} ${syncs.join(', ')}` : base)
      }
    },
    onSuccess: invalidate,
    onError: (cause: unknown) => {
      toast(cause instanceof Error ? cause.message : 'Could not delete the connection.', {
        tone: 'error',
      })
    },
  })

  const list = connections.data ?? []
  return (
    <section
      aria-labelledby="connections-heading"
      className="rounded-app border border-border bg-surface"
    >
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <h2 id="connections-heading" className="text-base font-semibold">
          Connections
        </h2>
        <span className="rounded-full bg-elev px-1.5 text-2xs text-muted">{list.length}</span>
        <span className="hidden text-2xs text-dim sm:inline">
          Credentials saved once for the organization and shared by syncs.
        </span>
        <Button
          variant="secondary"
          size="sm"
          className="ml-auto"
          onClick={() => {
            setEditing('new')
          }}
        >
          <Plus className="size-3.5" /> Add connection
        </Button>
      </div>
      <div className="divide-y divide-border-soft">
        {list.map((connection) => {
          const meta = providers.find((candidate) => candidate.id === connection.provider)
          const inUse = connection.sync_count > 0
          return (
            <div key={connection.id} className="flex flex-wrap items-center gap-2 px-4 py-3">
              {meta && <ProviderMark provider={meta} size="sm" />}
              <div className="min-w-0 flex-1">
                <p className="flex flex-wrap items-center gap-x-2 text-sm">
                  <span className="font-medium">{connection.name}</span>
                  <span className="font-mono text-2xs text-dim">{connection.base_url}</span>
                  <span className="inline-flex items-center gap-1 rounded-full bg-elev px-1.5 py-0.5 text-2xs text-muted">
                    <KeyRound className="size-3" />
                    {authLabels[connection.auth] ?? connection.auth}
                  </span>
                </p>
                <p className="truncate text-2xs text-dim" title={usageText(connection)}>
                  {usageText(connection)} · last used{' '}
                  {connection.last_used_at
                    ? new Date(connection.last_used_at).toLocaleString()
                    : 'never'}
                </p>
              </div>
              <span className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={test.isPending}
                  onClick={() => {
                    test.mutate(connection)
                  }}
                >
                  {test.isPending && test.variables.id === connection.id ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    <Stethoscope className="size-3.5" />
                  )}{' '}
                  Test
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setEditing(connection)
                  }}
                >
                  <RotateCw className="size-3.5" /> Rotate
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Delete connection ${connection.name}`}
                  title={inUse ? 'Delete or move the syncs that use it first' : 'Delete connection'}
                  disabled={inUse || remove.isPending}
                  onClick={() => {
                    remove.mutate(connection)
                  }}
                >
                  <Trash2 className="size-3.5" />
                </Button>
              </span>
            </div>
          )
        })}
        {connections.isLoading && (
          <p className="flex items-center gap-2 px-4 py-5 text-sm text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading connections…
          </p>
        )}
        {connections.error && <Notice className="m-4">{connections.error.message}</Notice>}
        {!connections.isLoading && !connections.error && list.length === 0 && (
          <p className="px-4 py-6 text-sm text-muted">
            No connections yet. Save a Forgejo, GitHub, or cloud credential once, then each new sync
            only needs the repository and environment.
          </p>
        )}
      </div>
      {editing && (
        <ConnectionDialog
          orgSlug={orgSlug}
          existing={editing === 'new' ? null : editing}
          onClose={() => {
            setEditing(null)
          }}
          onSaved={(saved, rotated) => {
            setEditing(null)
            toast(
              rotated
                ? `Rotated “${saved.name}”. ${usageText(saved)}.`
                : `Saved “${saved.name}”. Pick it when you add a sync.`,
            )
            void invalidate()
          }}
        />
      )}
    </section>
  )
}

/** Create a connection, or rename / move / rotate an existing one. */
export function ConnectionDialog({
  orgSlug,
  existing,
  onClose,
  onSaved,
}: {
  orgSlug: string
  existing: Connection | null
  onClose: () => void
  onSaved: (connection: Connection, rotated: boolean) => void
}) {
  const [providerID, setProviderID] = useState(existing?.provider ?? connectable[0]?.id ?? '')
  const provider: Provider | undefined = connectable.find((p) => p.id === providerID)
  const [name, setName] = useState(existing?.name ?? '')
  const [values, setValues] = useState<Record<string, string>>(() =>
    existing ? { base_url: existing.base_url } : {},
  )
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  if (!provider) return null
  const fields = connectionFields(provider)
  const credentialKeys = fields.filter((field) => field.key !== 'base_url')

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (!provider) return
    setBusy(true)
    setError(null)
    const body: Record<string, string> = {}
    for (const field of fields) {
      const value = values[field.key]?.trim()
      if (value) body[field.key] = value
    }
    const rotated =
      existing !== null && credentialKeys.some((field) => Boolean(values[field.key]?.trim()))
    const result = existing
      ? await api.PATCH('/api/v1/orgs/{org}/connections/{id}', {
          params: { path: { org: orgSlug, id: existing.id } },
          body: { ...body, ...(name.trim() ? { name: name.trim() } : {}) },
        })
      : await api.POST('/api/v1/orgs/{org}/connections', {
          params: { path: { org: orgSlug } },
          body: {
            ...body,
            provider: provider.id as components['schemas']['CreateConnectionInputBody']['provider'],
            name: name.trim() || defaultConnectionName(provider, values),
          },
        })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not save the connection.'))
      return
    }
    onSaved(result.data, rotated)
  }

  return (
    <Modal onClose={onClose} className="max-w-lg">
      <form
        onSubmit={(event) => {
          void submit(event)
        }}
      >
        <ModalHeader
          title={
            <span className="flex items-center gap-2.5">
              <ProviderMark provider={provider} size="sm" />
              {existing ? `Rotate “${existing.name}”` : 'Add a connection'}
            </span>
          }
          description={
            existing
              ? 'Leave the credential blank to keep it. A new credential reaches every sync that uses this connection on its next run.'
              : 'Saved once for the organization. Syncs pick it instead of asking for a token.'
          }
          onClose={onClose}
        />
        <div className="mt-4 space-y-3">
          {!existing && (
            <Field label="Provider">
              <Select
                value={providerID}
                onChange={(event) => {
                  setProviderID(event.target.value)
                  setValues({})
                }}
              >
                {connectable.map((candidate) => (
                  <option key={candidate.id} value={candidate.id}>
                    {candidate.name}
                  </option>
                ))}
              </Select>
            </Field>
          )}
          <Field label="Name">
            <Input
              value={name}
              maxLength={100}
              placeholder={defaultConnectionName(provider, values)}
              onChange={(event) => {
                setName(event.target.value)
              }}
            />
          </Field>
          {!existing && <TokenHelp provider={provider} link={provider.tokenUrl?.(values)} />}
          {fields.map((field) => (
            <SpecField
              key={field.key}
              field={field}
              value={values[field.key] ?? ''}
              required={existing ? false : field.required}
              placeholder={existing && field.secret ? 'Unchanged' : field.placeholder}
              {...(existing && field.secret ? { labelSuffix: '' } : {})}
              onChange={(value) => {
                setValues((state) => ({ ...state, [field.key]: value }))
              }}
            />
          ))}
          <p className="flex items-center gap-1.5 text-2xs text-dim">
            <Lock className="size-3" /> Encrypted with the organization key and never shown again.
          </p>
          {error && <Notice>{error}</Notice>}
        </div>
        <ModalFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" size="sm" disabled={busy}>
            {busy && <Loader2 className="size-3.5 animate-spin" />}{' '}
            {existing ? 'Save' : 'Save connection'}
          </Button>
        </ModalFooter>
      </form>
    </Modal>
  )
}
