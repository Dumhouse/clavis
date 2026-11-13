import { useQuery } from '@tanstack/react-query'

import { api } from '@/api/client'

/**
 * The SHA-256 of the server's self-signed certificate, or undefined when the
 * server has a certificate the system already trusts (or no HTTPS). The CLI
 * shows the same fingerprint when `clavis login` asks to trust the server.
 */
export function useSelfSignedFingerprint(): string | undefined {
  const { data } = useQuery({
    queryKey: ['setup', 'status'],
    queryFn: async () => {
      const result = await api.GET('/api/v1/setup/status')
      return result.data ?? null
    },
    staleTime: 5 * 60_000,
  })
  return data?.self_signed_fingerprint
}
