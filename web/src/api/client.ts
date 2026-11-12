import createClient from 'openapi-fetch'

import type { paths } from '@/api/schema'

/**
 * Typed API client generated from the server's OpenAPI 3.1 document.
 * Regenerate with `make gen-api` after changing an API route.
 */
export const api = createClient<paths>({
  baseUrl: '/',
  credentials: 'same-origin',
  headers: { Accept: 'application/json' },
})

const safeMethods = new Set(['GET', 'HEAD', 'OPTIONS'])

/** Reads the double-submit CSRF cookie (dev and production names). */
export function csrfToken(): string {
  const raw = document.cookie
    .split('; ')
    .find((row) => row.startsWith('clavis_csrf=') || row.startsWith('__Host-clavis_csrf='))
  if (!raw) return ''
  return decodeURIComponent(raw.slice(raw.indexOf('=') + 1))
}

api.use({
  onRequest({ request }) {
    if (!safeMethods.has(request.method)) {
      const token = csrfToken()
      if (token) request.headers.set('X-CSRF-Token', token)
    }
    return request
  },
})
