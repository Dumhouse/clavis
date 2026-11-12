import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from '@tanstack/react-router'
import { ArrowLeft, Check, Loader2, Send, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Cmd, PageIntro } from '@/components/page-intro'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'
import { Input, Select } from '@/components/ui/field'

type Env = components['schemas']['EnvBody']
type Plan = components['schemas']['PromotionPlanOutputBody']
type Entry = components['schemas']['PromotionEntryBody']
type Request = components['schemas']['PromotionRequestBody']

const envOrder: Record<string, number> = { development: 0, preview: 1, staging: 2, production: 3 }

export function PromotePage() {
  const { org } = useCurrentOrg()
  const params = useParams({ strict: false }) as { project?: string }
  const projectSlug = params.project ?? ''
  const queryClient = useQueryClient()
  const [source, setSource] = useState('')
  const [target, setTarget] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [snapshot, setSnapshot] = useState(true)
  const [message, setMessage] = useState('')
  const [notice, setNotice] = useState<string | null>(null)

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

  const envs = useMemo(
    () =>
      [...(project.data?.environments ?? [])].sort(
        (a: Env, b: Env) => (envOrder[a.type] ?? 9) - (envOrder[b.type] ?? 9),
      ),
    [project.data],
  )
  useEffect(() => {
    if (envs.length < 2) return
    if (!source) setSource(envs[0]?.slug ?? '')
    if (!target) setTarget(envs[envs.length - 1]?.slug ?? '')
  }, [envs, source, target])

  const plan = useQuery({
    queryKey: ['promotion-plan', org?.slug, projectSlug, source, target],
    enabled: Boolean(org && source && target && source !== target),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/promotions:plan', {
        params: { path: { org: org.slug, project: projectSlug } },
        body: { source, target },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not plan the promotion.'))
      return result.data as Plan
    },
  })

  useEffect(() => {
    const next = new Set<string>()
    for (const entry of plan.data?.entries ?? []) {
      if (entry.status === 'added' || entry.status === 'changed') next.add(entry.key)
    }
    setSelected(next)
  }, [plan.data])

  const selectedEntries = (plan.data?.entries ?? []).filter((entry) => selected.has(entry.key))
  const schemaErrors = selectedEntries.filter((entry) => entry.schema_error)
  const canAdmin = org?.role === 'Owner' || org?.role === 'Admin'
  const approvalRequired = Boolean(plan.data?.approval_required)

  const invalidate = async () => {
    await queryClient.invalidateQueries({
      queryKey: ['promotion-plan', org?.slug, projectSlug, source, target],
    })
    await queryClient.invalidateQueries({
      queryKey: ['promotion-requests', org?.slug, projectSlug],
    })
    await queryClient.invalidateQueries({ queryKey: ['secrets', org?.slug, projectSlug] })
  }

  const apply = useMutation({
    mutationFn: async (request: boolean) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/promotions', {
        params: { path: { org: org.slug, project: projectSlug } },
        body: {
          source,
          target,
          keys: [...selected],
          snapshot,
          message,
          request,
        },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not promote.'))
      return result.data
    },
    onSuccess: async (data, request) => {
      setNotice(
        request
          ? 'Promotion requested. An Admin or Owner can approve it below.'
          : `Promoted ${String(data?.applied ?? 0)} keys${data?.snapshot_id ? ' and captured a snapshot' : ''}.`,
      )
      await invalidate()
    },
  })

  const requests = useQuery({
    queryKey: ['promotion-requests', org?.slug, projectSlug],
    enabled: Boolean(org && projectSlug),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}/promotion-requests', {
        params: { path: { org: org.slug, project: projectSlug }, query: { status: 'pending' } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load requests.'))
      return result.data?.requests ?? []
    },
  })

  const approve = useMutation({
    mutationFn: async (request: Request) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/promotion-requests/{id}/approve',
        { params: { path: { org: org.slug, project: projectSlug, id: request.id } } },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not approve.'))
      return result.data
    },
    onSuccess: async (data) => {
      setNotice(`Approved and promoted ${String(data?.applied ?? 0)} keys.`)
      await invalidate()
      await queryClient.invalidateQueries({
        queryKey: ['promotion-requests', org?.slug, projectSlug],
      })
    },
  })

  const reject = useMutation({
    mutationFn: async (request: Request) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST(
        '/api/v1/orgs/{org}/projects/{project}/promotion-requests/{id}/reject',
        { params: { path: { org: org.slug, project: projectSlug, id: request.id } } },
      )
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not reject.'))
    },
    onSuccess: async () => {
      setNotice('Promotion request rejected.')
      await queryClient.invalidateQueries({
        queryKey: ['promotion-requests', org?.slug, projectSlug],
      })
    },
  })

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/projects/$project"
          params={{ project: projectSlug }}
          className="inline-flex items-center gap-1.5 text-sm text-muted hover:text-text"
        >
          <ArrowLeft className="size-3.5" /> Secrets
        </Link>
        <h1 className="mt-2 font-head text-2xl font-semibold tracking-tight">Promote</h1>
        <p className="mt-1 text-base text-muted">
          Review added, changed, missing, and unchanged keys. Selected keys are schema-checked,
          snapshotted, and copied to the target.
        </p>
      </div>

      <PageIntro
        id="promote"
        summary={
          <>
            Promote copies values from one environment to the next, for example staging to
            production, so you ship exactly the values you tested instead of retyping them.
          </>
        }
        steps={[
          {
            title: 'Pick From and To',
            body: 'Usually the environment you tested in, and the one you are releasing to.',
          },
          {
            title: 'Review the differences',
            body: 'Added keys are new to the target and Changed keys have a different value. Keys that exist only in the target are left alone. Tick what to copy.',
          },
          {
            title: 'Promote',
            body: 'Values are checked against the schema and a snapshot of the target is saved first, so you can undo. Protected targets wait for an Admin to approve.',
          },
        ]}
        when="a new key or changed value worked in staging and should go live."
        footer={
          <p className="text-xs text-muted">
            Terminal: <Cmd>clavis env diff staging production</Cmd>, then{' '}
            <Cmd>clavis promote --from staging --to production</Cmd>.
          </p>
        }
      />

      <section className="flex flex-wrap items-end gap-3 rounded-app border border-border bg-surface px-4 py-3">
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">From</span>
          <Select
            className="h-9"
            value={source}
            onChange={(event) => {
              setSource(event.target.value)
            }}
          >
            {envs.map((env) => (
              <option key={env.id} value={env.slug}>
                {env.name} ({env.slug})
              </option>
            ))}
          </Select>
        </label>
        <span className="pb-2 text-dim">→</span>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">To</span>
          <Select
            className="h-9"
            value={target}
            onChange={(event) => {
              setTarget(event.target.value)
            }}
          >
            {envs.map((env) => (
              <option key={env.id} value={env.slug}>
                {env.name} ({env.slug})
              </option>
            ))}
          </Select>
        </label>
        <label className="block min-w-48 flex-1">
          <span className="mb-1 block text-xs font-medium text-muted">Message</span>
          <Input
            className="h-9"
            placeholder="Release 2026.11"
            value={message}
            onChange={(event) => {
              setMessage(event.target.value)
            }}
          />
        </label>
        <label className="flex items-center gap-1.5 pb-2 text-xs text-muted">
          <input
            type="checkbox"
            checked={snapshot}
            onChange={(event) => {
              setSnapshot(event.target.checked)
            }}
          />
          Snapshot target first
        </label>
      </section>

      {notice && (
        <p className="rounded-app border border-accent-line bg-accent-soft px-4 py-2.5 text-sm text-accent-text">
          {notice}
        </p>
      )}
      {(plan.error || apply.error || approve.error || reject.error) && (
        <p className="rounded-app bg-danger-bg px-4 py-3 text-sm text-danger">
          {plan.error instanceof Error
            ? plan.error.message
            : apply.error instanceof Error
              ? apply.error.message
              : approve.error instanceof Error
                ? approve.error.message
                : reject.error instanceof Error
                  ? reject.error.message
                  : null}
        </p>
      )}

      <div className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[760px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-2xs uppercase tracking-[0.06em] text-dim">
              <th className="w-10 px-3 py-2.5" />
              <th className="px-3 py-2.5 font-semibold">Key</th>
              <th className="px-3 py-2.5 font-semibold">Status</th>
              <th className="px-3 py-2.5 font-semibold">Kind</th>
              <th className="px-3 py-2.5 font-semibold">Schema</th>
            </tr>
          </thead>
          <tbody>
            {(plan.data?.entries ?? []).map((entry) => (
              <tr key={entry.key} className="border-b border-border-soft last:border-0">
                <td className="px-3 py-2">
                  <input
                    type="checkbox"
                    checked={selected.has(entry.key)}
                    disabled={entry.status === 'missing'}
                    onChange={(event) => {
                      setSelected((current) => {
                        const next = new Set(current)
                        if (event.target.checked) next.add(entry.key)
                        else next.delete(entry.key)
                        return next
                      })
                    }}
                  />
                </td>
                <td className="px-3 py-2 font-mono text-xs text-text">{entry.key}</td>
                <td className="px-3 py-2">
                  <StatusBadge status={entry.status} />
                </td>
                <td className="px-3 py-2 text-xs text-muted">{entry.kind || '—'}</td>
                <td className="px-3 py-2 text-xs text-danger">{entry.schema_error || '—'}</td>
              </tr>
            ))}
            {plan.isLoading && (
              <tr>
                <td colSpan={5} className="px-3 py-8 text-center text-sm text-muted">
                  <Loader2 className="mr-2 inline size-3.5 animate-spin" /> Comparing…
                </td>
              </tr>
            )}
            {!plan.isLoading && (plan.data?.entries ?? []).length === 0 && (
              <tr>
                <td colSpan={5} className="px-3 py-8 text-center text-sm text-muted">
                  Both environments have the same keys.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted">
          {selected.size} selected · {schemaErrors.length} schema issue(s)
          {approvalRequired ? ' · protected target needs approval' : ''}
        </span>
        <div className="ml-auto flex gap-2">
          {approvalRequired && (
            <Button
              variant="secondary"
              size="sm"
              disabled={selected.size === 0 || apply.isPending}
              onClick={() => {
                apply.mutate(true)
              }}
            >
              <Send className="size-3.5" /> Request approval
            </Button>
          )}
          {!approvalRequired && (
            <Button
              variant="primary"
              size="sm"
              disabled={selected.size === 0 || schemaErrors.length > 0 || apply.isPending}
              onClick={() => {
                apply.mutate(false)
              }}
            >
              {apply.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Check className="size-3.5" />
              )}
              Promote {selected.size} keys
            </Button>
          )}
        </div>
      </div>

      <section className="rounded-app border border-border bg-surface">
        <div className="border-b border-border px-4 py-2.5">
          <h2 className="text-base font-semibold">Pending approvals</h2>
          <p className="text-xs text-dim">
            Protected-environment promotions waiting for an Admin or Owner.
          </p>
        </div>
        <div className="divide-y divide-border-soft">
          {(requests.data ?? []).map((request) => (
            <div key={request.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">
                  {request.source} → {request.target}
                </p>
                <p className="text-xs text-dim">
                  {(request.keys ?? []).length} keys · requested by {request.requested_by} ·{' '}
                  {new Date(request.requested_at).toLocaleString()}
                </p>
              </div>
              {canAdmin && (
                <>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={approve.isPending}
                    onClick={() => {
                      approve.mutate(request)
                    }}
                  >
                    <Check className="size-3.5" /> Approve
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={reject.isPending}
                    onClick={() => {
                      reject.mutate(request)
                    }}
                  >
                    <X className="size-3.5" /> Reject
                  </Button>
                </>
              )}
            </div>
          ))}
          {(requests.data ?? []).length === 0 && (
            <p className="px-4 py-8 text-center text-sm text-muted">
              No pending promotion requests.
            </p>
          )}
        </div>
      </section>
    </div>
  )
}

function StatusBadge({ status }: { status: Entry['status'] }) {
  return (
    <span
      className={cn(
        'rounded px-1.5 py-0.5 text-2xs font-medium',
        status === 'added' && 'bg-success-bg text-success',
        status === 'changed' && 'bg-amber-bg text-amber-fg',
        status === 'missing' && 'bg-danger-bg text-danger',
        status === 'unchanged' && 'bg-elev text-dim',
      )}
    >
      {status}
    </span>
  )
}
