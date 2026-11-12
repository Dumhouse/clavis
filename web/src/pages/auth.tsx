import { startAuthentication } from '@simplewebauthn/browser'
import { Fingerprint, KeyRound, Loader2 } from 'lucide-react'
import { useState, type FormEvent, type ReactNode } from 'react'

import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth, type SetupForm } from '@/lib/auth'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/field'

export function AuthLoading() {
  return (
    <div className="flex h-full items-center justify-center bg-bg">
      <div className="flex items-center gap-2 text-sm text-muted">
        <Loader2 className="size-4 animate-spin" />
        Loading Clavis…
      </div>
    </div>
  )
}

export function AuthLayout({
  title,
  subtitle,
  children,
}: {
  title: string
  subtitle: string
  children: ReactNode
}) {
  return (
    <div className="relative flex min-h-full items-center justify-center overflow-hidden bg-bg px-4 py-10">
      <div
        aria-hidden
        className="pointer-events-none absolute -top-40 left-1/2 size-[42rem] -translate-x-1/2 rounded-full opacity-60 blur-3xl"
        style={{ background: 'radial-gradient(circle, var(--accent-soft), transparent 70%)' }}
      />
      <div className="relative w-full max-w-[420px]">
        <div className="mb-6 flex flex-col items-center text-center">
          <div className="logo-mark mb-3 flex size-10 items-center justify-center rounded-[12px] shadow-app">
            <KeyRound className="size-5 text-[#06241c]" strokeWidth={2.6} />
          </div>
          <h1 className="font-head text-2xl font-semibold tracking-tight">{title}</h1>
          <p className="mt-1 max-w-sm text-sm text-muted">{subtitle}</p>
        </div>
        <div className="rounded-app border border-border bg-surface p-5 shadow-app">{children}</div>
        <p className="mt-4 text-center text-xs text-dim">Open source · Apache-2.0</p>
      </div>
    </div>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="mb-1 flex items-baseline justify-between gap-2 text-xs font-medium text-muted">
        {label}
        {hint && <span className="text-2xs font-normal text-dim">{hint}</span>}
      </span>
      {children}
    </label>
  )
}

const inputClass =
  'h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-sm text-text placeholder:text-dim focus:border-accent-line focus:outline-none'

export function LoginPage() {
  const { login, verifyMFA } = useAuth()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [challenge, setChallenge] = useState<string | null>(null)
  const [code, setCode] = useState('')
  const [useRecovery, setUseRecovery] = useState(false)
  const [passkeyBusy, setPasskeyBusy] = useState(false)
  const [ssoOrg, setSsoOrg] = useState('')

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      const result = await login(email, password)
      if (result.mfaRequired) {
        setChallenge(result.challenge)
        setCode('')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not sign in.')
    } finally {
      setBusy(false)
    }
  }

  async function signInWithPasskey() {
    setPasskeyBusy(true)
    setError(null)
    try {
      const begin = await api.POST('/api/v1/auth/passkeys/login/begin')
      if (begin.error || !begin.data) {
        throw new Error(apiErrorMessage(begin.error, 'Passkey sign-in is unavailable.'))
      }
      const credential = await startAuthentication({
        optionsJSON: begin.data.options as Parameters<typeof startAuthentication>[0]['optionsJSON'],
      })
      const finish = await api.POST('/api/v1/auth/passkeys/login/finish', {
        body: { challenge_id: begin.data.challenge_id, credential },
      })
      if (finish.error) {
        throw new Error(apiErrorMessage(finish.error, 'Passkey sign-in failed.'))
      }
      window.location.assign('/')
    } catch (cause) {
      if (cause instanceof Error && cause.name === 'NotAllowedError') {
        setError('Passkey sign-in was cancelled.')
      } else {
        setError(cause instanceof Error ? cause.message : 'Passkey sign-in failed.')
      }
    } finally {
      setPasskeyBusy(false)
    }
  }

  async function submitMFA(event: FormEvent) {
    event.preventDefault()
    if (!challenge) return
    setBusy(true)
    setError(null)
    try {
      await verifyMFA(challenge, useRecovery ? '' : code, useRecovery ? code : undefined)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not verify the code.')
      setBusy(false)
    }
  }

  if (challenge) {
    return (
      <AuthLayout
        title="Two-factor authentication"
        subtitle="Enter the 6-digit code from your authenticator app, or use a recovery code."
      >
        <form
          onSubmit={(event) => {
            void submitMFA(event)
          }}
          className="space-y-3"
        >
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted">
              {useRecovery ? 'Recovery code' : 'Authenticator code'}
            </span>
            <Input
              autoFocus
              required
              inputMode={useRecovery ? 'text' : 'numeric'}
              autoComplete="one-time-code"
              maxLength={useRecovery ? 32 : 6}
              className={cn(inputClass, 'text-center font-mono text-lg tracking-[0.2em]')}
              value={code}
              onChange={(event) => {
                setCode(event.target.value.trim())
              }}
            />
          </label>
          {error && (
            <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
          )}
          <Button type="submit" variant="primary" size="lg" className="w-full" disabled={busy}>
            {busy && <Loader2 className="size-4 animate-spin" />} Verify
          </Button>
          <button
            type="button"
            className="w-full text-center text-xs text-accent-text hover:underline"
            onClick={() => {
              setUseRecovery((current) => !current)
              setCode('')
              setError(null)
            }}
          >
            {useRecovery ? 'Use an authenticator code' : 'Use a recovery code'}
          </button>
          <button
            type="button"
            className="w-full text-center text-xs text-dim hover:text-muted"
            onClick={() => {
              setChallenge(null)
              setCode('')
              setUseRecovery(false)
            }}
          >
            Back to sign in
          </button>
        </form>
      </AuthLayout>
    )
  }

  const passkeysSupported = typeof window !== 'undefined' && 'PublicKeyCredential' in window

  return (
    <AuthLayout title="Welcome back" subtitle="Sign in to Clavis.">
      <div className="space-y-3">
        {passkeysSupported && (
          <>
            <Button
              type="button"
              variant="primary"
              size="lg"
              className="w-full"
              disabled={passkeyBusy || busy}
              onClick={() => {
                void signInWithPasskey()
              }}
            >
              {passkeyBusy ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Fingerprint className="size-4" />
              )}
              Sign in with a passkey
            </Button>
            <div className="flex items-center gap-2 py-1 text-2xs text-dim">
              <span className="h-px flex-1 bg-border" /> or use your email{' '}
              <span className="h-px flex-1 bg-border" />
            </div>
          </>
        )}
        <form
          onSubmit={(event) => {
            void submit(event)
          }}
          className="space-y-3"
        >
          <Field label="Email">
            <Input
              type="email"
              autoComplete="username webauthn"
              required
              autoFocus={!passkeysSupported}
              value={email}
              onChange={(event) => {
                setEmail(event.target.value)
              }}
            />
          </Field>
          <div>
            <label htmlFor="login-password" className="mb-1 block text-xs font-medium text-muted">
              Password
            </label>
            <Input
              id="login-password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(event) => {
                setPassword(event.target.value)
              }}
            />
            <p className="mt-1.5 text-right">
              <a href="/forgot" className="text-2xs text-accent-text hover:underline">
                Forgot password?
              </a>
            </p>
          </div>
          {error && (
            <p role="alert" className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">
              {error}
            </p>
          )}
          <Button
            type="submit"
            variant={passkeysSupported ? 'secondary' : 'primary'}
            size="lg"
            className="w-full"
            disabled={busy}
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : null}
            {busy ? 'Signing in…' : 'Sign in with password'}
          </Button>
        </form>
        <details className="group border-t border-border-soft pt-3">
          <summary className="cursor-pointer list-none text-center text-xs text-muted hover:text-text">
            Use your company’s single sign-on
          </summary>
          <form
            className="mt-3 flex items-end gap-2"
            onSubmit={(event) => {
              event.preventDefault()
              if (ssoOrg.trim() === '') return
              window.location.assign(`/api/v1/auth/sso/${encodeURIComponent(ssoOrg.trim())}/start`)
            }}
          >
            <label className="block min-w-0 flex-1">
              <span className="mb-1 block text-xs font-medium text-muted">Organization</span>
              <Input
                placeholder="acme"
                autoCapitalize="none"
                value={ssoOrg}
                onChange={(event) => {
                  setSsoOrg(event.target.value)
                }}
              />
            </label>
            <Button type="submit" variant="secondary" size="md" disabled={ssoOrg.trim() === ''}>
              Continue
            </Button>
          </form>
        </details>
      </div>
    </AuthLayout>
  )
}

export function SetupPage() {
  const { setup } = useAuth()
  const params = new URLSearchParams(window.location.search)
  const [form, setForm] = useState<SetupForm>({
    token: params.get('token') ?? '',
    email: '',
    name: '',
    password: '',
    orgName: '',
    orgSlug: '',
    projectName: '',
    projectSlug: '',
  })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function update<K extends keyof SetupForm>(key: K, value: SetupForm[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await setup(form)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Setup failed.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthLayout
      title="Set up Clavis"
      subtitle="This instance is unclaimed. The one-time token is in the server log and in the data directory as setup-token."
    >
      <form
        onSubmit={(event) => {
          void submit(event)
        }}
        className="space-y-3"
      >
        <Field label="Setup token" hint="one-time">
          <Input
            className={cn(inputClass, 'font-mono text-xs')}
            required
            autoFocus
            value={form.token}
            onChange={(event) => {
              update('token', event.target.value)
            }}
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Your name">
            <Input
              required
              value={form.name}
              onChange={(event) => {
                update('name', event.target.value)
              }}
            />
          </Field>
          <Field label="Email">
            <Input
              type="email"
              required
              value={form.email}
              onChange={(event) => {
                update('email', event.target.value)
              }}
            />
          </Field>
        </div>
        <Field label="Password" hint="at least 12 characters">
          <Input
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            value={form.password}
            onChange={(event) => {
              update('password', event.target.value)
            }}
          />
        </Field>
        <div className="border-t border-border-soft pt-3">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Organization">
              <Input
                required
                placeholder="Acme"
                value={form.orgName}
                onChange={(event) => {
                  update('orgName', event.target.value)
                }}
              />
            </Field>
            <Field label="First project" hint="optional">
              <Input
                placeholder="Web app"
                value={form.projectName}
                onChange={(event) => {
                  update('projectName', event.target.value)
                }}
              />
            </Field>
          </div>
        </div>
        {error && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
        )}
        <Button type="submit" variant="primary" size="lg" className="w-full" disabled={busy}>
          {busy ? <Loader2 className="size-4 animate-spin" /> : null}
          {busy ? 'Creating your instance…' : 'Create instance'}
        </Button>
      </form>
    </AuthLayout>
  )
}
