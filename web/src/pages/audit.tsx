import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Search, ShieldAlert, ShieldCheck, X } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Notice } from '@/components/ui/field'
import { Modal } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { useOrgEvents } from '@/lib/events'
import { useShortcuts } from '@/lib/use-shortcuts'
import { cn } from '@/lib/utils'
import { formatDateTime } from '@/lib/format'

type AuditEvent = components['schemas']['AuditEventBody']

export function AuditPage() {
  const { org } = useCurrentOrg()
  const [filter, setFilter] = useState('')
  const filterRef = useRef<HTMLInputElement>(null)
  useShortcuts({
    '/': () => {
      filterRef.current?.focus()
    },
  })
  const [category, setCategory] = useState<Category>('all')
  const [selected, setSelected] = useState<AuditEvent | null>(null)
  const queryClient = useQueryClient()

  useOrgEvents(org?.slug, () => {
    void queryClient.invalidateQueries({ queryKey: ['audit', org?.slug] })
  })

  const verification = useQuery({
    queryKey: ['audit', 'verify', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/audit-events/verify', {
        params: { path: { org: org.slug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not verify the audit chain.'))
      return result.data
    },
  })

  const signCheckpoint = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/audit-events/checkpoint', {
        params: { path: { org: org.slug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not sign a checkpoint.'))
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['audit', org?.slug] })
      await queryClient.invalidateQueries({ queryKey: ['audit', 'verify', org?.slug] })
    },
  })

  const events = useQuery({
    queryKey: ['audit', org?.slug],
    enabled: Boolean(org),
    refetchInterval: 30_000,
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/audit-events', {
        params: { path: { org: org.slug }, query: { limit: 200 } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load the audit log.'))
      return result.data
    },
  })

  const visible = useMemo(() => {
    const list = events.data?.events ?? []
    const needle = filter.trim().toLowerCase()
    return list
      .filter((event) => category === 'all' || categoryOf(event.action) === category)
      .filter(
        (event) =>
          !needle ||
          [
            event.action,
            event.actor.name,
            event.actor.id,
            event.resource.type,
            event.resource.name,
            event.resource.key,
            event.resource.project,
            event.resource.environment,
            event.details.reason,
            event.ip,
          ]
            .filter(Boolean)
            .some((value) => value?.toLowerCase().includes(needle)),
      )
      .reverse()
  }, [events.data, filter, category])

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="font-head text-2xl font-semibold tracking-tight">Audit log</h1>
          <p className="mt-1 text-base text-muted">
            Append-only and hash-chained per organization. Secret values never appear here.
          </p>
        </div>
        {events.data && (
          <span
            className={cn(
              'inline-flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-xs font-medium',
              events.data.verified
                ? 'border-accent-line bg-accent-soft text-accent-text'
                : 'border-danger/40 bg-danger-bg text-danger',
            )}
          >
            {events.data.verified ? (
              <ShieldCheck className="size-3.5" />
            ) : (
              <ShieldAlert className="size-3.5" />
            )}
            {events.data.verified ? 'Chain verified' : 'Chain broken'}
          </span>
        )}
      </header>

      {verification.data && !verification.data.chain_valid && (
        <Notice>{verification.data.problem ?? 'Audit verification failed.'}</Notice>
      )}
      {signCheckpoint.error && (
        <Notice>
          {signCheckpoint.error instanceof Error
            ? signCheckpoint.error.message
            : 'Could not sign a checkpoint.'}
        </Notice>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <label className="flex h-8 min-w-0 flex-1 items-center gap-2 rounded-app-sm border border-border bg-surface px-2.5 focus-within:border-accent-line sm:max-w-md">
          <Search className="size-3.5 text-dim" aria-hidden />
          <span className="sr-only">Filter events</span>
          <input
            ref={filterRef}
            className="w-full bg-transparent text-sm text-text placeholder:text-dim focus:outline-none"
            placeholder="Key, person, project, reason, IP…"
            value={filter}
            onChange={(event) => {
              setFilter(event.target.value)
            }}
          />
        </label>
        <div role="group" aria-label="Event type" className="flex flex-wrap gap-1">
          {categories.map(([value, label]) => (
            <button
              key={value}
              type="button"
              aria-pressed={category === value}
              onClick={() => {
                setCategory(value)
              }}
              className={cn(
                'h-8 rounded-app-sm px-2.5 text-xs font-medium',
                category === value
                  ? 'bg-accent-soft text-accent-text'
                  : 'text-muted hover:bg-hover hover:text-text',
              )}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {events.isLoading && (
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-sm text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading audit events…
        </div>
      )}
      {events.isError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-sm text-danger">
          {events.error instanceof Error ? events.error.message : 'Could not load the audit log.'}
        </div>
      )}

      <div className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[720px] border-collapse text-left">
          <caption className="sr-only">Audit events, newest first</caption>
          <thead>
            <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
              <th scope="col" className="px-4 py-2.5 font-semibold">
                Time
              </th>
              <th scope="col" className="px-4 py-2.5 font-semibold">
                Who
              </th>
              <th scope="col" className="px-4 py-2.5 font-semibold">
                Action
              </th>
              <th scope="col" className="px-4 py-2.5 font-semibold">
                What
              </th>
              <th scope="col" className="px-4 py-2.5 font-semibold">
                Why
              </th>
            </tr>
          </thead>
          <tbody>
            {visible.map((event) => (
              <tr
                key={event.id}
                tabIndex={0}
                aria-label={`${event.action} by ${event.actor.name || event.actor.kind}, open details`}
                onClick={() => {
                  setSelected(event)
                }}
                onKeyDown={(keyEvent) => {
                  if (keyEvent.key === 'Enter') setSelected(event)
                }}
                className="cursor-pointer border-b border-border-soft align-top last:border-0 hover:bg-hover-soft focus:bg-hover focus:outline-none"
              >
                <td className="whitespace-nowrap px-4 py-2 text-xs text-muted">
                  <time dateTime={event.created_at}>{formatTime(event.created_at)}</time>
                </td>
                <td className="px-4 py-2 text-xs">
                  <span className="text-text">
                    {event.actor.name || event.actor.id.slice(0, 8)}
                  </span>
                  {event.actor.kind !== 'user' && (
                    <span className="ml-1 text-2xs text-dim">{event.actor.kind}</span>
                  )}
                </td>
                <td className="px-4 py-2">
                  <ActionBadge action={event.action} />
                </td>
                <td className="px-4 py-2 text-xs">
                  <Target event={event} />
                </td>
                <td className="max-w-64 truncate px-4 py-2 text-xs text-muted">
                  {event.details.reason ? (
                    `“${event.details.reason}”`
                  ) : (
                    <span className="text-dim">—</span>
                  )}
                </td>
              </tr>
            ))}
            {visible.length === 0 && !events.isLoading && (
              <tr>
                <td colSpan={5} className="px-4 py-10 text-center text-sm text-muted">
                  No audit events match this filter.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {selected && (
        <EventDetail
          event={selected}
          onClose={() => {
            setSelected(null)
          }}
        />
      )}
    </div>
  )
}

type Category = 'all' | 'secrets' | 'reveals' | 'access' | 'auth' | 'settings'

const categories: Array<[Category, string]> = [
  ['all', 'All'],
  ['reveals', 'Reveals'],
  ['secrets', 'Changes'],
  ['access', 'Access'],
  ['auth', 'Sign-ins'],
  ['settings', 'Settings'],
]

function categoryOf(action: string): Category {
  if (action.includes('reveal') || action.endsWith('.read')) return 'reveals'
  if (
    /^(secret|secrets|promotion|rotation|sync|share|lease|dynamic|env|overlay|folder|set|schema)/.test(
      action,
    )
  )
    return 'secrets'
  if (/^(auth|session|passkey|mfa|login|sso|saml)/.test(action)) return 'auth'
  if (/^(access|member|invite|role|group|identity|agent|token|binding|break)/.test(action))
    return 'access'
  return 'settings'
}

/** Human description of what an event touched: key in project/env when known. */
function Target({ event }: { event: AuditEvent }) {
  const { resource, details } = event
  const env = resource.environment || details.environment
  if (resource.type === 'secret') {
    return (
      <span>
        {resource.key ? (
          <span className="font-mono text-text">{resource.key}</span>
        ) : (
          <span className="text-dim" title="You cannot read key names in this scope">
            hidden key
          </span>
        )}
        {(resource.project || env) && (
          <span className="ml-1.5 text-dim">
            {resource.project}
            {resource.project && env ? ' / ' : ''}
            {env}
          </span>
        )}
        {details.version ? <span className="ml-1 text-dim">v{details.version}</span> : null}
      </span>
    )
  }
  return (
    <span>
      <span className="text-muted">{resource.type}</span>
      {resource.name && <span className="ml-1 text-text">{resource.name}</span>}
      {resource.project && <span className="ml-1.5 text-dim">{resource.project}</span>}
      {env && <span className="ml-1 text-dim">/ {env}</span>}
      {details.count ? <span className="ml-1 text-dim">· {details.count} items</span> : null}
    </span>
  )
}

function EventDetail({ event, onClose }: { event: AuditEvent; onClose: () => void }) {
  const rows: Array<[string, string | undefined]> = [
    ['Sequence', `#${String(event.seq)}`],
    ['Time', formatDateTime(event.created_at)],
    ['Actor', `${event.actor.name || event.actor.id} (${event.actor.kind})`],
    ['Action', event.action],
    ['Resource', event.resource.type],
    ['Key', event.resource.key],
    ['Name', event.resource.name],
    ['Project', event.resource.project],
    ['Environment', event.resource.environment || event.details.environment],
    ['Version', event.details.version ? `v${String(event.details.version)}` : undefined],
    ['Reason', event.details.reason],
    ['Channel', event.details.channel],
    ['Command', event.details.command],
    ['Count', event.details.count ? String(event.details.count) : undefined],
    ['IP address', event.ip],
    ['User agent', event.user_agent],
    ['Request ID', event.details.request_id],
    ['Key blind index', event.details.key_blind_index],
  ]
  return (
    <Modal onClose={onClose} variant="sheet" className="max-w-md">
      <div className="flex items-start justify-between gap-3 border-b border-border px-5 py-4">
        <div>
          <h2 className="font-head text-md font-semibold">Audit event</h2>
          <p className="mt-0.5 font-mono text-2xs text-dim">{event.id}</p>
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
          <X className="size-4" />
        </Button>
      </div>
      <dl className="flex-1 divide-y divide-border-soft overflow-y-auto px-5">
        {rows
          .filter((row): row is [string, string] => Boolean(row[1]))
          .map(([label, value]) => (
            <div key={label} className="grid grid-cols-[120px_1fr] gap-3 py-2 text-xs">
              <dt className="text-dim">{label}</dt>
              <dd className="break-all text-text">{value}</dd>
            </div>
          ))}
      </dl>
      <p className="border-t border-border px-5 py-3 text-2xs text-dim">
        Events are hash-chained; values are never recorded.
      </p>
    </Modal>
  )
}

function ActionBadge({ action }: { action: string }) {
  const tone = action.includes('delete')
    ? 'text-danger bg-danger-bg'
    : action.includes('reveal')
      ? 'text-amber-fg bg-amber-bg'
      : action.includes('create')
        ? 'text-success bg-success-bg'
        : 'text-muted bg-elev'
  return (
    <span className={cn('inline-flex h-5 items-center rounded px-1.5 font-mono text-2xs', tone)}>
      {action}
    </span>
  )
}

function formatTime(value: string): string {
  return formatDateTime(value, value, true)
}
