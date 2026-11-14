import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, KeyRound, Loader2, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useCurrentOrg } from '@/lib/org'
import { Select } from '@/components/ui/field'
import { formatDate as formatDay } from '@/lib/format'

const permissionOptions = [
  ['secrets:read-names', 'Read key names'],
  ['secrets:read-values', 'Read values'],
  ['secrets:use', 'Use without reveal'],
  ['secrets:write', 'Create and update'],
  ['secrets:delete', 'Delete'],
  ['audit:read', 'Read audit log'],
] as const

type Token = {
  id: string
  prefix: string
  permissions: string[]
  expires_at?: string
  last_used_at?: string
  created_at: string
}

export function PersonalTokens() {
  const { org } = useCurrentOrg()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [permissions, setPermissions] = useState<string[]>(['secrets:read-names'])
  const [days, setDays] = useState(90)
  const [token, setToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const tokens = useQuery({
    queryKey: ['personal-tokens', org?.slug],
    enabled: Boolean(org),
    queryFn: async () => {
      if (!org) throw new Error('No organization.')
      const result = await api.GET('/api/v1/orgs/{org}/tokens', {
        params: { path: { org: org.slug } },
      })
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load tokens.'))
      return (result.data?.tokens ?? []) as Token[]
    },
  })

  async function create(event: FormEvent) {
    event.preventDefault()
    if (!org) return
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/orgs/{org}/tokens', {
      params: { path: { org: org.slug } },
      body: { permissions, expires_in_days: days },
    })
    setBusy(false)
    if (result.error || !result.data) {
      setError(apiErrorMessage(result.error, 'Could not create the token.'))
      return
    }
    setOpen(false)
    setToken(result.data.token)
    await queryClient.invalidateQueries({ queryKey: ['personal-tokens', org.slug] })
  }

  async function revoke(id: string) {
    if (!org) return
    const result = await api.DELETE('/api/v1/orgs/{org}/tokens/{id}', {
      params: { path: { org: org.slug, id } },
    })
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not revoke the token.'))
      return
    }
    await queryClient.invalidateQueries({ queryKey: ['personal-tokens', org.slug] })
  }

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
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
        <KeyRound className="size-4 text-muted" />
        <h2 className="text-base font-semibold">Personal access tokens</h2>
        <Button
          variant="secondary"
          size="sm"
          className="ml-auto"
          onClick={() => {
            setOpen((value) => !value)
          }}
        >
          New token
        </Button>
      </div>

      {token && (
        <div className="border-b border-border bg-accent-soft p-4">
          <p className="text-sm font-semibold text-accent-text">Copy this token now</p>
          <p className="text-xs text-muted">It is shown once and can be revoked at any time.</p>
          <div className="mt-2 flex items-center gap-2">
            <code className="min-w-0 flex-1 break-all rounded-app-sm border border-border bg-bg px-2.5 py-1.5 font-mono text-xs text-text">
              {token}
            </code>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                void copy()
              }}
            >
              {copied ? (
                <Check className="size-3.5 text-accent-text" />
              ) : (
                <Copy className="size-3.5" />
              )}
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

      {open && (
        <form
          onSubmit={(event) => {
            void create(event)
          }}
          className="space-y-3 border-b border-border p-4"
        >
          <div className="grid gap-2 sm:grid-cols-2">
            {permissionOptions.map(([value, label]) => (
              <label key={value} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={permissions.includes(value)}
                  onChange={(event) => {
                    setPermissions((current) =>
                      event.target.checked
                        ? [...current, value]
                        : current.filter((item) => item !== value),
                    )
                  }}
                />
                {label}
              </label>
            ))}
          </div>
          <label className="flex items-center gap-2 text-sm">
            Expires in
            <Select
              className="h-8"
              value={days}
              onChange={(event) => {
                setDays(Number(event.target.value))
              }}
            >
              <option value={30}>30 days</option>
              <option value={90}>90 days</option>
              <option value={180}>180 days</option>
              <option value={365}>365 days</option>
            </Select>
          </label>
          {error && (
            <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
          )}
          <Button
            type="submit"
            variant="primary"
            size="sm"
            disabled={busy || permissions.length === 0}
          >
            {busy && <Loader2 className="size-3.5 animate-spin" />} Create token
          </Button>
        </form>
      )}

      <div className="divide-y divide-border-soft">
        {(tokens.data ?? []).map((item) => (
          <div key={item.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="font-mono text-xs text-text">{item.prefix}</p>
              <p className="truncate text-2xs text-dim">
                {item.permissions.join(', ')} · expires{' '}
                {item.expires_at ? formatDay(item.expires_at) : 'never'} · last used{' '}
                {item.last_used_at ? formatDay(item.last_used_at) : 'never'}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Revoke token"
              onClick={() => {
                void revoke(item.id)
              }}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        ))}
        {(tokens.data?.length ?? 0) === 0 && (
          <p className="px-4 py-5 text-sm text-muted">No personal tokens yet.</p>
        )}
      </div>
    </section>
  )
}
