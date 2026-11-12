import { Check, KeyRound, Loader2, ShieldAlert, X } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth } from '@/lib/auth'
import { Input } from '@/components/ui/field'

export function DevicePage() {
  const { user } = useAuth()
  const params = new URLSearchParams(window.location.search)
  const [code, setCode] = useState((params.get('code') ?? '').toUpperCase())
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<'approved' | 'denied' | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function decide(action: 'approve' | 'deny') {
    setBusy(true)
    setError(null)
    const response = await api.POST('/api/v1/auth/device/approve', {
      body: { user_code: code, action },
    })
    setBusy(false)
    if (response.error) {
      setError(apiErrorMessage(response.error, 'Could not complete the request.'))
      return
    }
    setResult(action === 'approve' ? 'approved' : 'denied')
  }

  function submit(event: FormEvent) {
    event.preventDefault()
    void decide('approve')
  }

  if (result) {
    return (
      <div className="mx-auto max-w-md py-10">
        <div className="rounded-app border border-border bg-surface p-6 text-center shadow-app">
          <div className="mx-auto flex size-10 items-center justify-center rounded-full bg-accent-soft">
            {result === 'approved' ? (
              <Check className="size-5 text-accent-text" />
            ) : (
              <X className="size-5 text-danger" />
            )}
          </div>
          <h1 className="mt-3 font-head text-xl font-semibold">
            {result === 'approved' ? 'CLI approved' : 'Request denied'}
          </h1>
          <p className="mt-1 text-sm text-muted">
            {result === 'approved'
              ? 'You can return to your terminal. The CLI finishes signing in automatically.'
              : 'The CLI was not granted access. You can close this tab.'}
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-md py-10">
      <div className="rounded-app border border-border bg-surface p-6 shadow-app">
        <div className="flex items-center gap-2.5">
          <div className="logo-mark flex size-9 items-center justify-center rounded-[10px]">
            <KeyRound className="size-4 text-[#06241c]" strokeWidth={2.6} />
          </div>
          <div>
            <h1 className="font-head text-lg font-semibold tracking-tight">Approve CLI login</h1>
            <p className="text-xs text-dim">Signed in as {user?.email}</p>
          </div>
        </div>

        <form onSubmit={submit} className="mt-5 space-y-3">
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted">
              Code from the terminal
            </span>
            <Input
              autoFocus
              required
              maxLength={20}
              className="h-11 px-3 text-center font-mono text-lg tracking-[0.18em]"
              placeholder="ABCD-EFGH"
              value={code}
              onChange={(event) => {
                setCode(event.target.value.toUpperCase())
              }}
            />
          </label>
          {error && (
            <p className="rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
          )}
          <div className="flex gap-2">
            <Button type="submit" variant="primary" size="lg" className="flex-1" disabled={busy}>
              {busy ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}
              Approve
            </Button>
            <Button
              type="button"
              variant="secondary"
              size="lg"
              disabled={busy}
              onClick={() => {
                void decide('deny')
              }}
            >
              Deny
            </Button>
          </div>
        </form>

        <div className="mt-4 flex items-start gap-2 rounded-app-sm bg-amber-bg px-2.5 py-2 text-xs text-amber-fg">
          <ShieldAlert className="mt-0.5 size-3.5 shrink-0" />
          <span>
            Only approve a code you just started with{' '}
            <code className="font-mono">clavis login</code>. Anyone with the code can ask for
            access.
          </span>
        </div>
      </div>
    </div>
  )
}
