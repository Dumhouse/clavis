import { useQueryClient } from '@tanstack/react-query'
import {
  BookOpen,
  CircleHelp,
  ClipboardCopy,
  Keyboard,
  LifeBuoy,
  MessageCircle,
} from 'lucide-react'

import { api } from '@/api/client'
import { Menu } from '@/components/ui/menu'
import { toast } from '@/components/ui/toast'
import { useCurrentOrg } from '@/lib/org'

const repo = 'https://github.com/dumhouse/clavis'

/**
 * The header's Help menu: where to read, where to ask, and a button that
 * copies what a bug report needs. The diagnostics hold versions and the
 * browser, never secrets, tokens or the page's address.
 */
export function HelpMenu({ onShortcuts, onDocs }: { onShortcuts: () => void; onDocs: () => void }) {
  const queryClient = useQueryClient()
  const { org } = useCurrentOrg()

  async function copyDiagnostics() {
    const [version, update] = await Promise.all([
      queryClient.fetchQuery({
        queryKey: ['version'],
        staleTime: Infinity,
        queryFn: async () => (await api.GET('/api/v1/version')).data ?? null,
      }),
      queryClient
        .fetchQuery({
          queryKey: ['system', 'update'],
          staleTime: 60 * 60 * 1000,
          queryFn: async () => (await api.GET('/api/v1/system/update')).data ?? null,
        })
        .catch(() => null),
    ])
    const lines = [
      `Clavis ${version?.version ?? 'unknown'} (commit ${version?.commit?.slice(0, 7) ?? 'unknown'}, built ${version?.built ?? 'unknown'})`,
      `Installed with: ${update?.install.label ?? 'unknown'}`,
      `Newest release: ${update?.latest || 'unknown'}`,
      `Your role: ${org?.role ?? 'unknown'}`,
      `Browser: ${navigator.userAgent}`,
      `Screen: ${String(window.innerWidth)}x${String(window.innerHeight)}`,
      `Time: ${new Date().toISOString()}`,
    ]
    try {
      await navigator.clipboard.writeText(lines.join('\n'))
      toast('Diagnostics copied. Paste them into your bug report.')
    } catch {
      toast('Could not copy. Your browser blocked clipboard access.', { tone: 'error' })
    }
  }

  const open = (url: string) => {
    window.open(url, '_blank', 'noopener,noreferrer')
  }

  return (
    <Menu
      label="Help"
      triggerClassName="flex size-8 items-center justify-center rounded-app-sm text-muted hover:bg-hover hover:text-text"
      trigger={<CircleHelp className="size-4" />}
      items={[
        { label: 'Documentation', icon: <BookOpen />, onSelect: onDocs },
        { label: 'Keyboard shortcuts', icon: <Keyboard />, hint: '?', onSelect: onShortcuts },
        'separator',
        {
          label: 'Ask a question',
          icon: <MessageCircle />,
          description: 'GitHub Discussions',
          onSelect: () => {
            open(`${repo}/discussions`)
          },
        },
        {
          label: 'Report a problem',
          icon: <LifeBuoy />,
          description: 'GitHub Issues',
          onSelect: () => {
            open(`${repo}/issues/new`)
          },
        },
        {
          label: 'Copy diagnostics',
          icon: <ClipboardCopy />,
          description: 'Versions and browser, for bug reports',
          onSelect: () => {
            void copyDiagnostics()
          },
        },
      ]}
    />
  )
}
