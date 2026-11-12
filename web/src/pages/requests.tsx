import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Loader2, Send, X } from 'lucide-react'
import { useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type Project = components['schemas']['ProjectBody']
type Request = components['schemas']['AccessRequestBody']

export function RequestsPage() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [projectSlug, setProjectSlug] = useState('')
  const [envSlug, setEnvSlug] = useState('')
  const [role, setRole] = useState('Developer')
  const [reason, setReason] = useState('')
  const [hours, setHours] = useState(1)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const projects = useQuery({
    queryKey: ['projects', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) return []
      const result = await api.GET('/api/v1/orgs/{org}/projects', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load projects.'))
      return result.data?.projects ?? []
    },
  })
  const selectedProject = projectSlug || projects.data?.[0]?.slug || ''
  const project = useQuery({
    queryKey: ['project', org?.slug, selectedProject],
    enabled: Boolean(org && selectedProject),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}', {
        params: { path: { org: org.slug, project: selectedProject } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load the project.'))
      return result.data
    },
  })
  const envs = project.data?.environments ?? []
  const selectedEnv = envSlug || envs[0]?.slug || ''

  const requests = useQuery({
    queryKey: ['access-requests', org?.slug, selectedProject],
    enabled: Boolean(org && selectedProject),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/projects/{project}/access-requests', {
        params: { path: { org: org.slug, project: selectedProject }, query: { status: 'pending' } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load requests.'))
      return result.data?.requests ?? []
    },
  })

  const refresh = async () => {
    await queryClient.invalidateQueries({
      queryKey: ['access-requests', org?.slug, selectedProject],
    })
    await queryClient.invalidateQueries({ queryKey: ['access', org?.slug, selectedProject] })
  }

  const create = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/projects/{project}/access-requests', {
        params: { path: { org: org.slug, project: selectedProject } },
        body: {
          environment: selectedEnv,
          role,
          reason,
          duration_seconds: Math.max(300, Math.round(hours * 3600)),
          required_approvals: 1,
        },
      })
      if (result.error || !result.data)
        throw new Error(apiErrorMessage(result.error, 'Could not create the request.'))
      return result.data
    },
    onSuccess: async () => {
      setReason('')
      setMessage('Access request submitted.')
      setError(null)
      await refresh()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not create the request.')
    },
  })

  const decide = useMutation({
    mutationFn: async ({ request, approve }: { request: Request; approve: boolean }) => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST(
        `/api/v1/orgs/{org}/projects/{project}/access-requests/{id}/${approve ? 'approve' : 'reject'}` as '/api/v1/orgs/{org}/projects/{project}/access-requests/{id}/approve',
        {
          params: { path: { org: org.slug, project: selectedProject, id: request.id } },
        },
      )
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not update the request.'))
    },
    onSuccess: async () => {
      setMessage('Request updated.')
      setError(null)
      await refresh()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not update the request.')
    },
  })

  return (
    <div className="space-y-5">
      <header>
        <h1 className="font-head text-[25px] font-semibold tracking-tight">Requests</h1>
        <p className="mt-1 text-[14px] text-muted">
          Just-in-time access requests. Approvals create a time-bound project grant; requesters
          cannot approve themselves.
        </p>
      </header>

      {message && (
        <p className="rounded-app border border-accent-line bg-accent-soft px-4 py-2.5 text-[13px] text-accent-text">
          {message}
        </p>
      )}
      {error && (
        <p className="rounded-app bg-danger-bg px-4 py-3 text-[13px] text-danger">{error}</p>
      )}

      <section className="flex flex-wrap items-end gap-3 rounded-app border border-border bg-surface px-4 py-3">
        <label className="block">
          <span className="mb-1 block text-[12.5px] font-medium text-muted">Project</span>
          <select
            className="h-9 min-w-40 rounded-app-sm border border-border bg-bg px-2 text-[13px]"
            value={selectedProject}
            onChange={(e) => {
              setProjectSlug(e.target.value)
              setEnvSlug('')
            }}
          >
            {(projects.data ?? []).map((candidate: Project) => (
              <option key={candidate.id} value={candidate.slug}>
                {candidate.name}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="mb-1 block text-[12.5px] font-medium text-muted">Environment</span>
          <select
            className="h-9 rounded-app-sm border border-border bg-bg px-2 text-[13px]"
            value={selectedEnv}
            onChange={(e) => {
              setEnvSlug(e.target.value)
            }}
          >
            {envs.map((env) => (
              <option key={env.id} value={env.slug}>
                {env.name}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <span className="mb-1 block text-[12.5px] font-medium text-muted">Role</span>
          <select
            className="h-9 rounded-app-sm border border-border bg-bg px-2 text-[13px]"
            value={role}
            onChange={(e) => {
              setRole(e.target.value)
            }}
          >
            <option>Developer</option>
            <option>Viewer</option>
            <option>Admin</option>
          </select>
        </label>
        <label className="block">
          <span className="mb-1 block text-[12.5px] font-medium text-muted">Hours</span>
          <input
            className="h-9 w-24 rounded-app-sm border border-border bg-bg px-2 text-[13px]"
            type="number"
            min={1}
            max={24}
            value={hours}
            onChange={(e) => {
              setHours(Number(e.target.value))
            }}
          />
        </label>
        <label className="block min-w-56 flex-1">
          <span className="mb-1 block text-[12.5px] font-medium text-muted">Reason</span>
          <input
            className="h-9 w-full rounded-app-sm border border-border bg-bg px-2 text-[13px]"
            value={reason}
            onChange={(e) => {
              setReason(e.target.value)
            }}
            placeholder="Debugging the checkout incident"
          />
        </label>
        <Button
          variant="primary"
          size="sm"
          disabled={!reason.trim() || create.isPending}
          onClick={() => {
            create.mutate()
          }}
        >
          {create.isPending ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <Send className="size-3.5" />
          )}{' '}
          Request access
        </Button>
      </section>

      <section className="overflow-x-auto rounded-app border border-border bg-surface">
        <table className="w-full min-w-[760px] border-collapse text-left">
          <thead>
            <tr className="border-b border-border text-[11.5px] uppercase tracking-[0.06em] text-dim">
              <th className="px-4 py-2.5 font-semibold">Requester</th>
              <th className="px-4 py-2.5 font-semibold">Environment</th>
              <th className="px-4 py-2.5 font-semibold">Role</th>
              <th className="px-4 py-2.5 font-semibold">Reason</th>
              <th className="px-4 py-2.5 font-semibold">Approvals</th>
              <th className="px-4 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {(requests.data ?? []).map((request) => (
              <tr key={request.id} className="border-b border-border-soft last:border-0">
                <td className="px-4 py-2 font-mono text-[11.5px] text-muted">
                  {request.requester_id.slice(0, 8)}
                </td>
                <td className="px-4 py-2 text-[12.5px]">{request.environment}</td>
                <td className="px-4 py-2 text-[12.5px]">{request.role}</td>
                <td
                  className="px-4 py-2 max-w-64 truncate text-[12px] text-muted"
                  title={request.reason}
                >
                  {request.reason}
                </td>
                <td className="px-4 py-2 text-[12px]">
                  {(request.approvals ?? []).length}/{request.required_approvals}
                </td>
                <td className="px-4 py-2">
                  <div className="flex justify-end gap-1">
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={decide.isPending}
                      onClick={() => {
                        decide.mutate({ request, approve: true })
                      }}
                    >
                      <Check className="size-3.5" /> Approve
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={decide.isPending}
                      onClick={() => {
                        decide.mutate({ request, approve: false })
                      }}
                    >
                      <X className="size-3.5" /> Reject
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
            {(requests.data ?? []).length === 0 && !requests.isLoading && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-[13px] text-muted">
                  No pending access requests.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>
    </div>
  )
}
