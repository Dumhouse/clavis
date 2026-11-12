import { useEffect, useState } from 'react'
import { KeyRound, Loader2, ShieldCheck } from 'lucide-react'

import { api } from '@/api/client'
import { apiErrorMessage } from '@/lib/api-error'
import { useAuth } from '@/lib/auth'
import { ensureReauth } from '@/lib/reauth'

export function ApprovalPage() {
  const { status } = useAuth()
  const token = new URLSearchParams(window.location.search).get('token') ?? ''
  const [state, setState] = useState<'idle' | 'busy' | 'done' | 'error'>('idle')
  const [message, setMessage] = useState('')

  async function apply() {
    setState('busy')
    setMessage('Confirm your password to apply this decision.')
    try {
      const reauth = await ensureReauth()
      const result = await api.POST('/api/v1/access-approvals', {
        body: { token },
        headers: { 'X-Clavis-Reauth': reauth },
      })
      if (result.error || !result.data) {
        setState('error')
        setMessage(apiErrorMessage(result.error, 'This approval link is invalid or expired.'))
        return
      }
      setState('done')
      setMessage(`Request ${result.data.status}. The decision is recorded in the audit log.`)
    } catch (cause) {
      setState('error')
      setMessage(cause instanceof Error ? cause.message : 'Could not apply the decision.')
    }
  }

  useEffect(() => {
    if (status !== 'authenticated' || !token) return
    void apply()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status])

  return (
    <div className="flex min-h-full items-center justify-center bg-bg px-4 py-10 text-text">
      <div className="w-full max-w-lg rounded-app border border-border bg-surface p-6 shadow-app">
        <div className="flex items-center gap-2">
          <div className="logo-mark flex size-8 items-center justify-center rounded-[10px]">
            <KeyRound className="size-4 text-[#06241c]" strokeWidth={2.6} />
          </div>
          <div>
            <h1 className="font-head text-[19px] font-semibold">Access approval</h1>
            <p className="text-[12.5px] text-muted">
              Signed decision link. Re-authentication is required.
            </p>
          </div>
        </div>
        {!token && (
          <p className="mt-5 text-[13px] text-danger">This link is missing its signed token.</p>
        )}
        {status === 'anonymous' && (
          <p className="mt-5 rounded-app bg-amber-bg px-3 py-2 text-[13px] text-amber-fg">
            Sign in first, then open this link again to apply the decision.
          </p>
        )}
        {state === 'busy' && (
          <p className="mt-5 flex items-center gap-2 text-[13px] text-muted">
            <Loader2 className="size-4 animate-spin" /> {message}
          </p>
        )}
        {state === 'done' && (
          <p className="mt-5 flex items-center gap-2 rounded-app border border-accent-line bg-accent-soft px-3 py-2 text-[13px] text-accent-text">
            <ShieldCheck className="size-4" /> {message}
          </p>
        )}
        {state === 'error' && (
          <p className="mt-5 rounded-app bg-danger-bg px-3 py-2 text-[13px] text-danger">
            {message}
          </p>
        )}
      </div>
    </div>
  )
}
