import { api } from '@/api/client'
import { apiErrorMessage } from '@/lib/api-error'

type Cached = { token: string; expiresAt: number }

let cached: Cached | null = null

function usable(): string | null {
  if (!cached) return null
  if (Date.now() > cached.expiresAt - 15_000) return null
  return cached.token
}

/** Obtain a short-lived token for one sensitive action. */
export async function reauth(password: string): Promise<string> {
  const result = await api.POST('/api/v1/auth/reauthenticate', { body: { password } })
  if (result.error || !result.data) {
    throw new Error(apiErrorMessage(result.error, 'Could not confirm your password.'))
  }
  cached = {
    token: result.data.reauth_token,
    expiresAt: new Date(result.data.expires_at).getTime(),
  }
  return cached.token
}

/**
 * Return a cached confirmation token or prompt for the password. Sensitive
 * actions call this immediately before the request.
 */
export async function ensureReauth(): Promise<string> {
  const current = usable()
  if (current) return current
  const password = window.prompt('Confirm your password to continue')
  if (!password) throw new Error('Password confirmation is required.')
  return reauth(password)
}

/** Drop the cached token (for example after a failed request). */
export function clearReauth(): void {
  cached = null
}
