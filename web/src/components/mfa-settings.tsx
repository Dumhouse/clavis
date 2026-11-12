import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, Loader2, ShieldCheck } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'

type Mode = 'idle' | 'enroll' | 'recovery' | 'disable' | 'regenerate'

const inputClass =
  'h-9 w-full rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none'

export function MfaSettings() {
  const queryClient = useQueryClient()
  const [mode, setMode] = useState<Mode>('idle')
  const [code, setCode] = useState('')
  const [secret, setSecret] = useState('')
  const [uri, setUri] = useState('')
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [copied, setCopied] = useState<string | null>(null)

  const status = useQuery({
    queryKey: ['mfa', 'status'],
    queryFn: async () => {
      const result = await api.GET('/api/v1/auth/mfa')
      if (result.error || !result.data)
        throw new Error(apiErrorMessage(result.error, 'Could not load MFA status.'))
      return result.data
    },
  })

  async function copy(value: string, id: string) {
    await navigator.clipboard.writeText(value)
    setCopied(id)
    window.setTimeout(() => {
      setCopied(null)
    }, 1500)
  }

  async function startEnroll() {
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/auth/mfa/totp/enroll')
    setBusy(false)
    if (result.error || !result.data) {
      setError(apiErrorMessage(result.error, 'Could not start enrollment.'))
      return
    }
    setSecret(result.data.secret)
    setUri(result.data.url)
    setCode('')
    setMode('enroll')
  }

  async function activate(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/auth/mfa/totp/activate', { body: { code } })
    setBusy(false)
    if (result.error || !result.data) {
      setError(apiErrorMessage(result.error, 'Could not activate MFA.'))
      return
    }
    setRecoveryCodes(result.data.recovery_codes ?? [])
    setMode('recovery')
    setMessage('Two-factor authentication is enabled.')
    await queryClient.invalidateQueries({ queryKey: ['mfa', 'status'] })
  }

  async function disable(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/auth/mfa/totp/disable', { body: { code } })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not disable MFA.'))
      return
    }
    setMode('idle')
    setCode('')
    setRecoveryCodes([])
    setMessage('Two-factor authentication is disabled.')
    await queryClient.invalidateQueries({ queryKey: ['mfa', 'status'] })
  }

  async function regenerate(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/auth/mfa/recovery-codes', { body: { code } })
    setBusy(false)
    if (result.error || !result.data) {
      setError(apiErrorMessage(result.error, 'Could not regenerate codes.'))
      return
    }
    setRecoveryCodes(result.data.recovery_codes ?? [])
    setMode('recovery')
    setMessage('New recovery codes created. Old codes no longer work.')
    await queryClient.invalidateQueries({ queryKey: ['mfa', 'status'] })
  }

  const enabled = status.data?.enabled ?? false

  return (
    <section className="rounded-app border border-border bg-surface">
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <ShieldCheck className="size-4 text-muted" />
        <h2 className="text-[14px] font-semibold">Two-factor authentication</h2>
        {status.data && (
          <span
            className={
              enabled
                ? 'rounded-full bg-success-bg px-2 py-0.5 text-[11px] font-semibold text-success'
                : 'rounded-full bg-elev px-2 py-0.5 text-[11px] font-semibold text-dim'
            }
          >
            {enabled ? 'enabled' : 'off'}
          </span>
        )}
      </div>

      <div className="space-y-3 p-4 text-[13.5px]">
        {status.isLoading && (
          <div className="flex items-center gap-2 text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading…
          </div>
        )}
        {status.isError && (
          <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger">
            {status.error instanceof Error ? status.error.message : 'Could not load MFA status.'}
          </p>
        )}

        {message && (
          <p className="rounded-app-sm bg-success-bg px-2.5 py-2 text-[12.5px] text-success">
            {message}
          </p>
        )}

        {mode === 'idle' && !enabled && status.data && (
          <div className="flex flex-wrap items-center gap-3">
            <div className="min-w-0 flex-1">
              <p className="font-medium">Protect your account with an authenticator app</p>
              <p className="text-[12.5px] text-muted">
                TOTP codes plus one-time recovery codes. Recommended for every Owner and Admin.
              </p>
            </div>
            <Button
              variant="primary"
              size="sm"
              onClick={() => {
                void startEnroll()
              }}
              disabled={busy}
            >
              {busy && <Loader2 className="size-3.5 animate-spin" />} Enable MFA
            </Button>
          </div>
        )}

        {mode === 'idle' && enabled && status.data && (
          <div className="flex flex-wrap items-center gap-3">
            <div className="min-w-0 flex-1">
              <p className="font-medium">Authenticator app</p>
              <p className="text-[12.5px] text-muted">
                {status.data.recovery_codes_remaining} recovery{' '}
                {status.data.recovery_codes_remaining === 1 ? 'code' : 'codes'} remaining.
              </p>
            </div>
            <div className="flex gap-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  setMode('regenerate')
                  setCode('')
                  setError(null)
                }}
              >
                New recovery codes
              </Button>
              <Button
                variant="danger"
                size="sm"
                onClick={() => {
                  setMode('disable')
                  setCode('')
                  setError(null)
                }}
              >
                Disable
              </Button>
            </div>
          </div>
        )}

        {mode === 'enroll' && (
          <form
            onSubmit={(event) => {
              void activate(event)
            }}
            className="space-y-3"
          >
            <p className="text-[12.5px] text-muted">
              Add this secret to your authenticator manually, then enter the current 6-digit code.
            </p>
            <div className="rounded-app-sm border border-border bg-bg px-2.5 py-2">
              <p className="flex items-center gap-2 font-mono text-[12.5px] text-text">
                <span className="break-all">{secret}</span>
                <button
                  type="button"
                  onClick={() => {
                    void copy(secret, 'secret')
                  }}
                  className="ml-auto text-dim hover:text-text"
                  aria-label="Copy secret"
                >
                  {copied === 'secret' ? (
                    <Check className="size-3.5 text-accent-text" />
                  ) : (
                    <Copy className="size-3.5" />
                  )}
                </button>
              </p>
              <p className="mt-1 truncate font-mono text-[11px] text-dim">{uri}</p>
            </div>
            <label className="block">
              <span className="mb-1 block text-[12.5px] font-medium text-muted">6-digit code</span>
              <input
                autoFocus
                required
                inputMode="numeric"
                maxLength={6}
                className={`${inputClass} text-center font-mono tracking-[0.2em]`}
                value={code}
                onChange={(event) => {
                  setCode(event.target.value.trim())
                }}
              />
            </label>
            {error && (
              <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger">
                {error}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  setMode('idle')
                  setError(null)
                }}
              >
                Cancel
              </Button>
              <Button type="submit" variant="primary" size="sm" disabled={busy}>
                {busy && <Loader2 className="size-3.5 animate-spin" />} Activate
              </Button>
            </div>
          </form>
        )}

        {(mode === 'disable' || mode === 'regenerate') && (
          <form
            onSubmit={(event) => {
              void (mode === 'disable' ? disable(event) : regenerate(event))
            }}
            className="space-y-3"
          >
            <p className="text-[12.5px] text-muted">
              Enter a current authenticator code to{' '}
              {mode === 'disable' ? 'disable MFA' : 'replace your recovery codes'}.
            </p>
            <input
              autoFocus
              required
              inputMode="numeric"
              maxLength={6}
              className={`${inputClass} text-center font-mono tracking-[0.2em]`}
              value={code}
              onChange={(event) => {
                setCode(event.target.value.trim())
              }}
            />
            {error && (
              <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-[12.5px] text-danger">
                {error}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => {
                  setMode('idle')
                  setError(null)
                }}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                variant={mode === 'disable' ? 'danger' : 'primary'}
                size="sm"
                disabled={busy}
              >
                {busy && <Loader2 className="size-3.5 animate-spin" />}
                {mode === 'disable' ? 'Disable MFA' : 'Replace codes'}
              </Button>
            </div>
          </form>
        )}

        {mode === 'recovery' && recoveryCodes.length > 0 && (
          <div className="space-y-3">
            <p className="text-[12.5px] text-muted">
              Save these codes somewhere safe. Each works once and they will not be shown again.
            </p>
            <div className="grid grid-cols-2 gap-1.5 rounded-app-sm border border-border bg-bg p-2.5 font-mono text-[12.5px] text-text">
              {recoveryCodes.map((recovery) => (
                <span key={recovery}>{recovery}</span>
              ))}
            </div>
            <div className="flex justify-end gap-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  void copy(recoveryCodes.join('\n'), 'codes')
                }}
              >
                {copied === 'codes' ? (
                  <Check className="size-3.5 text-accent-text" />
                ) : (
                  <Copy className="size-3.5" />
                )}
                Copy all
              </Button>
              <Button
                variant="primary"
                size="sm"
                onClick={() => {
                  setMode('idle')
                  setRecoveryCodes([])
                }}
              >
                Done
              </Button>
            </div>
          </div>
        )}
      </div>
    </section>
  )
}
