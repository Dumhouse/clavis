import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Plus, Send, Trash2, Webhook } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type Hook = components['schemas']['WebhookBody']
type Delivery = components['schemas']['WebhookDeliveryBody']

export function WebhookSettings() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [events, setEvents] = useState('secret.*')
  const [kind, setKind] = useState('generic')
  const [protectedOnly, setProtectedOnly] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const hooks = useQuery({
    queryKey: ['webhooks', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/webhooks', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load webhooks.'))
      return result.data?.webhooks ?? []
    },
  })

  const deliveries = useQuery({
    queryKey: ['webhook-deliveries', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/webhooks/deliveries', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load deliveries.'))
      return result.data?.deliveries ?? []
    },
  })

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['webhooks', org?.slug] })
    await queryClient.invalidateQueries({ queryKey: ['webhook-deliveries', org?.slug] })
  }

  const create = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/webhooks', {
        params: { path: { org: org.slug } },
        body: {
          url: url.trim(),
          enabled: true,
          kind: kind as 'generic' | 'slack' | 'teams' | 'discord',
          protected_only: protectedOnly,
          secret,
          events: events
            .split(',')
            .map((entry) => entry.trim())
            .filter((entry) => entry.length > 0),
        },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not create the webhook.'))
    },
    onSuccess: async () => {
      setOpen(false)
      setUrl('')
      setSecret('')
      setProtectedOnly(false)
      setError(null)
      await invalidate()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not create the webhook.')
    },
  })

  const remove = useMutation({
    mutationFn: async (hook: Hook) => {
      if (!org) throw new Error('No organization.')
      if (!window.confirm(`Delete the webhook for ${hook.url}?`)) return
      const result = await api.DELETE('/api/v1/orgs/{org}/webhooks/{id}', {
        params: { path: { org: org.slug, id: hook.id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not delete the webhook.'))
    },
    onSuccess: invalidate,
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not delete the webhook.')
    },
  })

  function submit(event: FormEvent) {
    event.preventDefault()
    create.mutate()
  }

  return (
    <section className="rounded-app border border-border bg-surface">
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <Webhook className="size-4 text-muted" />
        <h2 className="text-[14px] font-semibold">Outgoing webhooks</h2>
        <Button
          variant="secondary"
          size="sm"
          className="ml-auto"
          onClick={() => {
            setOpen((value) => !value)
          }}
        >
          <Plus className="size-3.5" /> Add webhook
        </Button>
      </div>
      <p className="border-b border-border-soft px-4 py-2 text-[12px] text-dim">
        Signed with HMAC-SHA256 in <code className="font-mono">X-Clavis-Signature</code> and retried
        five times with backoff.
      </p>

      {open && (
        <form onSubmit={submit} className="space-y-3 border-b border-border p-4">
          <label className="block">
            <span className="mb-1 block text-[12.5px] font-medium text-muted">Endpoint URL</span>
            <input
              required
              className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
              placeholder="https://example.com/hooks/clavis"
              value={url}
              onChange={(event) => {
                setUrl(event.target.value)
              }}
            />
          </label>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">Type</span>
              <select
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
                value={kind}
                onChange={(event) => {
                  setKind(event.target.value)
                }}
              >
                <option value="generic">Generic JSON</option>
                <option value="slack">Slack</option>
                <option value="teams">Microsoft Teams</option>
                <option value="discord">Discord</option>
              </select>
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">
                Signing secret
              </span>
              <input
                type="password"
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
                value={secret}
                onChange={(event) => {
                  setSecret(event.target.value)
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">
                Events (comma separated)
              </span>
              <input
                className="h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 font-mono text-[13px] text-text focus:border-accent-line focus:outline-none"
                placeholder="secret.*, project.delete"
                value={events}
                onChange={(event) => {
                  setEvents(event.target.value)
                }}
              />
            </label>
          </div>
          <label className="flex items-center gap-2 text-[13px]">
            <input
              type="checkbox"
              checked={protectedOnly}
              onChange={(event) => {
                setProtectedOnly(event.target.checked)
              }}
            />
            Only notify for protected environments
          </label>
          {error && (
            <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2">
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
            <Button type="submit" variant="primary" size="sm" disabled={create.isPending}>
              {create.isPending && <Loader2 className="size-3.5 animate-spin" />} Create
            </Button>
          </div>
        </form>
      )}

      <div className="divide-y divide-border-soft">
        {(hooks.data ?? []).map((hook) => (
          <div key={hook.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="truncate font-mono text-[12.5px]">{hook.url}</p>
              <p className="text-[11.5px] text-dim">
                {hook.kind} · {(hook.events ?? []).join(', ') || 'All events'}
                {hook.protected_only ? ' · protected only' : ''} ·{' '}
                {hook.secret_set ? 'signed' : 'unsigned'}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Delete webhook"
              disabled={remove.isPending}
              onClick={() => {
                remove.mutate(hook)
              }}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        ))}
        {(hooks.data?.length ?? 0) === 0 && !hooks.isLoading && (
          <p className="px-4 py-5 text-[13px] text-muted">No webhooks configured.</p>
        )}
      </div>

      <div className="border-t border-border px-4 py-3">
        <div className="flex items-center gap-2">
          <Send className="size-3.5 text-muted" />
          <h3 className="text-[13px] font-semibold">Recent deliveries</h3>
        </div>
        <div className="mt-2 max-h-48 divide-y divide-border-soft overflow-y-auto">
          {(deliveries.data ?? []).slice(0, 20).map((delivery: Delivery) => (
            <div key={delivery.id} className="flex items-center gap-2 py-1.5 text-[11.5px]">
              <span
                className={
                  delivery.status === 'success'
                    ? 'rounded-full bg-success-bg px-1.5 py-0.5 font-semibold text-success'
                    : 'rounded-full bg-danger-bg px-1.5 py-0.5 font-semibold text-danger'
                }
              >
                {delivery.status}
              </span>
              <span className="font-mono text-muted">{delivery.event_type}</span>
              <span className="truncate text-dim">{delivery.url}</span>
              <span className="ml-auto text-dim">{delivery.attempts}×</span>
            </div>
          ))}
          {(deliveries.data?.length ?? 0) === 0 && (
            <p className="py-2 text-[12px] text-dim">No deliveries yet.</p>
          )}
        </div>
      </div>
    </section>
  )
}
