import { useMemo, useState } from 'react'
import { Check, Copy, Loader2, RefreshCw, X } from 'lucide-react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { cn } from '@/lib/utils'
import { Input, Select } from '@/components/ui/field'

type Env = components['schemas']['EnvBody']
type Secret = components['schemas']['SecretBody']

const types = [
  { value: 'password', label: 'Password', bitsPerChar: 6.3 },
  { value: 'apikey', label: 'API key', bitsPerChar: 5.95 },
  { value: 'hex', label: 'Hex', bitsPerChar: 4 },
  { value: 'base64', label: 'Base64', bitsPerChar: 6 },
  { value: 'uuid', label: 'UUID', bitsPerChar: 0 },
  { value: 'jwt', label: 'JWT secret', bitsPerChar: 0 },
  { value: 'ed25519', label: 'Ed25519 keypair', bitsPerChar: 0 },
  { value: 'rsa', label: 'RSA-2048 keypair', bitsPerChar: 0 },
] as const

type GenType = (typeof types)[number]['value']

export function GenerateSecretDialog({
  orgSlug,
  projectSlug,
  envs,
  secretsForEnv,
  onClose,
  onDone,
}: {
  orgSlug: string
  projectSlug: string
  envs: Env[]
  secretsForEnv: (env: string) => Secret[]
  onClose: () => void
  onDone: (message: string) => void
}) {
  const [env, setEnv] = useState(envs[0]?.slug ?? '')
  const [key, setKey] = useState('')
  const [type, setType] = useState<GenType>('password')
  const [length, setLength] = useState(32)
  const [value, setValue] = useState('')
  const [busy, setBusy] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const exists = useMemo(
    () => Boolean(env && secretsForEnv(env).some((secret) => secret.key === key.trim())),
    [env, key, secretsForEnv],
  )
  const selectedType = types.find((candidate) => candidate.value === type) ?? types[0]
  const entropy = entropyBits(type, length, value)

  async function generate() {
    setGenerating(true)
    setError(null)
    try {
      setValue(await generateValue(type, length))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not generate a value.')
    } finally {
      setGenerating(false)
    }
  }

  async function copy() {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    window.setTimeout(() => {
      setCopied(false)
    }, 1500)
  }

  async function save() {
    if (!key.trim() || !value || !env) return
    setBusy(true)
    setError(null)
    const payload = {
      value,
      kind: 'secret' as const,
      message: `Generated ${type} with the web UI`,
    }
    const result = exists
      ? await api.PUT('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}', {
          params: { path: { org: orgSlug, project: projectSlug, env, key: key.trim() } },
          body: payload,
        })
      : await api.POST('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets', {
          params: { path: { org: orgSlug, project: projectSlug, env } },
          body: { key: key.trim(), ...payload },
        })
    setBusy(false)
    if (result.error) {
      setError(apiErrorMessage(result.error, 'Could not store the generated secret.'))
      return
    }
    onDone(`${exists ? 'Updated' : 'Created'} ${key.trim()}.`)
  }

  return (
    <Modal onClose={onClose} className="max-w-xl">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="font-head text-lg font-semibold">Generate a secret</h2>
          <p className="mt-0.5 text-sm text-muted">
            Generated locally with the Web Crypto API. Nothing is stored until you save.
          </p>
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
          <X className="size-4" />
        </Button>
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-2">
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Key</span>
          <Input
            autoFocus
            className="h-9 font-mono"
            placeholder="STRIPE_WEBHOOK_SECRET"
            value={key}
            onChange={(event) => {
              setKey(event.target.value)
            }}
          />
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Environment</span>
          <Select
            className="h-9"
            value={env}
            onChange={(event) => {
              setEnv(event.target.value)
            }}
          >
            {envs.map((candidate) => (
              <option key={candidate.id} value={candidate.slug}>
                {candidate.name} ({candidate.slug})
              </option>
            ))}
          </Select>
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Type</span>
          <Select
            className="h-9"
            value={type}
            onChange={(event) => {
              setType(event.target.value as GenType)
            }}
          >
            {types.map((candidate) => (
              <option key={candidate.value} value={candidate.value}>
                {candidate.label}
              </option>
            ))}
          </Select>
        </label>
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">
            Length {selectedType.bitsPerChar === 0 || type === 'rsa' ? '' : `(${String(length)})`}
          </span>
          <input
            type="range"
            min={8}
            max={128}
            value={length}
            disabled={selectedType.bitsPerChar === 0 || type === 'rsa'}
            className="w-full accent-[var(--color-accent)]"
            onChange={(event) => {
              setLength(Number(event.target.value))
            }}
          />
        </label>
      </div>

      <div className="mt-4 rounded-app-sm border border-border bg-bg p-3">
        <div className="flex items-center gap-2">
          <code className="min-h-10 flex-1 break-all font-mono text-xs text-text">
            {value || <span className="text-dim">Generate a value to preview it.</span>}
          </code>
          <Button
            variant="secondary"
            size="sm"
            disabled={generating}
            onClick={() => {
              void generate()
            }}
          >
            {generating ? (
              <Loader2 className="size-3.5 animate-spin" />
            ) : (
              <RefreshCw className="size-3.5" />
            )}
            Generate
          </Button>
          {value && (
            <Button
              variant="ghost"
              size="icon"
              onClick={() => {
                void copy()
              }}
              aria-label="Copy value"
            >
              {copied ? (
                <Check className="size-3.5 text-accent-text" />
              ) : (
                <Copy className="size-3.5" />
              )}
            </Button>
          )}
        </div>
        <div className="mt-3 flex items-center gap-2">
          <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-elev">
            <div
              className={cn(
                'h-full rounded-full',
                entropy < 64 ? 'bg-danger' : entropy < 128 ? 'bg-amber-fg' : 'bg-accent-text',
              )}
              style={{ width: `${String(Math.min(100, (entropy / 256) * 100))}%` }}
            />
          </div>
          <span className="w-20 text-right font-mono text-2xs text-dim">
            {Math.round(entropy)} bits
          </span>
        </div>
      </div>

      {error && (
        <p className="mt-2 rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
      )}
      <p className="mt-2 text-xs text-muted">
        {exists
          ? 'This key exists in the environment; saving creates a new version.'
          : 'The key will be created in the selected environment.'}
      </p>

      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="primary"
          size="sm"
          disabled={busy || !key.trim() || !value || !env}
          onClick={() => {
            void save()
          }}
        >
          {busy && <Loader2 className="size-3.5 animate-spin" />} {exists ? 'Update' : 'Create'}
        </Button>
      </div>
    </Modal>
  )
}

async function generateValue(type: GenType, length: number): Promise<string> {
  switch (type) {
    case 'password':
      return randomString(
        'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*()-_=+[]{}',
        length,
      )
    case 'apikey':
      return randomString('ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789', length)
    case 'hex':
      return [...randomBytes(Math.ceil(length / 2))]
        .map((byte) => byte.toString(16).padStart(2, '0'))
        .join('')
        .slice(0, length)
    case 'base64':
      return base64url(randomBytes(length))
    case 'uuid':
      return crypto.randomUUID()
    case 'jwt':
      return base64url(randomBytes(32))
    case 'ed25519': {
      const pair = (await crypto.subtle.generateKey({ name: 'Ed25519' }, true, [
        'sign',
        'verify',
      ])) as CryptoKeyPair
      return pemFromKey(pair.privateKey)
    }
    case 'rsa': {
      const pair = (await crypto.subtle.generateKey(
        {
          name: 'RSA-OAEP',
          modulusLength: 2048,
          publicExponent: new Uint8Array([1, 0, 1]),
          hash: 'SHA-256',
        },
        true,
        ['encrypt', 'decrypt'],
      )) as CryptoKeyPair
      return pemFromKey(pair.privateKey)
    }
  }
}

function randomBytes(length: number): Uint8Array {
  return crypto.getRandomValues(new Uint8Array(length))
}

function randomString(alphabet: string, length: number): string {
  // Rejection sampling: discard bytes that would bias the modulo.
  const limit = 256 - (256 % alphabet.length)
  let out = ''
  while (out.length < length) {
    for (const byte of randomBytes(length * 2)) {
      if (byte >= limit) continue
      out += alphabet[byte % alphabet.length]
      if (out.length === length) break
    }
  }
  return out
}

function base64url(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '')
}

async function pemFromKey(key: CryptoKey): Promise<string> {
  const der = await crypto.subtle.exportKey('pkcs8', key)
  const base64 = btoa(String.fromCharCode(...new Uint8Array(der)))
  const lines = base64.match(/.{1,64}/g) ?? []
  return `-----BEGIN PRIVATE KEY-----\n${lines.join('\n')}\n-----END PRIVATE KEY-----\n`
}

function entropyBits(type: GenType, length: number, value: string): number {
  if (!value) return 0
  if (type === 'uuid') return 122
  if (type === 'jwt') return 256
  if (type === 'ed25519') return 128
  if (type === 'rsa') return 112
  const bitsPerChar = types.find((candidate) => candidate.value === type)?.bitsPerChar ?? 0
  return bitsPerChar * length
}
