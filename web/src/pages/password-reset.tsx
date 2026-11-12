import { Check, Loader2 } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { AuthLayout } from '@/pages/auth'

const inputClass =
  'h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[12.5px] text-text focus:border-accent-line focus:outline-none'

export function ForgotPasswordPage() {
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState(false)
  const [sent, setSent] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/auth/password/forgot', { body: { email } })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not request a reset.'))
      return
    }
    setSent(true)
  }

  if (sent) {
    return (
      <AuthLayout
        title="Check your email"
        subtitle="If an account exists for that address, a reset link is on its way."
      >
        <div className="flex flex-col items-center gap-3 py-2 text-center">
          <div className="flex size-10 items-center justify-center rounded-full bg-accent-soft">
            <Check className="size-5 text-accent-text" />
          </div>
          <p className="text-[12.5px] text-muted">
            The link expires in 30 minutes and can be used once. Check spam if it does not arrive.
          </p>
          <a className="text-[12px] text-accent-text hover:underline" href="/">
            Back to sign in
          </a>
        </div>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout
      title="Reset your password"
      subtitle="Enter your account email. We will send a single-use link."
    >
      <form
        onSubmit={(event) => {
          void submit(event)
        }}
        className="space-y-3"
      >
        <label className="block">
          <span className="mb-1 block text-[11.5px] font-medium text-muted">Email</span>
          <input
            required
            autoFocus
            type="email"
            autoComplete="email"
            className={inputClass}
            value={email}
            onChange={(event) => {
              setEmail(event.target.value)
            }}
          />
        </label>
        {error && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[11.5px] text-danger">
            {error}
          </p>
        )}
        <Button type="submit" variant="primary" size="lg" className="w-full" disabled={busy}>
          {busy && <Loader2 className="size-4 animate-spin" />} Send reset link
        </Button>
        <p className="text-center text-[11px] text-dim">
          <a className="text-accent-text hover:underline" href="/">
            Back to sign in
          </a>
        </p>
      </form>
    </AuthLayout>
  )
}

export function ResetPasswordPage() {
  const token = new URLSearchParams(window.location.search).get('token') ?? ''
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (password !== confirm) {
      setError('Passwords do not match.')
      return
    }
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/auth/password/reset', { body: { token, password } })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not reset the password.'))
      return
    }
    setDone(true)
  }

  if (token === '') {
    return (
      <AuthLayout title="Reset link invalid" subtitle="This link is missing its token.">
        <p className="text-[12.5px] text-muted">Request a new reset link from the sign-in page.</p>
        <a
          className="mt-4 block text-center text-[12px] text-accent-text hover:underline"
          href="/forgot"
        >
          Request a new link
        </a>
      </AuthLayout>
    )
  }

  if (done) {
    return (
      <AuthLayout title="Password updated" subtitle="Every session was signed out for safety.">
        <div className="flex flex-col items-center gap-3 py-2 text-center">
          <div className="flex size-10 items-center justify-center rounded-full bg-accent-soft">
            <Check className="size-5 text-accent-text" />
          </div>
          <Button
            variant="primary"
            size="md"
            onClick={() => {
              window.location.assign('/')
            }}
          >
            Sign in with the new password
          </Button>
        </div>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout
      title="Choose a new password"
      subtitle="At least 12 characters. All existing sessions will be revoked."
    >
      <form
        onSubmit={(event) => {
          void submit(event)
        }}
        className="space-y-3"
      >
        <label className="block">
          <span className="mb-1 block text-[11.5px] font-medium text-muted">New password</span>
          <input
            required
            autoFocus
            type="password"
            minLength={12}
            autoComplete="new-password"
            className={inputClass}
            value={password}
            onChange={(event) => {
              setPassword(event.target.value)
            }}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-[11.5px] font-medium text-muted">Confirm password</span>
          <input
            required
            type="password"
            minLength={12}
            autoComplete="new-password"
            className={inputClass}
            value={confirm}
            onChange={(event) => {
              setConfirm(event.target.value)
            }}
          />
        </label>
        {error && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[11.5px] text-danger">
            {error}
          </p>
        )}
        <Button type="submit" variant="primary" size="lg" className="w-full" disabled={busy}>
          {busy && <Loader2 className="size-4 animate-spin" />} Update password
        </Button>
      </form>
    </AuthLayout>
  )
}

export function VerifyEmailPage() {
  const token = new URLSearchParams(window.location.search).get('token') ?? ''
  const [state, setState] = useState<'loading' | 'done' | 'failed'>('loading')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (token === '') {
      setState('failed')
      setError('This link is missing its token.')
      return
    }
    let active = true
    void (async () => {
      const result = await api.POST('/api/v1/auth/verify', { body: { token } })
      if (!active) return
      if (result.error) {
        setState('failed')
        setError(apiErrorMessage(result.error, 'Could not verify the address.'))
      } else {
        setState('done')
      }
    })()
    return () => {
      active = false
    }
  }, [token])

  if (state === 'loading') {
    return (
      <AuthLayout title="Verifying your email" subtitle="One moment.">
        <div className="flex items-center justify-center gap-2 py-4 text-[12px] text-muted">
          <Loader2 className="size-4 animate-spin" /> Checking the link…
        </div>
      </AuthLayout>
    )
  }

  if (state === 'failed') {
    return (
      <AuthLayout
        title="Verification failed"
        subtitle="The link may have expired or already been used."
      >
        <p className="text-[12.5px] text-muted">{error}</p>
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
      </AuthLayout>
    )
  }

  return (
    <AuthLayout title="Email verified" subtitle="Your address is confirmed.">
      <div className="flex flex-col items-center gap-3 py-2 text-center">
        <div className="flex size-10 items-center justify-center rounded-full bg-accent-soft">
          <Check className="size-5 text-accent-text" />
        </div>
        <Button
          variant="primary"
          size="md"
          onClick={() => {
            window.location.assign('/')
          }}
        >
          Continue
        </Button>
      </div>
    </AuthLayout>
  )
}
