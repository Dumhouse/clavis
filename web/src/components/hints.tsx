import { Check, ChevronRight, Copy, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  defaultTipRotationMs,
  globalShortcuts,
  hintsEnabledKey,
  hintsFor,
  type CliHint,
} from '@/lib/hints'
import { cn } from '@/lib/utils'

function hintsEnabled(): boolean {
  return localStorage.getItem(hintsEnabledKey) !== 'false'
}

export function HintBar({ pathname, onShowSheet }: { pathname: string; onShowSheet: () => void }) {
  const page = hintsFor(pathname)
  const [enabled, setEnabled] = useState(hintsEnabled)
  const [tipIndex, setTipIndex] = useState(0)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    const handler = () => {
      setEnabled(hintsEnabled())
    }
    window.addEventListener('clavis:hints', handler)
    return () => {
      window.removeEventListener('clavis:hints', handler)
    }
  }, [])

  useEffect(() => {
    if (!enabled || page.tips.length <= 1) return
    const timer = window.setInterval(() => {
      setTipIndex((current) => (current + 1) % page.tips.length)
    }, defaultTipRotationMs)
    return () => {
      window.clearInterval(timer)
    }
  }, [enabled, page.tips.length])

  const cli = useMemo<CliHint | undefined>(() => page.cmds[0], [page.cmds])
  const tip = page.tips[tipIndex % Math.max(1, page.tips.length)] ?? ''

  function hide() {
    localStorage.setItem(hintsEnabledKey, 'false')
    setEnabled(false)
    window.dispatchEvent(new CustomEvent('clavis:hints'))
  }

  async function copy() {
    if (!cli) return
    await navigator.clipboard.writeText(cli.cmd)
    setCopied(true)
    window.setTimeout(() => {
      setCopied(false)
    }, 1500)
  }

  if (!enabled) {
    return (
      <div className="flex h-8 shrink-0 items-center gap-2 border-t border-border bg-panel px-4 text-[11px] text-dim">
        <button
          type="button"
          className="rounded px-1.5 py-0.5 hover:bg-hover hover:text-text"
          onClick={onShowSheet}
        >
          ? Shortcuts
        </button>
        <button
          type="button"
          className="ml-auto rounded px-1.5 py-0.5 hover:bg-hover hover:text-text"
          onClick={() => {
            localStorage.setItem(hintsEnabledKey, 'true')
            setEnabled(true)
            window.dispatchEvent(new CustomEvent('clavis:hints'))
          }}
        >
          Show hints
        </button>
      </div>
    )
  }

  return (
    <div className="flex h-[34px] shrink-0 items-center gap-4 border-t border-border bg-panel px-4 text-[11px] text-dim">
      <div className="flex items-center gap-3 overflow-hidden">
        {page.keys.slice(0, 3).map((shortcut) => (
          <span key={shortcut.keys} className="flex shrink-0 items-center gap-1.5">
            <kbd className="rounded border border-border bg-surface px-1.5 py-0.5 font-mono text-[10px] text-muted">
              {shortcut.keys}
            </kbd>
            <span className="hidden lg:inline">{shortcut.label}</span>
          </span>
        ))}
      </div>

      <button
        type="button"
        className="flex min-w-0 flex-1 items-center gap-1.5 truncate text-left hover:text-muted"
        onClick={() => {
          setTipIndex((current) => (current + 1) % Math.max(1, page.tips.length))
        }}
        title="Next tip"
      >
        <ChevronRight className="size-3 shrink-0" />
        <span className="truncate">{tip}</span>
      </button>

      {cli && (
        <button
          type="button"
          onClick={() => {
            void copy()
          }}
          className="hidden max-w-[340px] items-center gap-1.5 truncate rounded border border-border bg-bg px-1.5 py-0.5 font-mono text-[10.5px] text-muted hover:text-text md:flex"
          title={`Copy: ${cli.cmd}`}
        >
          {copied ? <Check className="size-3 text-accent-text" /> : <Copy className="size-3" />}
          <span className="truncate">{cli.cmd}</span>
        </button>
      )}

      <button
        type="button"
        className="rounded px-1.5 py-0.5 hover:bg-hover hover:text-text"
        onClick={onShowSheet}
      >
        ?
      </button>
      <button
        type="button"
        className="rounded px-1 py-0.5 hover:bg-hover hover:text-text"
        onClick={hide}
        aria-label="Hide hints"
      >
        <X className="size-3" />
      </button>
    </div>
  )
}

export function ShortcutsSheet({ pathname, onClose }: { pathname: string; onClose: () => void }) {
  const page = hintsFor(pathname)
  const [copied, setCopied] = useState<string | null>(null)

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handler)
    return () => {
      window.removeEventListener('keydown', handler)
    }
  }, [onClose])

  async function copy(command: string) {
    await navigator.clipboard.writeText(command)
    setCopied(command)
    window.setTimeout(() => {
      setCopied(null)
    }, 1500)
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-4"
      onMouseDown={onClose}
    >
      <div
        className="flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden rounded-app border border-border bg-surface shadow-app"
        onMouseDown={(event) => {
          event.stopPropagation()
        }}
      >
        <div className="flex items-start justify-between gap-3 border-b border-border px-5 py-4">
          <div>
            <h2 className="font-head text-[18px] font-semibold">Shortcuts & commands</h2>
            <p className="mt-0.5 text-[12px] text-muted">
              {page.title} · everything here has a CLI equivalent.
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
            <X className="size-4" />
          </Button>
        </div>

        <div className="grid gap-5 overflow-y-auto p-5 sm:grid-cols-2">
          <section>
            <h3 className="mb-2 text-[10.5px] font-semibold uppercase tracking-[0.06em] text-dim">
              This page
            </h3>
            <ul className="space-y-1.5">
              {page.keys.length === 0 && (
                <li className="text-[12px] text-dim">No page-specific shortcuts yet.</li>
              )}
              {page.keys.map((shortcut) => (
                <li key={shortcut.keys} className="flex items-center gap-2 text-[12px]">
                  <kbd className="min-w-14 rounded border border-border bg-bg px-1.5 py-0.5 text-center font-mono text-[10.5px] text-muted">
                    {shortcut.keys}
                  </kbd>
                  <span className="text-text">{shortcut.label}</span>
                </li>
              ))}
            </ul>
          </section>

          <section>
            <h3 className="mb-2 text-[10.5px] font-semibold uppercase tracking-[0.06em] text-dim">
              Global
            </h3>
            <ul className="space-y-1.5">
              {globalShortcuts.map((shortcut) => (
                <li key={shortcut.keys} className="flex items-center gap-2 text-[12px]">
                  <kbd className="min-w-14 rounded border border-border bg-bg px-1.5 py-0.5 text-center font-mono text-[10.5px] text-muted">
                    {shortcut.keys}
                  </kbd>
                  <span className="text-text">{shortcut.label}</span>
                </li>
              ))}
            </ul>
          </section>

          <section className="sm:col-span-2">
            <h3 className="mb-2 text-[10.5px] font-semibold uppercase tracking-[0.06em] text-dim">
              CLI equivalents
            </h3>
            <ul className="space-y-1.5">
              {page.cmds.map((command) => (
                <li key={command.cmd} className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => {
                      void copy(command.cmd)
                    }}
                    className="flex flex-1 items-center gap-2 rounded-app-sm border border-border bg-bg px-2.5 py-1.5 text-left font-mono text-[11.5px] text-text hover:border-accent-line"
                  >
                    {copied === command.cmd ? (
                      <Check className="size-3.5 text-accent-text" />
                    ) : (
                      <Copy className="size-3.5 text-dim" />
                    )}
                    <span className="flex-1 truncate">{command.cmd}</span>
                  </button>
                  <span className="hidden w-56 text-[11px] text-dim sm:block">
                    {command.description}
                  </span>
                </li>
              ))}
            </ul>
          </section>

          <section className="sm:col-span-2">
            <h3 className="mb-2 text-[10.5px] font-semibold uppercase tracking-[0.06em] text-dim">
              Tips
            </h3>
            <ul className="space-y-1.5">
              {page.tips.map((tip) => (
                <li key={tip} className="flex gap-2 text-[12px] text-muted">
                  <span className={cn('mt-1.5 size-1 shrink-0 rounded-full bg-accent')} />
                  {tip}
                </li>
              ))}
            </ul>
          </section>
        </div>
      </div>
    </div>
  )
}
