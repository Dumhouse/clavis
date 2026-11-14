import { Check, Copy, Loader2, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'

import { api } from '@/api/client'
import type { components } from '@/api/schema'
import { Button } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/field'
import { Modal, ModalHeader } from '@/components/ui/modal'
import { apiErrorMessage } from '@/lib/api-error'
import { cn } from '@/lib/utils'

type Env = components['schemas']['EnvBody']
type Secret = components['schemas']['SecretBody']

const symbols = '!@#$%^&*()-_=+[]{}'
const letters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz'
const digits = '0123456789'

type Size = { min: number; max: number; default: number; unit: 'characters' | 'bytes' | 'digits' }

/** Every generator says exactly how its value is made. */
const types = {
  password: {
    label: 'Password',
    size: { min: 8, max: 128, default: 32, unit: 'characters' },
    how: (n: number) =>
      `${String(n)} characters drawn uniformly from A–Z, a–z, 0–9 and ${symbols} (80 symbols, ${(Math.log2(80) * n).toFixed(0)} bits).`,
  },
  token: {
    label: 'Random token (letters and digits)',
    size: { min: 16, max: 128, default: 40, unit: 'characters' },
    how: (n: number) =>
      `${String(n)} characters from A–Z, a–z, 0–9, no symbols, so it is safe in URLs and shells (${(Math.log2(62) * n).toFixed(0)} bits).`,
  },
  hex: {
    label: 'Hex',
    size: { min: 8, max: 128, default: 32, unit: 'bytes' },
    how: (n: number) =>
      `${String(n)} random bytes written as ${String(n * 2)} hex characters (0–9, a–f). Common for Rails secret_key_base (64 bytes) and session keys.`,
  },
  base64: {
    label: 'Base64url',
    size: { min: 8, max: 128, default: 32, unit: 'bytes' },
    how: (n: number) =>
      `${String(n)} random bytes, base64url-encoded without padding (${String(Math.ceil((n * 4) / 3))} characters).`,
  },
  jwt: {
    label: 'JWT signing key (HMAC)',
    size: { min: 32, max: 64, default: 32, unit: 'bytes' },
    how: (n: number) =>
      `The key your server uses to sign and verify JWTs with ${n >= 64 ? 'HS512' : n >= 48 ? 'HS384' : 'HS256'}, not a token itself: ${String(n)} random bytes, base64url-encoded. HS256 needs at least 32 bytes, HS384 48, HS512 64.`,
  },
  aes: {
    label: 'AES-256 key',
    size: null,
    how: () => '32 random bytes (256 bits), standard base64 with padding, 44 characters.',
  },
  uuid: {
    label: 'UUID v4',
    size: null,
    how: () => 'A random UUID (version 4): 122 random bits in the 8-4-4-4-12 hex format.',
  },
  pin: {
    label: 'Numeric code',
    size: { min: 4, max: 12, default: 6, unit: 'digits' },
    how: (n: number) => `${String(n)} digits, 0–9 (${(Math.log2(10) * n).toFixed(0)} bits).`,
  },
  ed25519: {
    label: 'Ed25519 key pair',
    size: null,
    how: () =>
      'An Ed25519 signing key pair. The private key is stored as a file (PKCS#8 PEM); the public key (SPKI PEM) is shown so you can copy it or save it too.',
  },
  rsa: {
    label: 'RSA key pair',
    size: null,
    how: () =>
      'An RSA key pair (exponent 65537) for RS256 signatures and TLS. The private key is stored as a file (PKCS#8 PEM); the public key (SPKI PEM) is shown so you can copy it or save it too.',
  },
} satisfies Record<string, { label: string; size: Size | null; how: (n: number) => string }>

type GenType = keyof typeof types
type Generated = { value: string; publicKey?: string }

/** Generator state: type, size, the current value, regenerated on change. */
function useGenerator(initial: GenType = 'password') {
  const [type, setTypeState] = useState<GenType>(initial)
  const [size, setSize] = useState(types[initial].size?.default ?? 32)
  const [rsaBits, setRsaBits] = useState(2048)
  const [generated, setGenerated] = useState<Generated | null>(null)
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const generate = useCallback(async () => {
    setGenerating(true)
    setError(null)
    try {
      setGenerated(await generateValue(type, size, rsaBits))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not generate a value.')
    } finally {
      setGenerating(false)
    }
  }, [type, size, rsaBits])

  // A value is ready as soon as the dialog opens and whenever the type or size changes.
  useEffect(() => {
    void generate()
  }, [generate])

  function setType(next: GenType) {
    setTypeState(next)
    const nextSize = types[next].size
    if (nextSize) setSize(nextSize.default)
  }

  const isKeyPair = type === 'ed25519' || type === 'rsa'
  return {
    type,
    setType,
    size,
    setSize,
    rsaBits,
    setRsaBits,
    generated,
    generating,
    error,
    generate,
    isKeyPair,
    kind: (isKeyPair ? 'file' : 'secret') as 'file' | 'secret',
    spec: types[type],
  }
}

type Generator = ReturnType<typeof useGenerator>

/** Type and length controls, the explanation, and the value boxes. */
function GeneratorFields({ gen, leading }: { gen: Generator; leading?: ReactNode }) {
  const strength = securityBits(gen.type, gen.size, gen.rsaBits)
  return (
    <>
      <div className="mt-4 grid gap-3 sm:grid-cols-2">
        {leading}
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-muted">Type</span>
          <Select
            value={gen.type}
            onChange={(event) => {
              gen.setType(event.target.value as GenType)
            }}
          >
            {(Object.keys(types) as GenType[]).map((candidate) => (
              <option key={candidate} value={candidate}>
                {types[candidate].label}
              </option>
            ))}
          </Select>
        </label>
        {gen.spec.size ? (
          <label className="block">
            <span className="mb-1 flex justify-between text-xs font-medium text-muted">
              <span>Length</span>
              <span className="font-mono text-dim">
                {gen.size} {gen.spec.size.unit}
              </span>
            </span>
            <div className="flex h-9 items-center gap-2">
              <input
                type="range"
                min={gen.spec.size.min}
                max={gen.spec.size.max}
                step={gen.type === 'jwt' ? 16 : 1}
                value={gen.size}
                className="w-full accent-[var(--color-accent)]"
                onChange={(event) => {
                  gen.setSize(Number(event.target.value))
                }}
              />
            </div>
          </label>
        ) : gen.type === 'rsa' ? (
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-muted">Key size</span>
            <Select
              value={String(gen.rsaBits)}
              onChange={(event) => {
                gen.setRsaBits(Number(event.target.value))
              }}
            >
              <option value="2048">2048 bits</option>
              <option value="3072">3072 bits</option>
              <option value="4096">4096 bits</option>
            </Select>
          </label>
        ) : (
          <div />
        )}
      </div>

      <p className="mt-3 rounded-app-sm bg-panel px-3 py-2 text-xs leading-5 text-muted">
        {gen.spec.how(gen.size)}
      </p>

      <ValueBox
        label={gen.isKeyPair ? 'Private key' : 'Value'}
        value={gen.generated?.value ?? ''}
        generating={gen.generating}
        onRegenerate={() => {
          void gen.generate()
        }}
        footer={
          <div className="flex items-center gap-2">
            <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-elev">
              <div
                className={cn(
                  'h-full rounded-full',
                  strength < 64 ? 'bg-danger' : strength < 112 ? 'bg-amber-fg' : 'bg-accent-text',
                )}
                style={{ width: `${String(Math.min(100, (strength / 256) * 100))}%` }}
              />
            </div>
            <span className="text-right font-mono text-2xs text-dim">
              {strength} bits of security
            </span>
          </div>
        }
      />
      {gen.generated?.publicKey && <ValueBox label="Public key" value={gen.generated.publicKey} />}
      {gen.error && (
        <p className="mt-2 rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">
          {gen.error}
        </p>
      )}
    </>
  )
}

/**
 * Generate a value and hand it back (Add secret, Replace value, the grid's
 * inline edit). Nothing is stored here.
 */
export function GenerateValueDialog({
  onUse,
  onClose,
}: {
  /** kind is 'file' for key pairs. */
  onUse: (value: string, kind: 'secret' | 'file') => void
  onClose: () => void
}) {
  const gen = useGenerator()
  return (
    <Modal onClose={onClose} className="max-w-2xl">
      <ModalHeader
        title="Generate a value"
        description="Made in your browser with the Web Crypto API (crypto.getRandomValues). Use it to fill the value field; nothing is saved yet."
        onClose={onClose}
      />
      <GeneratorFields gen={gen} />
      {gen.isKeyPair && (
        <p className="mt-2 text-xs text-dim">
          The private key is used as the value. Copy the public key now if you need it.
        </p>
      )}
      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="primary"
          size="sm"
          disabled={!gen.generated || gen.generating}
          onClick={() => {
            if (gen.generated) onUse(gen.generated.value, gen.kind)
          }}
        >
          <Check className="size-3.5" /> Use this value
        </Button>
      </div>
    </Modal>
  )
}

/** Generate a value and save it as a key in one step. */
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
  const gen = useGenerator()
  const [env, setEnv] = useState(envs[0]?.slug ?? '')
  const [key, setKey] = useState('')
  const [savePublic, setSavePublic] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { generated, kind } = gen
  const trimmedKey = key.trim()
  const publicKeyName = `${trimmedKey.replace(/_?(PRIVATE_KEY|KEY)$/, '') || trimmedKey}_PUBLIC_KEY`
  const exists = useMemo(
    () => Boolean(env && secretsForEnv(env).some((secret) => secret.key === trimmedKey)),
    [env, trimmedKey, secretsForEnv],
  )

  async function save() {
    if (!trimmedKey || !generated || !env) return
    setBusy(true)
    setError(null)
    const message = `Generated (${gen.spec.label}) in the web UI`
    const write = async (
      name: string,
      value: string,
      writeKind: 'secret' | 'file' | 'variable',
    ) => {
      const present = secretsForEnv(env).some((secret) => secret.key === name)
      const body = { value, kind: writeKind, message }
      return present
        ? api.PUT('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}', {
            params: { path: { org: orgSlug, project: projectSlug, env, key: name } },
            body,
          })
        : api.POST('/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets', {
            params: { path: { org: orgSlug, project: projectSlug, env } },
            body: { key: name, ...body },
          })
    }
    const result = await write(trimmedKey, generated.value, kind)
    if (result.error) {
      setBusy(false)
      setError(apiErrorMessage(result.error, 'Could not store the generated value.'))
      return
    }
    if (generated.publicKey && savePublic) {
      const pub = await write(publicKeyName, generated.publicKey, 'variable')
      if (pub.error) {
        setBusy(false)
        setError(
          `${trimmedKey} was saved, but ${publicKeyName} failed: ${apiErrorMessage(pub.error, 'error')}`,
        )
        return
      }
    }
    setBusy(false)
    onDone(
      `${exists ? 'Updated' : 'Created'} ${trimmedKey}` +
        (generated.publicKey && savePublic ? ` and ${publicKeyName}.` : '.'),
    )
  }

  return (
    <Modal onClose={onClose} className="max-w-2xl">
      <ModalHeader
        title="Generate a secret"
        description="Made in your browser with the Web Crypto API (crypto.getRandomValues). Nothing is stored until you save."
        onClose={onClose}
      />
      <GeneratorFields
        gen={gen}
        leading={
          <>
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Key</span>
              <Input
                autoFocus
                mono
                placeholder={gen.isKeyPair ? 'JWT_PRIVATE_KEY' : 'SESSION_SECRET'}
                value={key}
                onChange={(event) => {
                  setKey(event.target.value.toUpperCase().replace(/\s/g, '_'))
                }}
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-muted">Environment</span>
              <Select
                value={env}
                onChange={(event) => {
                  setEnv(event.target.value)
                }}
              >
                {envs.map((candidate) => (
                  <option key={candidate.id} value={candidate.slug}>
                    {candidate.name}
                  </option>
                ))}
              </Select>
            </label>
          </>
        }
      />

      {generated?.publicKey && (
        <label className="mt-2 flex items-center gap-2 text-xs text-muted">
          <input
            type="checkbox"
            checked={savePublic}
            onChange={(event) => {
              setSavePublic(event.target.checked)
            }}
          />
          Also save the public key as{' '}
          <span className="font-mono text-text">{trimmedKey ? publicKeyName : '…_PUBLIC_KEY'}</span>{' '}
          (a variable: public keys aren't secret)
        </label>
      )}

      {error && (
        <p className="mt-2 rounded-app-sm bg-danger-bg px-2.5 py-2 text-xs text-danger">{error}</p>
      )}
      <p className="mt-3 text-xs text-dim">
        Saved as a {kind}
        {exists
          ? `. ${trimmedKey} already exists in ${env}; saving adds a new version.`
          : trimmedKey
            ? ` in ${env}.`
            : '.'}
      </p>

      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button
          variant="primary"
          size="sm"
          disabled={busy || !trimmedKey || !generated || !env}
          onClick={() => {
            void save()
          }}
        >
          {busy && <Loader2 className="size-3.5 animate-spin" />} {exists ? 'Update' : 'Save'}
        </Button>
      </div>
    </Modal>
  )
}

function ValueBox({
  label,
  value,
  generating,
  onRegenerate,
  footer,
}: {
  label: string
  value: string
  generating?: boolean
  onRegenerate?: () => void
  footer?: ReactNode
}) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="mt-3 rounded-app-sm border border-border bg-bg">
      <div className="flex items-center gap-1 border-b border-border-soft px-3 py-1.5">
        <span className="text-2xs font-semibold uppercase tracking-[0.05em] text-dim">{label}</span>
        <span className="ml-1 font-mono text-2xs text-dim">{value.length} chars</span>
        <div className="ml-auto flex gap-1">
          {onRegenerate && (
            <Button variant="ghost" size="sm" disabled={generating} onClick={onRegenerate}>
              {generating ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <RefreshCw className="size-3.5" />
              )}
              New value
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            disabled={!value}
            onClick={() => {
              void navigator.clipboard.writeText(value).then(() => {
                setCopied(true)
                window.setTimeout(() => {
                  setCopied(false)
                }, 1500)
              })
            }}
          >
            {copied ? (
              <Check className="size-3.5 text-accent-text" />
            ) : (
              <Copy className="size-3.5" />
            )}
            Copy
          </Button>
        </div>
      </div>
      <pre
        className={cn(
          'max-h-48 overflow-auto px-3 py-2.5 font-mono text-xs leading-5 text-text select-all',
          // PEM keep their 64-character lines; single-line values wrap.
          value.includes('\n') ? 'whitespace-pre' : 'whitespace-pre-wrap break-all',
        )}
      >
        {value || ' '}
      </pre>
      {footer && <div className="border-t border-border-soft px-3 py-2">{footer}</div>}
    </div>
  )
}

async function generateValue(type: GenType, size: number, rsaBits: number): Promise<Generated> {
  switch (type) {
    case 'password':
      return { value: randomString(letters + digits + symbols, size) }
    case 'token':
      return { value: randomString(letters + digits, size) }
    case 'pin':
      return { value: randomString(digits, size) }
    case 'hex':
      return {
        value: [...randomBytes(size)].map((byte) => byte.toString(16).padStart(2, '0')).join(''),
      }
    case 'base64':
    case 'jwt':
      return { value: base64url(randomBytes(size)) }
    case 'aes':
      return { value: btoa(String.fromCharCode(...randomBytes(32))) }
    case 'uuid':
      return { value: crypto.randomUUID() }
    case 'ed25519': {
      const pair = (await crypto.subtle.generateKey({ name: 'Ed25519' }, true, [
        'sign',
        'verify',
      ])) as CryptoKeyPair
      return {
        value: await privatePem(pair.privateKey),
        publicKey: await publicPem(pair.publicKey),
      }
    }
    case 'rsa': {
      const pair = (await crypto.subtle.generateKey(
        {
          name: 'RSASSA-PKCS1-v1_5',
          modulusLength: rsaBits,
          publicExponent: new Uint8Array([1, 0, 1]),
          hash: 'SHA-256',
        },
        true,
        ['sign', 'verify'],
      )) as CryptoKeyPair
      return {
        value: await privatePem(pair.privateKey),
        publicKey: await publicPem(pair.publicKey),
      }
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
      out += alphabet.charAt(byte % alphabet.length)
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

function pem(label: string, der: ArrayBuffer): string {
  let binary = ''
  for (const byte of new Uint8Array(der)) binary += String.fromCharCode(byte)
  const lines = btoa(binary).match(/.{1,64}/g) ?? []
  return `-----BEGIN ${label}-----\n${lines.join('\n')}\n-----END ${label}-----\n`
}

async function privatePem(key: CryptoKey): Promise<string> {
  return pem('PRIVATE KEY', await crypto.subtle.exportKey('pkcs8', key))
}

async function publicPem(key: CryptoKey): Promise<string> {
  return pem('PUBLIC KEY', await crypto.subtle.exportKey('spki', key))
}

/** Bits of security: entropy for random values, the NIST level for key pairs. */
function securityBits(type: GenType, size: number, rsaBits: number): number {
  switch (type) {
    case 'password':
      return Math.round(Math.log2(80) * size)
    case 'token':
      return Math.round(Math.log2(62) * size)
    case 'pin':
      return Math.round(Math.log2(10) * size)
    case 'hex':
    case 'base64':
    case 'jwt':
      return size * 8
    case 'aes':
      return 256
    case 'uuid':
      return 122
    case 'ed25519':
      return 128
    case 'rsa':
      return rsaBits >= 4096 ? 140 : rsaBits >= 3072 ? 128 : 112
  }
}
