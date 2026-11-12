import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Loader2, RefreshCw, Trash2, UserCog } from 'lucide-react'
import { useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'

type ScimToken = components['schemas']['ScimTokenBody']

export function ScimSettings() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [token, setToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const tokens = useQuery({
    queryKey: ['scim-tokens', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/scim/tokens', {
        params: { path: { org: org.slug } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not load SCIM tokens.'))
      return result.data?.tokens ?? []
    },
  })

  const invalidate = async () => {
    await queryClient.invalidateQueries({ queryKey: ['scim-tokens', org?.slug] })
  }

  const create = useMutation({
    mutationFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.POST('/api/v1/orgs/{org}/scim/tokens', {
        params: { path: { org: org.slug } },
      })
      if (result.error || !result.data) {
        throw new Error(apiErrorMessage(result.error, 'Could not create a SCIM token.'))
      }
      return result.data.token
    },
    onSuccess: async (value) => {
      setToken(value)
      setError(null)
      await invalidate()
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not create a SCIM token.')
    },
  })

  const revoke = useMutation({
    mutationFn: async (item: ScimToken) => {
      if (!org) throw new Error('No organization.')
      if (!window.confirm(`Revoke the SCIM token ${item.prefix}…?`)) return
      const result = await api.DELETE('/api/v1/orgs/{org}/scim/tokens/{id}', {
        params: { path: { org: org.slug, id: item.id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not revoke the token.'))
    },
    onSuccess: invalidate,
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not revoke the token.')
    },
  })

  async function copy() {
    if (!token) return
    await navigator.clipboard.writeText(token)
    setCopied(true)
    window.setTimeout(() => {
      setCopied(false)
    }, 1500)
  }

  return (
    <section className="rounded-app border border-border bg-surface">
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <UserCog className="size-4 text-muted" />
        <h2 className="text-[14px] font-semibold">SCIM provisioning</h2>
        <Button
          variant="secondary"
          size="sm"
          className="ml-auto"
          disabled={create.isPending}
          onClick={() => {
            create.mutate()
          }}
        >
          {create.isPending ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <RefreshCw className="size-3.5" />
          )}
          New token
        </Button>
      </div>
      <p className="border-b border-border-soft px-4 py-2 text-[12px] text-dim">
        Point your identity provider at{' '}
        <code className="font-mono">{window.location.origin}/scim/v2</code> with a bearer token.
        Users and groups are provisioned into {org?.name ?? 'this organization'}.
      </p>

      {token && (
        <div className="border-b border-border bg-accent-soft p-4">
          <p className="text-[13px] font-semibold text-accent-text">Copy this token now</p>
          <p className="text-[12.5px] text-muted">It is shown once.</p>
          <div className="mt-2 flex items-center gap-2">
            <code className="min-w-0 flex-1 break-all rounded-app-sm border border-border bg-bg px-2.5 py-1.5 font-mono text-[12px]">
              {token}
            </code>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                void copy()
              }}
            >
              {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
              {copied ? 'Copied' : 'Copy'}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setToken(null)
              }}
            >
              Done
            </Button>
          </div>
        </div>
      )}

      {error && <p className="px-4 py-3 text-[12.5px] text-danger">{error}</p>}
      <div className="divide-y divide-border-soft">
        {(tokens.data ?? []).map((item) => (
          <div key={item.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="font-mono text-[12.5px]">{item.prefix}…</p>
              <p className="text-[11.5px] text-dim">
                Created {new Date(item.created_at).toLocaleDateString()} · last used{' '}
                {item.last_used_at ? new Date(item.last_used_at).toLocaleDateString() : 'never'}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Revoke SCIM token"
              disabled={revoke.isPending}
              onClick={() => {
                revoke.mutate(item)
              }}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        ))}
        {(tokens.data?.length ?? 0) === 0 && !tokens.isLoading && (
          <p className="px-4 py-5 text-[13px] text-muted">No SCIM tokens yet.</p>
        )}
      </div>
    </section>
  )
}
