import { startAuthentication } from '@simplewebauthn/browser'
import { Fingerprint, KeyRound, Loader2 } from 'lucide-react'
import { useEffect, useState, type FormEvent, type ReactNode } from 'react'

import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { CopyCommand } from '@/components/copy-command'
import { passkeyErrorMessage, passkeyOptions, passkeyUnavailableReason } from '@/lib/passkeys'
import { apiErrorMessage } from '@/lib/api-error'
import { useSelfSignedFingerprint } from '@/lib/server-certificate'
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
  wide = false,
}: {
  title: string
  subtitle: string
  children: ReactNode
  wide?: boolean
}) {
  return (
    <div className="relative flex min-h-full items-center justify-center overflow-clip bg-bg px-4 py-10">
      <div
        aria-hidden
        className="pointer-events-none absolute -top-40 left-1/2 size-[42rem] -translate-x-1/2 rounded-full opacity-60 blur-3xl"
        style={{ background: 'radial-gradient(circle, var(--accent-soft), transparent 70%)' }}
      />
      <div className={cn('relative w-full', wide ? 'max-w-[640px]' : 'max-w-[420px]')}>
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

export function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: ReactNode
}) {
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

export const inputClass =
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
  const selfSigned = Boolean(useSelfSignedFingerprint())

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
        optionsJSON: passkeyOptions<Parameters<typeof startAuthentication>[0]['optionsJSON']>(
          begin.data.options,
        ),
      })
      const finish = await api.POST('/api/v1/auth/passkeys/login/finish', {
        body: { challenge_id: begin.data.challenge_id, credential },
      })
      if (finish.error) {
        throw new Error(apiErrorMessage(finish.error, 'Passkey sign-in failed.'))
      }
      window.location.assign('/')
    } catch (cause) {
      setError(passkeyErrorMessage(cause, 'Passkey sign-in failed.', selfSigned))
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

  // Hide the button where browsers refuse passkeys (an IP address, plain HTTP).
  const passkeysSupported = passkeyUnavailableReason(selfSigned) === null

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

const DOCS_HTTPS = 'https://dumhouse.github.io/clavis/https'
const DOCS_WIZARD = 'https://dumhouse.github.io/clavis/install'
const SETUP_TOKEN_KEY = 'clavis:setup-token'

type SetupStatus = {
  ownerEmail: string
  tlsMode: string
  secure: boolean
}

/**
 * Reads ?token= or #token= and removes it from the address bar. The token is
 * kept in sessionStorage (this tab only) so a refresh does not lose it.
 */
function takeTokenFromURL(): string {
  const fromHash = new URLSearchParams(window.location.hash.replace(/^#/, '')).get('token')
  const fromQuery = new URLSearchParams(window.location.search).get('token')
  const token = fromHash ?? fromQuery
  if (token) {
    window.sessionStorage.setItem(SETUP_TOKEN_KEY, token)
    window.history.replaceState(null, '', window.location.pathname)
    return token
  }
  return window.sessionStorage.getItem(SETUP_TOKEN_KEY) ?? ''
}

function isLoopbackHost(host: string): boolean {
  return (
    host === 'localhost' || host === '127.0.0.1' || host === '[::1]' || host.endsWith('.localhost')
  )
}

function TokenHelp() {
  return (
    <details className="rounded-app-sm border border-border bg-panel px-3 py-2 text-xs text-muted">
      <summary className="cursor-pointer select-none font-medium text-text">
        Where do I find the token?
      </summary>
      <div className="mt-2 space-y-2">
        <p>Run the command that matches how you started Clavis:</p>
        <p className="text-dim">Docker</p>
        <CopyCommand command="docker exec clavis clavis setup-token" />
        <p className="text-dim">Docker Compose</p>
        <CopyCommand command="docker compose exec clavis clavis setup-token" />
        <p className="text-dim">Binary or package</p>
        <CopyCommand command="clavis setup-token" />
        <p>
          The server log also prints a setup link with the token built in. On a remote server,
          prefix the command with <code className="font-mono">ssh user@host</code>.
        </p>
      </div>
    </details>
  )
}

function ConnectionNotice({ status }: { status: SetupStatus | null }) {
  const host = window.location.host
  if (window.location.protocol === 'http:' && !isLoopbackHost(window.location.hostname)) {
    return (
      <div className="break-words rounded-app-sm bg-amber-bg px-3 py-2 text-xs text-amber-fg [&_code]:break-all">
        <p className="font-medium">This connection is not encrypted.</p>
        <p className="mt-1">
          Your password and secrets travel in clear text to {host}, and passkeys are disabled. That
          is fine for a quick test on a trusted network. For real use, restart Clavis with HTTPS,
          for example <code className="font-mono">CLAVIS_TLS=self-signed</code> and{' '}
          <code className="font-mono">
            CLAVIS_SERVER_BASE_URL=https://{window.location.hostname}
          </code>
          .{' '}
          <a
            className="underline"
            href={`${DOCS_WIZARD}?scenario=lan&host=${encodeURIComponent(window.location.hostname)}`}
            target="_blank"
            rel="noreferrer"
          >
            Open the install wizard
          </a>{' '}
          to get a ready command, or read{' '}
          <a className="underline" href={DOCS_HTTPS} target="_blank" rel="noreferrer">
            how HTTPS works
          </a>
          .
        </p>
      </div>
    )
  }
  if (status?.tlsMode === 'self-signed') {
    return (
      <p className="rounded-app-sm border border-border bg-panel px-3 py-2 text-xs text-muted">
        HTTPS with a self-signed certificate: the browser warning you saw is expected. To remove it,
        trust the certificate on your devices or switch to Let&apos;s Encrypt.{' '}
        <a className="underline" href={DOCS_HTTPS} target="_blank" rel="noreferrer">
          Learn how
        </a>
      </p>
    )
  }
  return null
}

export function SetupPage() {
  const { setup } = useAuth()
  const [initialToken] = useState(takeTokenFromURL)
  const [status, setStatus] = useState<SetupStatus | null>(null)
  const [form, setForm] = useState<SetupForm>({
    token: initialToken,
    email: '',
    name: '',
    password: '',
  })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    void api.GET('/api/v1/setup/status').then(({ data }) => {
      if (!data) return
      const next = {
        ownerEmail: data.owner_email ?? '',
        tlsMode: data.tls_mode ?? 'off',
        secure: data.secure,
      }
      setStatus(next)
      if (next.ownerEmail) {
        setForm((current) => (current.email ? current : { ...current, email: next.ownerEmail }))
      }
    })
  }, [])

  function update<K extends keyof SetupForm>(key: K, value: SetupForm[K]) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await setup(form)
      window.sessionStorage.removeItem(SETUP_TOKEN_KEY)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Setup failed.')
    } finally {
      setBusy(false)
    }
  }

  const tokenFromLink = initialToken !== ''
  const emailLocked = Boolean(status?.ownerEmail)

  return (
    <AuthLayout
      title="Set up Clavis"
      subtitle={
        tokenFromLink
          ? 'Your setup link included the one-time token. Create the owner account to claim this server. You will name your organization and first project next.'
          : 'This server is unclaimed. Paste the one-time setup token to claim it. You will name your organization and first project next.'
      }
    >
      <form
        onSubmit={(event) => {
          void submit(event)
        }}
        className="space-y-3"
      >
        <ConnectionNotice status={status} />
        {tokenFromLink ? (
          <p className="flex items-center gap-2 rounded-app-sm border border-border bg-panel px-3 py-2 text-xs text-muted">
            <KeyRound className="size-3.5 text-success" />
            Setup token filled in from the link.
          </p>
        ) : (
          <>
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
            <TokenHelp />
          </>
        )}
        <div className="grid grid-cols-2 gap-3">
          <Field label="Your name">
            <Input
              required
              autoFocus={tokenFromLink}
              value={form.name}
              onChange={(event) => {
                update('name', event.target.value)
              }}
            />
          </Field>
          <Field label="Email" {...(emailLocked ? { hint: 'preset by the server' } : {})}>
            <Input
              type="email"
              required
              readOnly={emailLocked}
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
        {error && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
        )}
        <Button type="submit" variant="primary" size="lg" className="w-full" disabled={busy}>
          {busy ? <Loader2 className="size-4 animate-spin" /> : null}
          {busy ? 'Creating your account…' : 'Create owner account'}
        </Button>
      </form>
    </AuthLayout>
  )
}
