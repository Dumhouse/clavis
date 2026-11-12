import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import {
  BookOpen,
  Bot,
  Cable,
  KeyRound,
  Loader2,
  ShieldCheck,
  UserRound,
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

import { api } from '@/api/client'
import { useTheme } from '@/components/theme-provider'
import { Modal } from '@/components/ui/modal'
import { useAuth } from '@/lib/auth'
import { useCurrentOrg } from '@/lib/org'
import { requestOpenKey } from '@/lib/open-key'
import { cn } from '@/lib/utils'

type Command = {
  id: string
  label: string
  hint: string
  icon: LucideIcon
  run: () => void
  group: string
  mono?: boolean
}

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate()
  const { theme, appearance, toggleTheme, setAppearance } = useTheme()
  const { logout } = useAuth()
  const { org } = useCurrentOrg()
  const [query, setQuery] = useState('')
  const [debounced, setDebounced] = useState('')

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebounced(query.trim())
    }, 150)
    return () => {
      window.clearTimeout(timer)
    }
  }, [query])

  const keySearch = useQuery({
    queryKey: ['key-search', org?.slug, debounced],
    enabled: Boolean(open && org && debounced.length >= 2),
    staleTime: 10_000,
    queryFn: async () => {
      if (!org) return []
      const result = await api.GET('/api/v1/orgs/{org}/search', {
        params: { path: { org: org.slug }, query: { q: debounced } },
      })
      if (result.error) return []
      return result.data?.keys ?? []
    },
  })
  const [selected, setSelected] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  const commands = useMemo<Command[]>(() => {
    const go = (to: string) => () => {
      void navigate({ to })
    }
    const nav: Array<[string, string, LucideIcon]> = [
      ['/', 'Projects', FolderKanban],
      ['/sets', 'Shared sets', Layers],
      ['/integrations', 'Integrations', Cable],
      ['/health', 'Health', HeartPulse],
      ['/requests', 'Access requests', Inbox],
      ['/members', 'Members', Users],
      ['/identities', 'Agents & machines', Bot],
      ['/access', 'Who has access', ShieldCheck],
      ['/audit', 'Audit log', ScrollText],
      ['/settings', 'Organization settings', Settings],
      ['/account', 'Your account', UserRound],
      ['/docs', 'Docs', BookOpen],
    ]
    return [
      ...nav.map(([to, label, icon]) => ({
        id: `nav:${to}`,
        label,
        hint: 'Go to',
        icon,
        group: 'Pages',
        run: go(to),
      })),
      {
        id: 'appearance',
        label: `Switch to ${appearance === 'classic' ? 'Warm' : 'Classic'}`,
        hint: 'Appearance',
        group: 'Actions',
        icon: Palette,
        run: () => {
          setAppearance(appearance === 'classic' ? 'warm' : 'classic')
        },
      },
      {
        id: 'theme',
        label: `Switch to ${theme === 'dark' ? 'light' : 'dark'} mode`,
        hint: 'Appearance',
        group: 'Actions',
        icon: theme === 'dark' ? Sun : Moon,
        run: toggleTheme,
      },
      {
        id: 'hints',
        label: 'Shortcuts & commands',
        hint: 'Help',
        group: 'Actions',
        icon: HelpCircle,
        run: () => {
          window.dispatchEvent(new CustomEvent('clavis:show-hints'))
        },
      },
      {
        id: 'logout',
        label: 'Sign out',
        hint: 'Account',
        group: 'Actions',
        icon: LogOut,
        run: () => {
          void logout()
        },
      },
    ]
  }, [navigate, appearance, setAppearance, theme, toggleTheme, logout])

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase()
    const keys: Command[] = (needle.length >= 2 ? (keySearch.data ?? []) : []).map((match) => ({
      id: `key:${match.project}:${match.key}`,
      label: match.key,
      hint: `${match.project_name} · ${(match.environments ?? []).join(', ')}`,
      icon: KeyRound,
      group: 'Secrets',
      mono: true,
      run: () => {
        requestOpenKey(match.key)
        void navigate({ to: '/projects/$project', params: { project: match.project } })
      },
    }))
    const matching = needle
      ? commands.filter((command) =>
          `${command.label} ${command.hint}`.toLowerCase().includes(needle),
        )
      : commands
    return [...keys, ...matching]
  }, [commands, query, keySearch.data, navigate])

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

  const searching = keySearch.isFetching && query.trim().length >= 2

  return (
    <Modal
      onClose={onClose}
      label="Command palette"
      className="max-w-xl self-start overflow-hidden p-0 mt-[12vh]"
    >
      <div className="flex items-center gap-2 border-b border-border px-3">
        <Search className="size-4 text-dim" aria-hidden />
        <input
          ref={inputRef}
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-results"
          aria-activedescendant={filtered[selected] ? `palette-${String(selected)}` : undefined}
          aria-label="Search pages, actions, and secret names"
          className="h-11 flex-1 bg-transparent text-base text-text placeholder:text-dim focus:outline-none"
          placeholder="Search secrets, pages, and actions…"
          value={query}
          onChange={(event) => {
            setQuery(event.target.value)
          }}
        />
        {searching && <Loader2 className="size-3.5 animate-spin text-dim" />}
        <kbd className="rounded border border-border bg-elev px-1.5 py-0.5 font-mono text-2xs text-dim">
          esc
        </kbd>
      </div>
      <div id="palette-results" role="listbox" className="max-h-[52vh] overflow-y-auto py-1">
        {filtered.map((command, index) => (
          <div key={command.id}>
            {(index === 0 || filtered[index - 1]?.group !== command.group) && (
              <p className="px-3 pb-1 pt-2 text-3xs font-semibold uppercase tracking-[0.08em] text-dim">
                {command.group}
              </p>
            )}
            <button
              id={`palette-${String(index)}`}
              type="button"
              role="option"
              aria-selected={index === selected}
              className={cn(
                'flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm',
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
              <span className={cn('flex-1 truncate', command.mono && 'font-mono text-xs')}>
                {command.label}
              </span>
              <span className="truncate text-2xs text-dim">{command.hint}</span>
            </button>
          </div>
        ))}
        {filtered.length === 0 && !searching && (
          <p className="px-3 py-6 text-center text-sm text-dim">
            Nothing matches “{query}”. Secret search looks at key names, never values.
          </p>
        )}
      </div>
      <div className="flex items-center gap-3 border-t border-border px-3 py-1.5 text-2xs text-dim">
        <span>↑↓ navigate</span>
        <span>↵ select</span>
        <span className="ml-auto">⌘K to open</span>
      </div>
    </Modal>
  )
}
