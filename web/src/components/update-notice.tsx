import { useQuery } from '@tanstack/react-query'
import { ArrowUpCircle, ExternalLink, X } from 'lucide-react'
import { useState } from 'react'

import { api } from '@/api/client'
import { CopyCommand } from '@/components/copy-command'

/** How long "Later" hides the notice for the same release. */
const snoozeMs = 7 * 24 * 60 * 60 * 1000
const snoozeKey = 'clavis.update.snoozed'

type Snooze = { version: string; until: number }

function snoozed(version: string): boolean {
  try {
    const saved = JSON.parse(localStorage.getItem(snoozeKey) ?? 'null') as Snooze | null
    return saved?.version === version && saved.until > Date.now()
  } catch {
    return false
  }
}

/**
 * Tells owners and admins that a newer Clavis release exists, with the command
 * that updates this server (worked out from how it was installed). "Later"
 * hides it for a week; a newer release shows it again.
 */
export function UpdateNotice({ canManage }: { canManage: boolean }) {
  const [hidden, setHidden] = useState(false)
  const status = useQuery({
    queryKey: ['system', 'update'],
    enabled: canManage,
    staleTime: 60 * 60 * 1000,
    refetchInterval: 6 * 60 * 60 * 1000,
    queryFn: async () => {
      const result = await api.GET('/api/v1/system/update')
      return result.data ?? null
    },
  })
  const data = status.data
  if (!canManage || hidden || !data?.update_available || !data.latest) return null
  if (snoozed(data.latest)) return null
  const latest = data.latest
  const install = data.install

  return (
    <aside
      aria-label="Update available"
      className="mb-5 flex items-start gap-3 rounded-app border border-accent-line bg-accent-soft px-4 py-3 text-sm"
    >
      <ArrowUpCircle className="mt-0.5 size-4 shrink-0 text-accent-text" />
      <div className="min-w-0 flex-1 space-y-2">
        <p className="text-text">
          <span className="font-medium">Clavis {latest} is available.</span>{' '}
          <span className="text-muted">
            This server runs {data.current}
            {install.label ? ` (${install.label})` : ''}.
          </span>{' '}
          {data.release_url && (
            <a
              href={data.release_url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-accent-text hover:underline"
            >
              What's new <ExternalLink className="size-3" />
            </a>
          )}
        </p>
        {install.command && <CopyCommand command={install.command} className="max-w-2xl" />}
        {install.note && <p className="text-xs text-muted">{install.note}</p>}
        <p className="text-xs text-dim">
          Update your CLI too: <code className="font-mono">clavis update</code> shows the command
          for your computer.
        </p>
      </div>
      <button
        type="button"
        className="inline-flex shrink-0 items-center gap-1 rounded-app-sm px-2 py-1 text-xs text-muted hover:bg-hover hover:text-text"
        onClick={() => {
          try {
            localStorage.setItem(
              snoozeKey,
              JSON.stringify({ version: latest, until: Date.now() + snoozeMs } satisfies Snooze),
            )
          } catch {
            // Hidden for this visit only.
          }
          setHidden(true)
        }}
      >
        <X className="size-3.5" /> Later
      </button>
    </aside>
  )
}
