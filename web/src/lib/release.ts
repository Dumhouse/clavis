import { useQuery } from '@tanstack/react-query'

import { api } from '@/api/client'
import { releaseTag } from '@/lib/release-tag'

export { releaseTag }

/** The release tag for this server (shares the hint bar's version query). */
export function useReleaseTag(): string {
  const { data } = useQuery({
    queryKey: ['version'],
    queryFn: async () => {
      const result = await api.GET('/api/v1/version')
      return result.data ?? null
    },
    staleTime: Infinity,
  })
  return releaseTag(data?.version)
}
