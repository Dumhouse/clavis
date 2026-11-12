import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, Search, ShieldAlert, ShieldCheck } from 'lucide-react'
import { useMemo, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { useOrgEvents } from '@/lib/events'
import { cn } from '@/lib/utils'

type AuditEvent = components['schemas']['AuditEventBody']

export function AuditPage() {
  const { org } = useCurrentOrg()
  const [filter, setFilter] = useState('')
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
    if (!needle) return [...list].reverse()
    return list
      .filter((event) =>
        [event.action, event.actor.name, event.actor.id, event.resource.type, event.resource.name]
          .filter(Boolean)
          .some((value) => value?.toLowerCase().includes(needle)),
      )
      .reverse()
  }, [events.data, filter])

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="font-head text-[24px] font-semibold tracking-tight">Audit log</h1>
          <p className="mt-1 text-[13px] text-muted">
            Append-only and hash-chained per organization. Secret values never appear here.
          </p>
        </div>
        {events.data && (
          <span
            className={cn(
              'inline-flex h-7 items-center gap-1.5 rounded-full border px-2.5 text-[11px] font-medium',
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
        <p className="rounded-app bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {verification.data.problem ?? 'Audit verification failed.'}
        </p>
      )}
      {signCheckpoint.error && (
        <p className="rounded-app bg-danger-bg px-3 py-2 text-[12px] text-danger">
          {signCheckpoint.error instanceof Error
            ? signCheckpoint.error.message
            : 'Could not sign a checkpoint.'}
        </p>
      )}

      <div className="flex h-8 max-w-md items-center gap-2 rounded-app-sm border border-border bg-surface px-2.5">
        <Search className="size-3.5 text-dim" />
        <input
          className="w-full bg-transparent text-[12px] text-text placeholder:text-dim focus:outline-none"
          placeholder="Filter by actor, action, or resource…"
          value={filter}
          onChange={(event) => {
            setFilter(event.target.value)
          }}
        />
      </div>

      {events.isLoading && (
        <div className="flex items-center gap-2 rounded-app border border-border bg-surface px-4 py-6 text-[12px] text-muted">
          <Loader2 className="size-4 animate-spin" /> Loading audit events…
        </div>
      )}
      {events.isError && (
        <div className="rounded-app bg-danger-bg px-4 py-3 text-[12px] text-danger">
          {events.error instanceof Error ? events.error.message : 'Could not load the audit log.'}
        </div>
      )}

      <div className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[720px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-[10.5px] uppercase tracking-[0.06em] text-dim">
              <th className="px-4 py-2.5 font-semibold">Seq</th>
              <th className="px-4 py-2.5 font-semibold">Time</th>
              <th className="px-4 py-2.5 font-semibold">Actor</th>
              <th className="px-4 py-2.5 font-semibold">Action</th>
              <th className="px-4 py-2.5 font-semibold">Resource</th>
              <th className="px-4 py-2.5 font-semibold">Details</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((event) => (
              <tr key={event.id} className="border-b border-border-soft align-top last:border-0">
                <td className="px-4 py-2 font-mono text-[10.5px] text-dim">#{event.seq}</td>
                <td className="whitespace-nowrap px-4 py-2 text-[11.5px] text-muted">
                  {formatTime(event.created_at)}
                </td>
                <td className="px-4 py-2 text-[11.5px]">
                  <span className="text-text">
                    {event.actor.name || event.actor.id.slice(0, 8)}
                  </span>
                  <span className="ml-1 text-[10.5px] text-dim">{event.actor.kind}</span>
                </td>
                <td className="px-4 py-2">
                  <ActionBadge action={event.action} />
                </td>
                <td className="px-4 py-2 text-[11.5px]">
                  <span className="text-muted">{event.resource.type}</span>
                  {event.resource.name && (
                    <span className="ml-1 text-text">{event.resource.name}</span>
                  )}
                </td>
                <td className="px-4 py-2 text-[11px] text-dim">{details(event)}</td>
              </tr>
            ))}
            {visible.length === 0 && !events.isLoading && (
              <tr>
                <td colSpan={6} className="px-4 py-10 text-center text-[12px] text-muted">
                  No audit events match this filter.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
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
    <span
      className={cn('inline-flex h-5 items-center rounded px-1.5 font-mono text-[10.5px]', tone)}
    >
      {action}
    </span>
  )
}

function details(event: AuditEvent): string {
  const parts: string[] = []
  if (event.details.environment) parts.push(event.details.environment)
  if (event.details.version) parts.push(`v${String(event.details.version)}`)
  if (event.details.count) parts.push(`${String(event.details.count)} items`)
  if (event.details.reason) parts.push(`“${event.details.reason}”`)
  if (event.details.key_blind_index) parts.push(`key ${event.details.key_blind_index.slice(0, 8)}…`)
  return parts.join(' · ') || '—'
}

function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}
