import { useNavigate } from '@tanstack/react-router'
import {
  Bot,
  FolderKanban,
  HeartPulse,
  HelpCircle,
  Inbox,
  Layers,
  LogOut,
  Moon,
  Palette,
  ScrollText,
  Search,
  Settings,
  Users,
  Sun,
  type LucideIcon,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'

import { useTheme } from '@/components/theme-provider'
import { useAuth } from '@/lib/auth'
import { cn } from '@/lib/utils'

type Command = {
  id: string
  label: string
  hint: string
  icon: LucideIcon
  run: () => void
}

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate()
  const { theme, appearance, toggleTheme, setAppearance } = useTheme()
  const { logout } = useAuth()
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  const commands = useMemo<Command[]>(() => {
    const go = (to: string) => () => {
      void navigate({ to })
    }
    const nav: Array<[string, string, string, LucideIcon]> = [
      ['/', 'Projects', 'Go to projects', FolderKanban],
      ['/shared-sets', 'Shared sets', 'Go to shared sets', Layers],
      ['/health', 'Health', 'Go to health', HeartPulse],
      ['/requests', 'Requests', 'Go to requests', Inbox],
      ['/audit', 'Audit log', 'Go to audit log', ScrollText],
      ['/members', 'Members', 'Go to members', Users],
      ['/identities', 'Identities', 'Go to identities', Bot],
      ['/settings', 'Settings', 'Go to settings', Settings],
    ]
    return [
      ...nav.map(([to, label, hint, icon]) => ({
        id: `nav:${to}`,
        label,
        hint,
        icon,
        run: go(to),
      })),
      {
        id: 'appearance',
        label: `Switch to ${appearance === 'classic' ? 'Warm' : 'Classic'}`,
        hint: 'Appearance',
        icon: Palette,
        run: () => {
          setAppearance(appearance === 'classic' ? 'warm' : 'classic')
        },
      },
      {
        id: 'theme',
        label: `Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`,
        hint: 'Appearance',
        icon: theme === 'dark' ? Sun : Moon,
        run: toggleTheme,
      },
      {
        id: 'hints',
        label: 'Shortcuts & commands',
        hint: 'Help',
        icon: HelpCircle,
        run: () => {
          window.dispatchEvent(new CustomEvent('clavis:show-hints'))
        },
      },
      {
        id: 'logout',
        label: 'Sign out',
        hint: 'Account',
        icon: LogOut,
        run: () => {
          void logout()
        },
      },
    ]
  }, [navigate, appearance, setAppearance, theme, toggleTheme, logout])

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return commands
    return commands.filter((command) =>
      `${command.label} ${command.hint}`.toLowerCase().includes(needle),
    )
  }, [commands, query])

  useEffect(() => {
    if (!open) return
    setQuery('')
    setSelected(0)
    window.setTimeout(() => {
      inputRef.current?.focus()
    }, 0)
  }, [open])

  useEffect(() => {
    if (selected >= filtered.length) setSelected(Math.max(0, filtered.length - 1))
  }, [filtered.length, selected])

  useEffect(() => {
    if (!open) return
    const handler = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onClose()
        return
      }
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        setSelected((current) => Math.min(current + 1, Math.max(0, filtered.length - 1)))
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault()
        setSelected((current) => Math.max(0, current - 1))
      }
      if (event.key === 'Enter') {
        event.preventDefault()
        const command = filtered[selected]
        if (command) {
          command.run()
          onClose()
        }
      }
    }
    window.addEventListener('keydown', handler)
    return () => {
      window.removeEventListener('keydown', handler)
    }
  }, [open, filtered, selected, onClose])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-scrim px-4 pt-[12vh]"
      onMouseDown={onClose}
    >
      <div
        className="w-full max-w-xl overflow-hidden rounded-app border border-border bg-surface shadow-app"
        onMouseDown={(event) => {
          event.stopPropagation()
        }}
      >
        <div className="flex items-center gap-2 border-b border-border px-3">
          <Search className="size-4 text-dim" />
          <input
            ref={inputRef}
            className="h-11 flex-1 bg-transparent text-[13px] text-text placeholder:text-dim focus:outline-none"
            placeholder="Search commands and pages…"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value)
            }}
          />
          <kbd className="rounded border border-border bg-elev px-1.5 py-0.5 font-mono text-[10px] text-dim">
            esc
          </kbd>
        </div>
        <div className="max-h-[52vh] overflow-y-auto py-1">
          {filtered.map((command, index) => (
            <button
              key={command.id}
              className={cn(
                'flex w-full items-center gap-2.5 px-3 py-2 text-left text-[12.5px]',
                index === selected
                  ? 'bg-hover text-text'
                  : 'text-muted hover:bg-hover hover:text-text',
              )}
              onMouseEnter={() => {
                setSelected(index)
              }}
              onClick={() => {
                command.run()
                onClose()
              }}
            >
              <command.icon
                className={cn('size-4', index === selected ? 'text-accent-text' : 'text-dim')}
              />
              <span className="flex-1">{command.label}</span>
              <span className="text-[10.5px] text-dim">{command.hint}</span>
            </button>
          ))}
          {filtered.length === 0 && (
            <p className="px-3 py-6 text-center text-[12px] text-dim">
              No commands match “{query}”.
            </p>
          )}
        </div>
        <div className="flex items-center gap-3 border-t border-border px-3 py-1.5 text-[10px] text-dim">
          <span>↑↓ navigate</span>
          <span>↵ select</span>
          <span className="ml-auto">⌘K to open</span>
        </div>
      </div>
    </div>
  )
}
