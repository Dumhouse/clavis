import { Link, Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import {
  Bot,
  HeartPulse,
  Inbox,
  KeyRound,
  LogOut,
  Layers,
  Moon,
  Palette,
  ScrollText,
  ShieldCheck,
  Search,
  Settings,
  Sun,
  Users,
  FolderKanban,
  type LucideIcon,
} from 'lucide-react'

import { useEffect, useState } from 'react'

import { CommandPalette } from '@/components/command-palette'
import { HintBar, ShortcutsSheet } from '@/components/hints'
import { Button } from '@/components/ui/button'
import { useTheme } from '@/components/theme-provider'
import { useAuth } from '@/lib/auth'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'

type NavItem = { to: string; label: string; icon: LucideIcon }

const workspaceNav: NavItem[] = [
  { to: '/', label: 'Projects', icon: FolderKanban },
  { to: '/shared-sets', label: 'Shared sets', icon: Layers },
  { to: '/health', label: 'Health', icon: HeartPulse },
  { to: '/requests', label: 'Requests', icon: Inbox },
]

const orgNav: NavItem[] = [
  { to: '/audit', label: 'Audit log', icon: ScrollText },
  { to: '/access', label: 'Access', icon: ShieldCheck },
  { to: '/members', label: 'Members', icon: Users },
  { to: '/identities', label: 'Identities', icon: Bot },
  { to: '/settings', label: 'Settings', icon: Settings },
]

export function AppShell() {
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const { theme, appearance, toggleTheme, setAppearance } = useTheme()
  const { user, logout } = useAuth()
  const { org, orgs, selectOrg } = useCurrentOrg()
  const navigate = useNavigate()
  const [orgOpen, setOrgOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [sheetOpen, setSheetOpen] = useState(false)

  useEffect(() => {
    let lastG = 0
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setPaletteOpen((open) => !open)
        return
      }
      const target = event.target as HTMLElement | null
      const typing =
        target?.tagName === 'INPUT' ||
        target?.tagName === 'TEXTAREA' ||
        target?.tagName === 'SELECT' ||
        target?.isContentEditable === true
      if (typing || event.metaKey || event.ctrlKey || event.altKey) return

      if (event.key === '?') {
        event.preventDefault()
        setSheetOpen(true)
        return
      }
      if (event.key.toLowerCase() === 'g') {
        lastG = Date.now()
        return
      }
      if (Date.now() - lastG < 1200) {
        const map: Record<string, string> = { s: '/', r: '/requests', h: '/health', a: '/members' }
        const destination = map[event.key.toLowerCase()]
        if (destination) {
          event.preventDefault()
          lastG = 0
          void navigate({ to: destination })
          return
        }
      }
      if (event.key.toLowerCase() === 'j' || event.key.toLowerCase() === 'k') {
        const region = document.querySelector<HTMLElement>('[data-scroll-region]')
        if (region) {
          region.scrollBy({ top: event.key.toLowerCase() === 'j' ? 56 : -56, behavior: 'smooth' })
        }
      }
    }
    const hintsHandler = () => {
      setSheetOpen(true)
    }
    window.addEventListener('keydown', handler)
    window.addEventListener('clavis:show-hints', hintsHandler)
    return () => {
      window.removeEventListener('keydown', handler)
      window.removeEventListener('clavis:show-hints', hintsHandler)
    }
  }, [navigate])
  const initials = (user?.name ?? 'C')
    .split(' ')
    .map((part) => part[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()
  const title =
    [...workspaceNav, ...orgNav].find((item) => item.to === pathname)?.label ?? 'Projects'

  return (
    <div className="flex h-full bg-bg text-text">
      <aside className="hidden w-[244px] shrink-0 flex-col border-r border-border bg-panel md:flex">
        <div className="flex h-14 items-center gap-2.5 border-b border-border px-4">
          <div className="logo-mark flex size-7 items-center justify-center rounded-[9px] shadow-sm">
            <KeyRound className="size-4 text-[#06241c]" strokeWidth={2.6} />
          </div>
          <span className="font-head text-[15px] font-semibold tracking-tight">Clavis</span>
          <span className="ml-auto rounded-full border border-border bg-surface px-1.5 py-0.5 text-[10px] font-medium text-dim">
            dev
          </span>
        </div>

        <div className="relative p-3">
          <button
            className="flex w-full items-center gap-2 rounded-app-sm border border-border bg-surface px-2.5 py-2 text-left hover:bg-hover"
            onClick={() => {
              setOrgOpen((open) => !open)
            }}
            aria-haspopup="listbox"
            aria-expanded={orgOpen}
          >
            <span className="flex size-6 items-center justify-center rounded-md bg-accent-soft text-[11px] font-bold text-accent-text">
              {(org?.name ?? 'C').slice(0, 1).toUpperCase()}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[12px] font-medium">
                {org?.name ?? 'No organization'}
              </span>
              <span className="block truncate text-[11px] text-dim">
                {org?.role ?? 'Member'} · free
              </span>
            </span>
            <span className="text-dim">⌄</span>
          </button>
          {orgOpen && (
            <div
              className="absolute left-3 right-3 z-30 mt-1 overflow-hidden rounded-app-sm border border-border bg-surface shadow-app"
              role="listbox"
            >
              {orgs.map((candidate) => (
                <button
                  key={candidate.id}
                  role="option"
                  aria-selected={candidate.slug === org?.slug}
                  className="flex w-full items-center gap-2 px-2.5 py-2 text-left text-[12px] hover:bg-hover"
                  onClick={() => {
                    setOrgOpen(false)
                    if (candidate.slug === org?.slug) return
                    selectOrg(candidate.slug)
                    void navigate({ to: '/' })
                  }}
                >
                  <span className="flex size-5 items-center justify-center rounded bg-elev text-[10px] font-semibold text-muted">
                    {candidate.name.slice(0, 1).toUpperCase()}
                  </span>
                  <span className="min-w-0 flex-1 truncate">{candidate.name}</span>
                  <span className="text-[10px] text-dim">{candidate.role}</span>
                </button>
              ))}
            </div>
          )}
        </div>

        <nav className="flex flex-1 flex-col gap-6 overflow-y-auto px-3 pb-4">
          <NavGroup label="Workspace" items={workspaceNav} pathname={pathname} />
          <NavGroup label="Organization" items={orgNav} pathname={pathname} />
        </nav>

        <div className="border-t border-border p-3">
          <div className="flex items-center gap-2 rounded-app-sm px-1.5 py-1.5">
            <div className="flex size-7 items-center justify-center rounded-full bg-elev text-[11px] font-semibold text-muted">
              {initials}
            </div>
            <div className="min-w-0 flex-1">
              <p className="truncate text-[12px] font-medium">{user?.name ?? 'Signed in'}</p>
              <p className="truncate text-[11px] text-dim">{org?.role ?? 'Member'}</p>
            </div>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Sign out"
              title="Sign out"
              onClick={() => void logout()}
            >
              <LogOut className="size-3.5" />
            </Button>
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-bg/80 px-4 backdrop-blur md:px-6">
          <div className="flex min-w-0 items-center gap-2 text-[12px]">
            <span className="text-dim">{org?.name ?? 'Clavis'}</span>
            <span className="text-dim">/</span>
            <span className="truncate font-medium">{title}</span>
          </div>

          <div className="ml-auto flex items-center gap-1.5">
            <button
              className="hidden h-8 w-52 items-center gap-2 rounded-app-sm border border-border bg-surface px-2.5 text-left text-[12px] text-dim hover:bg-hover sm:flex"
              onClick={() => {
                setPaletteOpen(true)
              }}
            >
              <Search className="size-3.5" />
              <span className="flex-1">Search…</span>
              <kbd className="rounded border border-border bg-elev px-1 font-mono text-[10px] text-muted">
                ⌘K
              </kbd>
            </button>
            <Button
              variant="ghost"
              size="icon"
              onClick={toggleTheme}
              aria-label={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
            >
              {theme === 'dark' ? <Sun className="size-4" /> : <Moon className="size-4" />}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="gap-1.5"
              onClick={() => {
                setAppearance(appearance === 'classic' ? 'warm' : 'classic')
              }}
              aria-label="Switch appearance"
            >
              <Palette className="size-3.5" />
              <span className="capitalize">{appearance}</span>
            </Button>
            <div className="ml-1 flex size-7 items-center justify-center rounded-full bg-elev text-[11px] font-semibold text-muted">
              {initials}
            </div>
          </div>
        </header>

        <main className="min-h-0 flex-1 overflow-y-auto" data-scroll-region>
          <div className="mx-auto w-full max-w-6xl px-4 py-6 md:px-8 md:py-8">
            <Outlet />
          </div>
        </main>

        <HintBar
          pathname={pathname}
          onShowSheet={() => {
            setSheetOpen(true)
          }}
        />
      </div>

      <CommandPalette
        open={paletteOpen}
        onClose={() => {
          setPaletteOpen(false)
        }}
      />
      {sheetOpen && (
        <ShortcutsSheet
          pathname={pathname}
          onClose={() => {
            setSheetOpen(false)
          }}
        />
      )}
    </div>
  )
}

function NavGroup({
  label,
  items,
  pathname,
}: {
  label: string
  items: NavItem[]
  pathname: string
}) {
  return (
    <div>
      <p className="mb-1 px-2 text-[10px] font-semibold uppercase tracking-[0.08em] text-dim">
        {label}
      </p>
      <div className="flex flex-col gap-0.5">
        {items.map((item) => {
          const active = pathname === item.to
          return (
            <Link
              key={item.to}
              to={item.to}
              className={cn(
                'flex h-8 items-center gap-2.5 rounded-app-sm px-2 text-[12.5px] text-muted transition-colors hover:bg-hover hover:text-text',
                active && 'bg-hover text-text',
              )}
            >
              <item.icon className={cn('size-4', active ? 'text-accent-text' : 'text-dim')} />
              <span className="flex-1">{item.label}</span>
              {item.to === '/requests' && (
                <span className="rounded-full bg-amber-bg px-1.5 text-[10px] font-semibold text-amber-fg">
                  3
                </span>
              )}
            </Link>
          )
        })}
      </div>
    </div>
  )
}
