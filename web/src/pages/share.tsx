import { useEffect, useState } from 'react'
import { Copy, KeyRound, Loader2, ShieldCheck } from 'lucide-react'

import { api } from '@/api/client'
import { Button } from '@/components/ui/button'
import { apiErrorMessage } from '@/lib/api-error'

export function SharePage() {
  const parts = window.location.pathname.split('/')
  const id = parts[parts.length - 1] ?? ''
  const fragment = window.location.hash.replace(/^#/, '')
  const [password, setPassword] = useState('')
  const [needsPassword, setNeedsPassword] = useState(false)
  const [value, setValue] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!fragment) {
      setError('This link is missing its decryption key.')
      return
    }
    void claim('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function claim(candidate: string) {
    if (!id) {
      setError('Invalid share link.')
      return
    }
    setBusy(true)
    setError(null)
    const result = await api.POST('/api/v1/shares/{id}/claim', {
      params: { path: { id } },
      body: { password: candidate },
    })
    setBusy(false)
    if (result.error || !result.data) {
      if (result.error?.status === 403) {
        setNeedsPassword(true)
        setError(candidate ? 'That password is not correct.' : 'This link is password protected.')
        return
      }
      setError(apiErrorMessage(result.error, 'This share link is expired, used, or invalid.'))
      return
    }
    try {
      const plaintext = await decrypt(fragment, result.data.ciphertext, result.data.nonce)
      setValue(plaintext)
      setNeedsPassword(false)
    } catch {
      setError('Could not decrypt this link. Check that the URL is complete.')
    }
  }

  async function copy() {
    if (!value) return
    await navigator.clipboard.writeText(value)
    setCopied(true)
    window.setTimeout(() => {
      setCopied(false)
    }, 1500)
  }

  return (
    <div className="flex min-h-full items-center justify-center bg-bg px-4 py-10 text-text">
      <div className="w-full max-w-2xl rounded-app border border-border bg-surface p-6 shadow-app">
        <div className="flex items-center gap-2">
          <div className="logo-mark flex size-8 items-center justify-center rounded-[10px]">
            <KeyRound className="size-4 text-[#06241c]" strokeWidth={2.6} />
          </div>
          <div>
            <h1 className="font-head text-[19px] font-semibold">Clavis share link</h1>
            <p className="text-[12.5px] text-muted">
              Decryption happens in your browser. The server only sees ciphertext.
            </p>
          </div>
        </div>

        {needsPassword && (
          <form
            className="mt-5 flex gap-2"
            onSubmit={(event) => {
              event.preventDefault()
              void claim(password)
            }}
          >
            <input
              autoFocus
              type="password"
              className="h-9 flex-1 rounded-app-sm border border-border bg-bg px-2.5 text-[13.5px] text-text focus:border-accent-line focus:outline-none"
              placeholder="Password"
              value={password}
              onChange={(event) => {
                setPassword(event.target.value)
              }}
            />
            <Button type="submit" variant="primary" size="md" disabled={busy}>
              {busy && <Loader2 className="size-4 animate-spin" />} Unlock
            </Button>
          </form>
        )}

        {busy && !value && (
          <p className="mt-5 flex items-center gap-2 text-[13px] text-muted">
            <Loader2 className="size-4 animate-spin" /> Claiming…
          </p>
        )}
        {error && (
          <p className="mt-5 rounded-app bg-danger-bg px-3 py-2 text-[13px] text-danger">{error}</p>
        )}

        {value !== null && (
          <div className="mt-5">
            <div className="flex items-center justify-between gap-2">
              <span className="flex items-center gap-1.5 text-[12.5px] font-medium text-accent-text">
                <ShieldCheck className="size-3.5" /> Decrypted locally
              </span>
              <Button variant="secondary" size="sm" onClick={() => void copy()}>
                <Copy className="size-3.5" /> {copied ? 'Copied' : 'Copy'}
              </Button>
            </div>
            <pre className="mt-2 max-h-[55vh] overflow-auto whitespace-pre-wrap break-all rounded-app-sm border border-border bg-bg p-3 font-mono text-[12.5px] text-text">
              {value}
            </pre>
            <p className="mt-2 text-[12px] text-dim">
              This link may now be used up. Do not paste the value into untrusted tools.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}

async function decrypt(fragment: string, ciphertext: string, nonce: string): Promise<string> {
  const keyBytes = base64urlDecode(fragment)
  const nonceBytes = base64urlDecode(nonce)
  const ciphertextBytes = base64urlDecode(ciphertext)
  const key = await crypto.subtle.importKey('raw', bufferOf(keyBytes), 'AES-GCM', false, [
    'decrypt',
  ])
  const plaintext = await crypto.subtle.decrypt(
    { name: 'AES-GCM', iv: bufferOf(nonceBytes) },
    key,
    bufferOf(ciphertextBytes),
  )
  return new TextDecoder().decode(plaintext)
}

function bufferOf(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
}

function base64urlDecode(value: string): Uint8Array {
  const padded = value.replaceAll('-', '+').replaceAll('_', '/')
  const binary = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4))
  return Uint8Array.from(binary, (char) => char.charCodeAt(0))
}
