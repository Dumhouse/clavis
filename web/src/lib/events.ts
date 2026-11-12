import { useEffect, useRef } from 'react'

export type OrgEvent = {
  type: string
  project?: string
  environment?: string
  at?: string
}

const eventTypes = ['secrets.changed', 'projects.changed', 'environments.changed'] as const

/**
 * Subscribes to an organization's SSE stream. The callback is kept in a ref so
 * re-renders do not tear down the connection.
 */
export function useOrgEvents(orgSlug: string | undefined, onEvent: (event: OrgEvent) => void) {
  const callback = useRef(onEvent)

  useEffect(() => {
    callback.current = onEvent
  }, [onEvent])

  useEffect(() => {
    if (!orgSlug) return
    const source = new EventSource(`/api/v1/orgs/${encodeURIComponent(orgSlug)}/events`)
    const handler = (message: MessageEvent) => {
      try {
        const raw = typeof message.data === 'string' ? message.data : ''
        callback.current(JSON.parse(raw) as OrgEvent)
      } catch {
        // Ignore malformed events; the next poll/refetch still converges.
      }
    }
    for (const type of eventTypes) {
      source.addEventListener(type, handler)
    }
    return () => {
      for (const type of eventTypes) {
        source.removeEventListener(type, handler)
      }
      source.close()
    }
  }, [orgSlug])
}
