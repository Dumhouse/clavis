import { useQuery } from '@tanstack/react-query'
import { KeyRound, Loader2, ShieldCheck } from 'lucide-react'
import { useState, type FormEvent, type ReactNode } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth } from '@/lib/auth'
import { Input } from '@/components/ui/field'

type InviteInfo = components['schemas']['InviteInfoOutputBody']

export function JoinPage() {
  const { status, user } = useAuth()
  const token = new URLSearchParams(window.location.search).get('token') ?? ''
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const invite = useQuery({
    queryKey: ['invite', token],
    enabled: token !== '',
    queryFn: async () => {
      const result = await api.GET('/api/v1/invites/{token}', { params: { path: { token } } })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'This invite link is not valid.'))
      return result.data
    },
  })

  async function acceptNew(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/invites/{token}/accept', {
      params: { path: { token } },
      body: { name, email: invite.data?.email || email, password },
    })
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not accept the invite.'))
      setBusy(false)
      return
    }
    window.location.assign('/')
  }

  async function acceptExisting() {
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/invites/{token}/accept-existing', {
      params: { path: { token } },
    })
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not join the organization.'))
      setBusy(false)
      return
    }
    window.location.assign('/')
  }

  if (token === '' || invite.isError) {
    return (
      <Shell title="Invite not valid">
        <p className="text-sm text-muted">
          {invite.error instanceof Error
            ? invite.error.message
            : 'This invite link is missing or has expired.'}
        </p>
        <Button
          variant="primary"
          size="md"
          className="mt-4 w-full"
          onClick={() => {
            window.location.assign('/')
          }}
        >
          Go to Clavis
        </Button>
      </Shell>
    )
  }

  if (invite.isLoading || !invite.data) {
    return (
      <Shell title="Loading invite">
        <div className="flex items-center gap-2 text-sm text-muted">
          <Loader2 className="size-4 animate-spin" /> Checking your invite…
        </div>
      </Shell>
    )
  }

  const info: InviteInfo = invite.data
  const role = info.role ?? 'Developer'

  if (status === 'authenticated') {
    return (
      <Shell title={`Join ${info.org}`}>
        <p className="text-sm text-muted">
          You are signed in as <span className="font-medium text-text">{user?.email}</span>. You
          will join as <span className="font-medium text-text">{role}</span>.
        </p>
        {error && (
          <p className="mt-3 rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">
            {error}
          </p>
        )}
        <Button
          variant="primary"
          size="lg"
          className="mt-4 w-full"
          disabled={busy}
          onClick={() => {
            void acceptExisting()
          }}
        >
          {busy ? <Loader2 className="size-4 animate-spin" /> : <ShieldCheck className="size-4" />}
          Join {info.org}
        </Button>
      </Shell>
    )
  }

  return (
    <Shell title={`Join ${info.org}`}>
      <p className="text-sm text-muted">
        You have been invited as <span className="font-medium text-text">{role}</span>. Create an
        account to accept.
      </p>
      <form
        onSubmit={(event) => {
          void acceptNew(event)
        }}
        className="mt-4 space-y-3"
      >
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Your name</span>
          <Input
            required
            autoFocus
            className="h-9"
            value={name}
            onChange={(event) => {
              setName(event.target.value)
            }}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Email</span>
          <Input
            required
            type="email"
            readOnly={Boolean(info.email)}
            className="h-9 read-only:text-muted"
            value={info.email || email}
            onChange={(event) => {
              setEmail(event.target.value)
            }}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">
            Password (12+ characters)
          </span>
          <Input
            required
            type="password"
            minLength={12}
            autoComplete="new-password"
            className="h-9"
            value={password}
            onChange={(event) => {
              setPassword(event.target.value)
            }}
          />
        </label>
        {error && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
        )}
        <Button type="submit" variant="primary" size="lg" className="w-full" disabled={busy}>
          {busy && <Loader2 className="size-4 animate-spin" />}
          Create account and join
        </Button>
      </form>
      <p className="mt-3 text-center text-xs text-dim">
        Already have an account?{' '}
        <a className="text-accent-text hover:underline" href="/">
          Sign in first
        </a>
        , then reopen this link.
      </p>
    </Shell>
  )
}

function Shell({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex min-h-full items-center justify-center bg-bg px-4 py-10">
      <div className="w-full max-w-[420px]">
        <div className="mb-5 flex flex-col items-center text-center">
          <div className="logo-mark mb-3 flex size-10 items-center justify-center rounded-[12px] shadow-app">
            <KeyRound className="size-5 text-[#06241c]" strokeWidth={2.6} />
          </div>
          <h1 className="font-head text-2xl font-semibold tracking-tight">{title}</h1>
        </div>
        <div className="rounded-app border border-border bg-surface p-5 shadow-app">{children}</div>
      </div>
    </div>
  )
}
