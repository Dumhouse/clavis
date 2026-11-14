import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { startRegistration } from '@simplewebauthn/browser'
import { Fingerprint, Loader2, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'
import {
  passkeyCertificateWarning,
  passkeyErrorMessage,
  passkeyOptions,
  passkeyUnavailableReason,
} from '@/lib/passkeys'
import { useSelfSignedFingerprint } from '@/lib/server-certificate'
import { confirmAction } from '@/components/ui/dialogs'
import { formatDate as formatDay } from '@/lib/format'

type Passkey = components['schemas']['PasskeyBody']

export function Passkeys() {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const selfSigned = Boolean(useSelfSignedFingerprint())
  const unavailable = passkeyUnavailableReason(selfSigned)
  const warning = passkeyCertificateWarning(selfSigned)

  const passkeys = useQuery({
    queryKey: ['passkeys'],
    queryFn: async () => {
      const result = await api.GET('/api/v1/auth/passkeys')
      if (result.error) throw new Error(apiErrorMessage(result.error, 'Could not load passkeys.'))
      return result.data?.passkeys ?? []
    },
  })

  const remove = useMutation({
    mutationFn: async (passkey: Passkey) => {
      if (!(await confirmAction(`Remove the passkey “${passkey.name}”?`))) return
      const result = await api.DELETE('/api/v1/auth/passkeys/{id}', {
        params: { path: { id: passkey.id } },
      })
      if (result.error)
        throw new Error(apiErrorMessage(result.error, 'Could not remove the passkey.'))
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['passkeys'] })
    },
    onError: (cause: unknown) => {
      setError(cause instanceof Error ? cause.message : 'Could not remove the passkey.')
    },
  })

  async function add() {
    setBusy(true)
    setError(null)
    try {
      const begin = await api.POST('/api/v1/auth/passkeys/register/begin')
      if (begin.error || !begin.data) {
        throw new Error(apiErrorMessage(begin.error, 'Could not start passkey enrollment.'))
      }
      const credential = await startRegistration({
        optionsJSON: passkeyOptions<Parameters<typeof startRegistration>[0]['optionsJSON']>(
          begin.data.options,
        ),
      })
      const finish = await api.POST('/api/v1/auth/passkeys/register/finish', {
        body: {
          challenge_id: begin.data.challenge_id,
          name: `Passkey ${formatDay(new Date())}`,
          credential,
        },
      })
      if (finish.error) {
        throw new Error(apiErrorMessage(finish.error, 'Could not save the passkey.'))
      }
      await queryClient.invalidateQueries({ queryKey: ['passkeys'] })
    } catch (cause) {
      setError(passkeyErrorMessage(cause, 'Could not enroll a passkey.', selfSigned))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="rounded-app border border-border bg-surface">
      <div className="flex items-center gap-2 border-b border-border px-4 py-3">
        <Fingerprint className="size-4 text-muted" />
        <h2 className="text-base font-semibold">Passkeys</h2>
        <Button
          variant="secondary"
          size="sm"
          className="ml-auto"
          disabled={busy || unavailable !== null}
          title={unavailable ?? undefined}
          onClick={() => {
            void add()
          }}
        >
          {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Plus className="size-3.5" />}
          Add passkey
        </Button>
      </div>
      <p className="border-b border-border-soft px-4 py-2 text-xs text-dim">
        Passkeys sign you in without a password and satisfy two-factor requirements.
      </p>
      {(unavailable ?? warning) && (
        <p className="border-b border-border-soft bg-amber-bg px-4 py-2 text-xs text-amber-fg">
          {unavailable ?? warning}
        </p>
      )}
      {error && <p className="px-4 py-3 text-xs text-danger">{error}</p>}
      <div className="divide-y divide-border-soft">
        {(passkeys.data ?? []).map((passkey) => (
          <div key={passkey.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{passkey.name}</p>
              <p className="text-xs text-dim">
                Added {formatDay(passkey.created_at)} · last used{' '}
                {passkey.last_used_at ? formatDay(passkey.last_used_at) : 'never'}
              </p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              aria-label={`Remove ${passkey.name}`}
              disabled={remove.isPending}
              onClick={() => {
                remove.mutate(passkey)
              }}
            >
              <Trash2 className="size-3.5" />
            </Button>
          </div>
        ))}
        {(passkeys.data?.length ?? 0) === 0 && !passkeys.isLoading && (
          <p className="px-4 py-5 text-sm text-muted">
            No passkeys yet. Add one to sign in without typing a password.
          </p>
        )}
        {passkeys.isLoading && (
          <p className="flex items-center gap-2 px-4 py-5 text-sm text-muted">
            <Loader2 className="size-4 animate-spin" /> Loading passkeys…
          </p>
        )}
      </div>
    </section>
  )
}
