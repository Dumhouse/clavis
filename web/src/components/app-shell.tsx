import { Link, Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import {
  BookOpen,
  Boxes,
  Cable,
  Bot,
  DatabaseZap,
  FileCode2,
  GitPullRequest,
  HeartPulse,
  Inbox,
  KeyRound,
  LogOut,
  Layers,
  Moon,
  Palette,
  Pencil,
  Plus,
  RefreshCw,
  ScrollText,
  ShieldCheck,
  Search,
  Settings,
  Sun,
  Users,
  FolderKanban,
  Archive,
  ChevronDown,
  Menu as MenuIcon,
  UserRound,
  X,
  type LucideIcon,
} from 'lucide-react'

import { useEffect, useState } from 'react'

import { CommandPalette } from '@/components/command-palette'
import { HintBar, ShortcutsSheet } from '@/components/hints'
import { OrgDialog } from '@/components/org-dialog'
import { Button } from '@/components/ui/button'
import { Menu } from '@/components/ui/menu'
import { Modal } from '@/components/ui/modal'
import { useTheme } from '@/components/theme-provider'
import { UpdateNotice } from '@/components/update-notice'
import { useAuth } from '@/lib/auth'
import { useCurrentOrg } from '@/lib/org'
import { cn } from '@/lib/utils'

type NavItem = { to: string; label: string; icon: LucideIcon }

const workspaceNav: NavItem[] = [
  { to: '/', label: 'Projects', icon: FolderKanban },
  { to: '/sets', label: 'Shared sets', icon: Layers },
  { to: '/integrations', label: 'Integrations', icon: Cable },
  { to: '/health', label: 'Health', icon: HeartPulse },
  { to: '/requests', label: 'Requests', icon: Inbox },
]

const orgNav: NavItem[] = [
  { to: '/members', label: 'Members', icon: Users },
  { to: '/identities', label: 'Agents & machines', icon: Bot },
  { to: '/access', label: 'Who has access', icon: ShieldCheck },
  { to: '/audit', label: 'Audit log', icon: ScrollText },
  { to: '/settings', label: 'Settings', icon: Settings },
]

const accountNav: NavItem[] = [
  { to: '/account', label: 'Your account', icon: UserRound },
  { to: '/docs', label: 'Docs', icon: BookOpen },
]

type ProjectNavItem = {
  to:
    | '/projects/$project'
    | '/projects/$project/environments'
    | '/projects/$project/schema'
    | '/projects/$project/promote'
    | '/projects/$project/dynamic'
    | '/projects/$project/rotation'
    | '/projects/$project/snapshots'
    | '/projects/$project/access'
  label: string
  icon: LucideIcon
  /** One-line explanation shown on hover. */
  hint?: string
}

const projectNav: ProjectNavItem[] = [
  {
    to: '/projects/$project',
    label: 'Secrets',
    icon: KeyRound,
    hint: 'Keys and values for every environment',
  },
  {
    to: '/projects/$project/environments',
    label: 'Environments',
    icon: Boxes,
    hint: 'Dev, staging, production: order, type, protection',
  },
  {
    to: '/projects/$project/promote',
    label: 'Promote',
    icon: GitPullRequest,
    hint: 'Copy tested values to the next environment',
  },
  {
    to: '/projects/$project/access',
    label: 'Project access',
    icon: ShieldCheck,
    hint: 'Give people or groups access to this project',
  },
]

// Less frequent project tools; collapsed unless one of them is open.
const projectToolsNav: ProjectNavItem[] = [
  {
    to: '/projects/$project/schema',
    label: 'Schema',
    icon: FileCode2,
    hint: 'Rules each key must follow; checked on save and in CI',
  },
  {
    to: '/projects/$project/rotation',
    label: 'Rotation',
    icon: RefreshCw,
    hint: 'Replace passwords and keys on a schedule',
  },
  {
    to: '/projects/$project/dynamic',
    label: 'Dynamic secrets',
    icon: DatabaseZap,
    hint: 'Temporary database logins that delete themselves',
  },
  {
    to: '/projects/$project/snapshots',
    label: 'Snapshots',
    icon: Archive,
    hint: 'Restore points for an environment',
  },
]

function projectSlugFromPath(pathname: string): string | null {
  const match = /^\/projects\/([^/]+)/.exec(pathname)
  const slug = match?.[1]
  return slug ? decodeURIComponent(slug) : null
}

function projectHref(to: ProjectNavItem['to'], slug: string): string {
  return to.replace('$project', encodeURIComponent(slug))
}

export function AppShell() {
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const { theme, appearance, toggleTheme, setAppearance } = useTheme()
  const { user, logout } = useAuth()
  const { org } = useCurrentOrg()
  const navigate = useNavigate()
  const [orgOpen, setOrgOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const [sheetOpen, setSheetOpen] = useState(false)
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [lastPath, setLastPath] = useState(pathname)
  if (lastPath !== pathname) {
    setLastPath(pathname)
    setMobileNavOpen(false)
  }

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
  const projectSlug = projectSlugFromPath(pathname)
  const initials = (user?.name ?? 'C')
    .split(' ')
    .map((part) => part[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()
  const title =
    (projectSlug
      ? [...projectNav, ...projectToolsNav].find(
          (item) => projectHref(item.to, projectSlug) === pathname,
        )?.label
      : undefined) ??
    [...workspaceNav, ...orgNav, ...accountNav].find((item) => item.to === pathname)?.label ??
    'Projects'

  return (
    <div className="flex h-full bg-bg text-text">
      <aside className="hidden w-[244px] shrink-0 flex-col border-r border-border bg-panel md:flex">
        <SidebarContent
          pathname={pathname}
          projectSlug={projectSlug}
          initials={initials}
          orgOpen={orgOpen}
          setOrgOpen={setOrgOpen}
        />
      </aside>

      {mobileNavOpen && (
        <Modal
          variant="sheet"
          label="Navigation"
          className="mr-auto ml-0 max-w-[280px] border-r border-l-0 bg-panel"
          onClose={() => {
            setMobileNavOpen(false)
          }}
        >
          <Button
            variant="ghost"
            size="icon"
            aria-label="Close navigation"
            className="absolute right-2 top-3 z-10"
            onClick={() => {
              setMobileNavOpen(false)
            }}
          >
            <X className="size-4" />
          </Button>
          <SidebarContent
            pathname={pathname}
            projectSlug={projectSlug}
            initials={initials}
            orgOpen={orgOpen}
            setOrgOpen={setOrgOpen}
          />
        </Modal>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-bg/80 px-4 backdrop-blur md:px-6">
          <Button
            variant="ghost"
            size="icon"
            className="-ml-1.5 md:hidden"
            aria-label="Open navigation"
            onClick={() => {
              setMobileNavOpen(true)
            }}
          >
            <MenuIcon className="size-4" />
          </Button>
          <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-2 text-sm">
            <span
              className={cn(
                'hidden shrink-0 whitespace-nowrap text-dim',
                projectSlug ? 'xl:inline' : 'sm:inline',
              )}
            >
              {org?.name ?? 'Clavis'}
            </span>
            <span className={cn('hidden text-dim', projectSlug ? 'xl:inline' : 'sm:inline')}>
              /
            </span>
            {projectSlug ? (
              <>
                <Link to="/" className="hidden shrink-0 text-dim hover:text-text lg:inline">
                  Projects
                </Link>
                <span className="hidden text-dim lg:inline">/</span>
                <Link
                  to="/projects/$project"
                  params={{ project: projectSlug }}
                  className="max-w-40 truncate font-mono text-xs text-muted hover:text-text"
                >
                  {projectSlug}
                </Link>
                <span className="text-dim">/</span>
              </>
            ) : null}
            <span className="truncate font-medium" aria-current="page">
              {title}
            </span>
          </nav>

          <div className="ml-auto flex items-center gap-1.5">
            <button
              className="hidden h-8 w-40 shrink-0 items-center gap-2 lg:w-52 rounded-app-sm border border-border bg-surface px-2.5 text-left text-sm text-dim hover:bg-hover sm:flex"
              onClick={() => {
                setPaletteOpen(true)
              }}
            >
              <Search className="size-3.5" />
              <span className="flex-1">Search…</span>
              <kbd className="rounded border border-border bg-elev px-1 font-mono text-2xs text-muted">
                ⌘K
              </kbd>
            </button>
            <Button
              variant="ghost"
              size="icon"
              className="sm:hidden"
              aria-label="Search"
              onClick={() => {
                setPaletteOpen(true)
              }}
            >
              <Search className="size-4" />
            </Button>
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
              className="hidden gap-1.5 sm:inline-flex"
              onClick={() => {
                setAppearance(appearance === 'classic' ? 'warm' : 'classic')
              }}
              aria-label={`Switch appearance, currently ${appearance}`}
            >
              <Palette className="size-3.5" />
              <span className="capitalize">{appearance}</span>
            </Button>
            <Menu
              label={`Account menu for ${initials}`}
              triggerClassName="ml-1 flex size-7 items-center justify-center rounded-full bg-elev text-xs font-semibold text-muted hover:ring-2 hover:ring-accent-line"
              trigger={initials}
              items={[
                {
                  label: (
                    <span className="block">
                      <span className="block font-medium">{user?.name ?? 'Signed in'}</span>
                      <span className="block text-2xs text-dim">{user?.email}</span>
                    </span>
                  ),
                  disabled: true,
                  onSelect: () => undefined,
                },
                'separator',
                {
                  label: 'Your account',
                  icon: <UserRound />,
                  onSelect: () => {
                    void navigate({ to: '/account' })
                  },
                },
                {
                  label: 'Keyboard shortcuts',
                  icon: <BookOpen />,
                  hint: '?',
                  onSelect: () => {
                    setSheetOpen(true)
                  },
                },
                'separator',
                {
                  label: 'Sign out',
                  icon: <LogOut />,
                  onSelect: () => {
                    void logout()
                  },
                },
              ]}
            />
          </div>
        </header>

        <main className="min-h-0 flex-1 overflow-y-auto" data-scroll-region>
          <div
            className={cn(
              'mx-auto w-full px-4 py-6 md:px-8 md:py-8',
              // The secrets grid needs room for long values.
              /^\/projects\/[^/]+\/?$/.test(pathname) ? 'max-w-[110rem]' : 'max-w-6xl',
            )}
          >
            <UpdateNotice canManage={org?.role === 'Owner' || org?.role === 'Admin'} />
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

function SidebarContent({
  pathname,
  projectSlug,
  initials,
  orgOpen,
  setOrgOpen,
}: {
  pathname: string
  projectSlug: string | null
  initials: string
  orgOpen: boolean
  setOrgOpen: (update: boolean | ((open: boolean) => boolean)) => void
}) {
  const { user, logout } = useAuth()
  const { org, orgs, selectOrg } = useCurrentOrg()
  const navigate = useNavigate()
  const [orgDialog, setOrgDialog] = useState<'new' | 'rename' | null>(null)
  return (
    <>
      {orgDialog && (
        <OrgDialog
          existing={orgDialog === 'rename' ? (org ?? null) : null}
          onClose={() => {
            setOrgDialog(null)
          }}
          onSaved={(saved) => {
            setOrgDialog(null)
            selectOrg(saved.slug)
            void navigate({ to: '/' })
          }}
        />
      )}
      <div className="flex h-14 items-center gap-2.5 border-b border-border px-4">
        <div className="logo-mark flex size-7 items-center justify-center rounded-[9px] shadow-sm">
          <KeyRound className="size-4 text-[#06241c]" strokeWidth={2.6} />
        </div>
        <span className="font-head text-md font-semibold tracking-tight">Clavis</span>
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
          <span className="flex size-6 items-center justify-center rounded-md bg-accent-soft text-xs font-bold text-accent-text">
            {(org?.name ?? 'C').slice(0, 1).toUpperCase()}
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium">
              {org?.name ?? 'No organization'}
            </span>
            <span className="block truncate text-xs text-dim">{org?.role ?? 'Member'}</span>
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
                className="flex w-full items-center gap-2 px-2.5 py-2 text-left text-sm hover:bg-hover"
                onClick={() => {
                  setOrgOpen(false)
                  if (candidate.slug === org?.slug) return
                  selectOrg(candidate.slug)
                  void navigate({ to: '/' })
                }}
              >
                <span className="flex size-5 items-center justify-center rounded bg-elev text-2xs font-semibold text-muted">
                  {candidate.name.slice(0, 1).toUpperCase()}
                </span>
                <span className="min-w-0 flex-1 truncate">{candidate.name}</span>
                <span className="text-2xs text-dim">{candidate.role}</span>
              </button>
            ))}
            <div className="border-t border-border-soft">
              {org?.role === 'Owner' && (
                <button
                  className="flex w-full items-center gap-2 px-2.5 py-2 text-left text-sm text-muted hover:bg-hover hover:text-text"
                  onClick={() => {
                    setOrgOpen(false)
                    setOrgDialog('rename')
                  }}
                >
                  <Pencil className="size-3.5" /> Rename {org.name}
                </button>
              )}
              <button
                className="flex w-full items-center gap-2 px-2.5 py-2 text-left text-sm text-muted hover:bg-hover hover:text-text"
                onClick={() => {
                  setOrgOpen(false)
                  setOrgDialog('new')
                }}
              >
                <Plus className="size-3.5" /> New organization
              </button>
            </div>
          </div>
        )}
      </div>

      <nav className="scroll-quiet flex flex-1 flex-col gap-6 overflow-y-auto pb-4 pl-3 pr-1">
        {projectSlug && <ProjectNavGroup slug={projectSlug} pathname={pathname} />}
        <NavGroup label="Workspace" items={workspaceNav} pathname={pathname} />
        <NavGroup label="Organization" items={orgNav} pathname={pathname} />
        <NavGroup label="You" items={accountNav} pathname={pathname} />
      </nav>

      <div className="border-t border-border p-3">
        <div className="flex items-center gap-2 rounded-app-sm px-1.5 py-1.5">
          <div className="flex size-7 items-center justify-center rounded-full bg-elev text-xs font-semibold text-muted">
            {initials}
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium">{user?.name ?? 'Signed in'}</p>
            <p className="truncate text-xs text-dim">{org?.role ?? 'Member'}</p>
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
    </>
  )
}

function ProjectNavGroup({ slug, pathname }: { slug: string; pathname: string }) {
  const inTools = projectToolsNav.some((item) => projectHref(item.to, slug) === pathname)
  const [toolsOpenPref, setToolsOpen] = useState(
    () => localStorage.getItem('clavis.nav.tools') === 'open',
  )
  const toolsOpen = toolsOpenPref || inTools
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between gap-2 px-2">
        <p className="text-2xs font-semibold uppercase tracking-[0.08em] text-dim">Project</p>
        <Link to="/" className="text-2xs text-dim hover:text-text">
          All projects
        </Link>
      </div>
      <div className="flex flex-col gap-0.5">
        {projectNav.map((item) => (
          <ProjectLink key={item.to} item={item} slug={slug} pathname={pathname} />
        ))}
        <button
          type="button"
          aria-expanded={toolsOpen}
          onClick={() => {
            const next = !toolsOpen
            setToolsOpen(next)
            localStorage.setItem('clavis.nav.tools', next ? 'open' : 'closed')
          }}
          className="flex h-8 items-center gap-2.5 rounded-app-sm px-2 text-sm text-dim hover:bg-hover hover:text-text"
        >
          <ChevronDown className={cn('size-4 transition-transform', !toolsOpen && '-rotate-90')} />
          <span className="flex-1 text-left">Tools</span>
        </button>
        {toolsOpen &&
          projectToolsNav.map((item) => (
            <ProjectLink key={item.to} item={item} slug={slug} pathname={pathname} />
          ))}
      </div>
    </div>
  )
}

function ProjectLink({
  item,
  slug,
  pathname,
}: {
  item: ProjectNavItem
  slug: string
  pathname: string
}) {
  const active = pathname === projectHref(item.to, slug)
  return (
    <Link
      to={item.to}
      params={{ project: slug }}
      aria-current={active ? 'page' : undefined}
      title={item.hint}
      className={cn(
        'flex h-8 items-center gap-2.5 rounded-app-sm px-2 text-sm text-muted transition-colors hover:bg-hover hover:text-text',
        active && 'bg-hover text-text',
      )}
    >
      <item.icon className={cn('size-4', active ? 'text-accent-text' : 'text-dim')} />
      <span className="flex-1">{item.label}</span>
    </Link>
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
      <p className="mb-1 px-2 text-2xs font-semibold uppercase tracking-[0.08em] text-dim">
        {label}
      </p>
      <div className="flex flex-col gap-0.5">
        {items.map((item) => {
          const active = pathname === item.to
          return (
            <Link
              key={item.to}
              to={item.to}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'flex h-8 items-center gap-2.5 rounded-app-sm px-2 text-sm text-muted transition-colors hover:bg-hover hover:text-text',
                active && 'bg-hover text-text',
              )}
            >
              <item.icon className={cn('size-4', active ? 'text-accent-text' : 'text-dim')} />
              <span className="flex-1">{item.label}</span>
            </Link>
          )
        })}
      </div>
    </div>
  )
}
