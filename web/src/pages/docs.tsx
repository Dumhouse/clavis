import {
  ArrowLeft,
  ArrowRight,
  BookOpen,
  Bot,
  Braces,
  Check,
  ChevronRight,
  Copy,
  ExternalLink,
  FileCode,
  GitBranch,
  Maximize2,
  Minimize2,
  Hash,
  KeyRound,
  Lightbulb,
  RefreshCw,
  Rocket,
  Search,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
  type LucideIcon,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'

import { CertificateNote } from '@/components/certificate-note'
import { DocMarkdown, type LinkRenderer } from '@/components/doc-markdown'
import { Select } from '@/components/ui/field'
import {
  commandByPath,
  flagSignature,
  groupCommands,
  publicDocsBase,
  reference,
  resolveReferenceLink,
  type RefCommand,
  type RefExample,
  type RefFlag,
  type RefGroup,
  type RefTopic,
} from '@/lib/cli-reference'
import { guides, type DocGuide, type DocSnippet } from '@/lib/docs-content'
import { cn } from '@/lib/utils'

// ---------------------------------------------------------------------------
// Page model
// ---------------------------------------------------------------------------

type Section = 'Get started' | 'Guides' | 'CLI reference' | 'CLI configuration'

type DocPage = {
  id: string
  title: string
  section: Section
  icon: LucideIcon
  summary: string
  guide?: DocGuide
  group?: RefGroup
  topic?: RefTopic
  kind: 'overview' | 'guide' | 'commands' | 'config' | 'topic'
}

const guideIcons: Record<string, LucideIcon> = {
  start: Rocket,
  schema: Braces,
  agents: Bot,
  sync: RefreshCw,
  workflow: GitBranch,
  dynamic: KeyRound,
  security: ShieldCheck,
}

const commandAnchor = (command: RefCommand) => `cmd-${command.slug}`
const groupPageId = (groupId: string) => `cli-${groupId}`
const topicPageId = (topicId: string) => `topic-${topicId}`
const stepAnchor = (index: number) => `step-${String(index + 1)}`

const pages: DocPage[] = [
  {
    id: 'overview',
    title: 'Overview',
    section: 'Get started',
    icon: BookOpen,
    summary: 'What Clavis does and where to start.',
    kind: 'overview',
  },
  ...guides.map<DocPage>((guide) => ({
    id: guide.id,
    title: guide.title,
    section: guide.id === 'start' ? 'Get started' : 'Guides',
    icon: guideIcons[guide.id] ?? BookOpen,
    summary: guide.summary,
    guide,
    kind: 'guide',
  })),
  ...reference.groups.map<DocPage>((group) => ({
    id: groupPageId(group.id),
    title: group.title,
    section: 'CLI reference',
    icon: Terminal,
    summary: group.description,
    group,
    kind: 'commands',
  })),
  {
    id: 'config',
    title: 'Global flags',
    section: 'CLI configuration',
    icon: SlidersHorizontal,
    summary: 'Flags that work with every command. Put them before the command name or after it.',
    kind: 'config',
  },
  ...reference.topics.map<DocPage>((topic) => ({
    id: topicPageId(topic.id),
    title: topic.title,
    section: 'CLI configuration',
    icon: FileCode,
    summary: topic.summary,
    topic,
    kind: 'topic',
  })),
]

const sections: Section[] = ['Get started', 'Guides', 'CLI reference', 'CLI configuration']
const pageById = new Map(pages.map((page) => [page.id, page]))

// ---------------------------------------------------------------------------
// Location: #page or #page/anchor, so links, back and forward all work.
// ---------------------------------------------------------------------------

function subscribeHash(callback: () => void) {
  window.addEventListener('hashchange', callback)
  return () => {
    window.removeEventListener('hashchange', callback)
  }
}

function useDocLocation() {
  const hash = useSyncExternalStore(subscribeHash, () => window.location.hash)
  const [pageId = '', anchor = ''] = hash.replace(/^#/, '').split('/')
  const page = pageById.get(pageId) ?? pages[0]
  if (!page) throw new Error('No documentation pages.')
  return { page, anchor }
}

const href = (pageId: string, anchor?: string) => `#${pageId}${anchor ? `/${anchor}` : ''}`
const commandHref = (command: RefCommand) =>
  href(groupPageId(command.group), commandAnchor(command))

/** Links inside reference text: commands and topics stay in the app. */
const renderLink: LinkRenderer = (target, children, key) => {
  const internal = resolveReferenceLink(target)
  const className = 'font-medium text-accent-text hover:underline'
  if (internal?.kind === 'command') {
    return (
      <a key={key} href={commandHref(internal.command)} className={className}>
        {children}
      </a>
    )
  }
  if (internal?.kind === 'topic') {
    return (
      <a key={key} href={href(topicPageId(internal.id))} className={className}>
        {children}
      </a>
    )
  }
  const external = /^https?:/.test(target) ? target : new URL(target, publicDocsBase).toString()
  return (
    <a key={key} href={external} target="_blank" rel="noreferrer" className={className}>
      {children}
    </a>
  )
}

function scrollRegion(): HTMLElement | null {
  return (
    document.querySelector<HTMLElement>('[data-docs-full]') ??
    document.querySelector<HTMLElement>('[data-scroll-region]')
  )
}

// ---------------------------------------------------------------------------
// Search index
// ---------------------------------------------------------------------------

type SearchEntry = {
  page: DocPage
  anchor?: string
  title: string
  context: string
  mono: boolean
  haystack: string
}

const searchIndex: SearchEntry[] = (() => {
  const entries: SearchEntry[] = []
  for (const page of pages) {
    if (page.guide) {
      const guide = page.guide
      entries.push({
        page,
        title: guide.title,
        context: guide.summary,
        mono: false,
        haystack: [guide.title, guide.summary, guide.tags.join(' ')].join(' '),
      })
      guide.steps.forEach((step, index) => {
        entries.push({
          page,
          anchor: stepAnchor(index),
          title: step.title,
          context: guide.title,
          mono: false,
          haystack: [
            step.title,
            step.body,
            ...(step.snippets ?? []).map((snippet) => snippet.code),
          ].join(' '),
        })
      })
    }
    if (page.group) {
      for (const command of groupCommands(page.group)) {
        entries.push({
          page,
          anchor: commandAnchor(command),
          title: `clavis ${command.path}`,
          context: command.summary,
          mono: true,
          haystack: [
            command.path,
            command.summary,
            command.description,
            ...command.flags.map((flag) => `--${flag.name}`),
            ...command.examples.map((example) => `${example.title ?? ''} ${example.run}`),
          ].join(' '),
        })
      }
    }
    if (page.topic) {
      entries.push({
        page,
        title: page.topic.title,
        context: page.topic.summary,
        mono: false,
        haystack: [page.topic.title, page.topic.summary, page.topic.body].join(' '),
      })
    }
    if (page.kind === 'config') {
      for (const flag of reference.globalFlags) {
        entries.push({
          page,
          anchor: `flag-${flag.name}`,
          title: `--${flag.name}`,
          context: flag.usage,
          mono: true,
          haystack: `--${flag.name} ${flag.usage}`,
        })
      }
    }
  }
  return entries.map((entry) => ({ ...entry, haystack: entry.haystack.toLowerCase() }))
})()

function search(needle: string): SearchEntry[] {
  const terms = needle.toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return []
  return searchIndex
    .filter((entry) => terms.every((term) => entry.haystack.includes(term)))
    .map((entry) => {
      const title = entry.title.toLowerCase().replace(/^clavis /, '')
      const score = terms.every((term) => title.includes(term))
        ? title.startsWith(terms[0] ?? '')
          ? 0
          : 1
        : 2
      return { entry, score }
    })
    .sort((a, b) => a.score - b.score)
    .slice(0, 40)
    .map(({ entry }) => entry)
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export function DocsPage() {
  const { page, anchor } = useDocLocation()
  const [query, setQuery] = useState('')
  const [full, setFull] = useState(false)
  const searchRef = useRef<HTMLInputElement>(null)
  const results = useMemo(() => search(query.trim()), [query])
  const searching = query.trim().length > 0

  // Scroll to the anchor, or to the top when switching pages.
  useEffect(() => {
    if (searching) return
    const region = scrollRegion()
    if (anchor) {
      document.getElementById(anchor)?.scrollIntoView({ block: 'start' })
    } else {
      region?.scrollTo({ top: 0 })
    }
  }, [page.id, anchor, searching])

  // Esc leaves full-page mode.
  useEffect(() => {
    if (!full) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setFull(false)
    }
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('keydown', onKey)
    }
  }, [full])

  // "/" focuses search, like the rest of the app.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const typing = Boolean(target?.closest('input, textarea, select, [contenteditable="true"]'))
      if (event.key === '/' && !typing && !event.metaKey && !event.ctrlKey) {
        event.preventDefault()
        searchRef.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('keydown', onKey)
    }
  }, [])

  const index = pages.indexOf(page)
  const previous = index > 0 ? pages[index - 1] : undefined
  const next = index < pages.length - 1 ? pages[index + 1] : undefined
  const outline = outlineFor(page)

  return (
    <div
      className={cn(
        '@container',
        full && 'fixed inset-0 z-40 overflow-y-auto bg-bg px-4 py-6 md:px-8',
      )}
      data-docs-full={full ? '' : undefined}
    >
      {/* Docs header */}
      <header className="mb-6 flex flex-wrap items-center gap-x-6 gap-y-3 border-b border-border-soft pb-5">
        <div className="flex items-center gap-2.5">
          <span className="flex size-9 items-center justify-center rounded-app-sm bg-accent-soft text-accent-text">
            <BookOpen className="size-[18px]" />
          </span>
          <div>
            <p className="font-head text-lg font-semibold leading-tight">Documentation</p>
            <p className="text-xs text-dim">Guides and CLI reference for this server</p>
          </div>
        </div>
        <div className="ml-auto flex w-full items-center gap-2 @2xl:w-auto">
          <div className="min-w-0 flex-1 @2xl:w-72 @2xl:flex-none">
            <SearchBox
              inputRef={searchRef}
              value={query}
              onChange={setQuery}
              onSubmit={() => {
                const first = results[0]
                if (first) {
                  setQuery('')
                  window.location.hash = href(first.page.id, first.anchor)
                }
              }}
            />
          </div>
          <button
            type="button"
            onClick={() => {
              setFull((value) => !value)
            }}
            aria-pressed={full}
            title={full ? 'Exit full page (Esc)' : 'Show docs on the whole page'}
            className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-app-sm border border-border px-2.5 text-xs text-muted hover:bg-hover hover:text-text"
          >
            {full ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
            <span className="hidden sm:inline">{full ? 'Exit full page' : 'Full page'}</span>
          </button>
        </div>
      </header>

      <div className="@2xl:grid @2xl:grid-cols-[12.5rem_minmax(0,1fr)] @2xl:gap-8 @5xl:grid-cols-[13rem_minmax(0,1fr)_11rem] @5xl:gap-10">
        {/* Sidebar */}
        <aside className="hidden @2xl:block">
          <nav
            aria-label="Documentation"
            className="scroll-quiet sticky top-0 -ml-1 max-h-[calc(100dvh-8rem)] space-y-6 overflow-y-auto pb-6 pl-1"
          >
            {sections.map((section) => (
              <div key={section}>
                <p className="mb-2 px-2.5 text-2xs font-semibold uppercase tracking-[0.08em] text-dim">
                  {section}
                </p>
                <ul className="space-y-px">
                  {pages
                    .filter((candidate) => candidate.section === section)
                    .map((candidate) => {
                      const active = candidate.id === page.id && !searching
                      return (
                        <li key={candidate.id}>
                          <a
                            href={href(candidate.id)}
                            aria-current={active ? 'page' : undefined}
                            onClick={() => {
                              setQuery('')
                            }}
                            className={cn(
                              'relative flex items-start gap-2 rounded-app-sm px-2.5 py-1.5 text-sm transition-colors [&>svg]:mt-[3px]',
                              active
                                ? 'bg-hover font-medium text-text before:absolute before:inset-y-1.5 before:left-0 before:w-0.5 before:rounded-full before:bg-accent'
                                : 'text-muted hover:bg-hover-soft hover:text-text',
                            )}
                          >
                            {candidate.section !== 'CLI reference' && (
                              <candidate.icon
                                className={cn('size-3.5 shrink-0', active && 'text-accent-text')}
                              />
                            )}
                            <span className="leading-snug">{candidate.title}</span>
                          </a>
                        </li>
                      )
                    })}
                </ul>
              </div>
            ))}
          </nav>
        </aside>

        {/* Main column */}
        <div className="min-w-0">
          {/* Narrow: page picker instead of the sidebar */}
          <div className="mb-5 @2xl:hidden">
            <Select
              aria-label="Documentation page"
              value={page.id}
              onChange={(event) => {
                setQuery('')
                window.location.hash = href(event.target.value)
              }}
            >
              {sections.map((section) => (
                <optgroup key={section} label={section}>
                  {pages
                    .filter((candidate) => candidate.section === section)
                    .map((candidate) => (
                      <option key={candidate.id} value={candidate.id}>
                        {candidate.title}
                      </option>
                    ))}
                </optgroup>
              ))}
            </Select>
          </div>

          {searching ? (
            <SearchResults
              query={query.trim()}
              results={results}
              onPick={() => {
                setQuery('')
              }}
            />
          ) : (
            <article className="max-w-3xl">
              <PageHeader page={page} />
              <div className="mt-8">
                {page.kind === 'overview' && <Overview />}
                {page.guide && <GuideBody guide={page.guide} />}
                {page.group && <CommandList group={page.group} />}
                {page.topic && <TopicBody topic={page.topic} />}
                {page.kind === 'config' && <FlagList flags={reference.globalFlags} />}
              </div>
              <PagerNav previous={previous} next={next} />
            </article>
          )}
        </div>

        {/* On this page */}
        {!searching && outline.length > 1 && (
          <aside className="hidden @5xl:block">
            <OnThisPage pageId={page.id} items={outline} />
          </aside>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Pieces
// ---------------------------------------------------------------------------

function SearchBox({
  value,
  onChange,
  onSubmit,
  inputRef,
}: {
  value: string
  onChange: (value: string) => void
  onSubmit?: () => void
  inputRef?: React.RefObject<HTMLInputElement | null>
}) {
  return (
    <div className="relative">
      <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-dim" />
      <input
        ref={inputRef}
        type="search"
        aria-label="Search documentation"
        className="h-8 w-full rounded-app-sm border border-border bg-surface pl-8 pr-7 text-sm text-text placeholder:text-dim focus:border-accent-line focus:outline-none"
        placeholder="Search docs"
        value={value}
        onChange={(event) => {
          onChange(event.target.value)
        }}
        onKeyDown={(event) => {
          if (event.key === 'Escape') onChange('')
          if (event.key === 'Enter') onSubmit?.()
        }}
      />
      {!value && (
        <kbd className="pointer-events-none absolute right-2 top-1/2 -translate-y-1/2 rounded border border-border px-1 font-mono text-3xs text-dim">
          /
        </kbd>
      )}
    </div>
  )
}

function SearchResults({
  query,
  results,
  onPick,
}: {
  query: string
  results: SearchEntry[]
  onPick: () => void
}) {
  return (
    <section className="max-w-3xl" aria-live="polite">
      <h1 className="font-head text-xl font-semibold tracking-tight">
        {results.length === 0
          ? `Nothing matches “${query}”`
          : `${String(results.length)} ${results.length === 1 ? 'result' : 'results'} for “${query}”`}
      </h1>
      {results.length === 0 ? (
        <p className="mt-2 text-sm text-muted">
          Try a command name like <code className="font-mono">run</code>, a flag like{' '}
          <code className="font-mono">--env</code>, or a topic like{' '}
          <code className="font-mono">rotation</code>.
        </p>
      ) : (
        <ul className="mt-4 divide-y divide-border-soft overflow-hidden rounded-app border border-border bg-surface">
          {results.map((entry) => (
            <li key={`${entry.page.id}/${entry.anchor ?? ''}`}>
              <a
                href={href(entry.page.id, entry.anchor)}
                onClick={onPick}
                className="flex items-start gap-3 px-4 py-2.5 hover:bg-hover"
              >
                <entry.page.icon className="mt-0.5 size-4 shrink-0 text-dim" />
                <span className="min-w-0 flex-1">
                  <span
                    className={cn(
                      'block truncate text-sm font-medium text-text',
                      entry.mono && 'font-mono',
                    )}
                  >
                    {entry.title}
                  </span>
                  <span className="block truncate text-xs text-muted">{entry.context}</span>
                </span>
                <span className="shrink-0 text-2xs text-dim">{entry.page.title}</span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function PageHeader({ page }: { page: DocPage }) {
  if (page.kind === 'overview') {
    return (
      <div className="relative overflow-hidden rounded-app border border-border bg-surface px-6 py-7">
        <div
          aria-hidden
          className="pointer-events-none absolute -right-16 -top-24 size-72 rounded-full bg-accent opacity-[0.08] blur-3xl"
        />
        <p className="text-xs font-semibold uppercase tracking-[0.08em] text-accent-text">
          Get started
        </p>
        <h1 className="mt-1.5 font-head text-2xl font-semibold tracking-tight">
          Secrets your team and your agents can use safely
        </h1>
        <p className="mt-2 max-w-xl text-md leading-relaxed text-muted">
          Clavis keeps app secrets encrypted in one place, hands them to apps, CI, and AI agents at
          runtime, and records who saw or changed what.
        </p>
      </div>
    )
  }
  const meta =
    page.guide !== undefined
      ? `${String(page.guide.steps.length)} steps`
      : page.group !== undefined
        ? (() => {
            const count = groupCommands(page.group).length
            return `${String(count)} ${count === 1 ? 'command' : 'commands'}`
          })()
        : undefined
  return (
    <header>
      <nav aria-label="Breadcrumb" className="mb-3 flex items-center gap-1 text-xs text-dim">
        <a href={href('overview')} className="hover:text-text">
          Docs
        </a>
        <ChevronRight className="size-3" />
        <span>{page.section}</span>
      </nav>
      <div className="flex items-start gap-3">
        {page.kind === 'guide' && (
          <span className="mt-0.5 flex size-10 shrink-0 items-center justify-center rounded-app bg-accent-soft text-accent-text">
            <page.icon className="size-5" />
          </span>
        )}
        <div className="min-w-0">
          <h1 className="font-head text-2xl font-semibold tracking-tight">{page.title}</h1>
          <p className="mt-1.5 text-md leading-relaxed text-muted">{page.summary}</p>
        </div>
      </div>
      {(meta || (page.guide && page.guide.tags.length > 0)) && (
        <div className="mt-4 flex flex-wrap items-center gap-1.5">
          {meta && (
            <span className="rounded-full bg-elev px-2 py-0.5 text-2xs font-medium text-muted">
              {meta}
            </span>
          )}
          {page.guide?.tags.slice(0, 5).map((tag) => (
            <span
              key={tag}
              className="rounded-full border border-border-soft px-2 py-0.5 text-2xs text-dim"
            >
              {tag}
            </span>
          ))}
        </div>
      )}
    </header>
  )
}

function Overview() {
  const origin = window.location.origin
  const guidePages = pages.filter((page) => page.kind === 'guide')
  return (
    <div className="space-y-10">
      <div>
        <div className="rounded-app border border-border bg-surface p-4">
          <p className="text-sm font-semibold">Connect your terminal</p>
          <p className="mt-0.5 text-xs text-muted">
            Sign in once; the token is kept in your OS keychain.
          </p>
          <div className="mt-3 space-y-1.5">
            <CodeBlock code={`clavis login --server ${origin}`} />
            <CodeBlock code={'clavis init\nclavis run npm run dev'} />
          </div>
          <CertificateNote className="mt-3" />
          <a
            href={href('start')}
            className="mt-3 inline-flex items-center gap-1 text-sm font-medium text-accent-text hover:underline"
          >
            Full walkthrough <ArrowRight className="size-3.5" />
          </a>
        </div>
      </div>

      <section>
        <h2 className="text-md font-semibold">Guides</h2>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {guidePages.map((page) => (
            <a
              key={page.id}
              href={href(page.id)}
              className="group rounded-app border border-border bg-surface p-4 transition-colors hover:border-accent-line"
            >
              <span className="flex items-center gap-2">
                <span className="flex size-7 items-center justify-center rounded-app-sm bg-accent-soft text-accent-text">
                  <page.icon className="size-4" />
                </span>
                <span className="text-sm font-semibold">{page.title}</span>
                <ArrowRight className="ml-auto size-3.5 text-dim opacity-0 transition-opacity group-hover:opacity-100" />
              </span>
              <span className="mt-2 block text-xs leading-relaxed text-muted">{page.summary}</span>
            </a>
          ))}
        </div>
      </section>

      <section>
        <h2 className="text-md font-semibold">CLI reference</h2>
        <p className="mt-1 text-sm text-muted">
          All {reference.commands.length} commands, grouped by task, with flags and examples.{' '}
          <code className="font-mono">clavis &lt;command&gt; --help</code> prints the same examples.
        </p>
        <div className="mt-3 flex flex-wrap gap-1.5">
          {pages
            .filter((page) => page.section.startsWith('CLI'))
            .map((page) => (
              <a
                key={page.id}
                href={href(page.id)}
                className="rounded-full border border-border bg-surface px-3 py-1 text-xs text-muted transition-colors hover:border-accent-line hover:text-text"
              >
                {page.title}
              </a>
            ))}
        </div>
      </section>
    </div>
  )
}

function GuideBody({ guide }: { guide: DocGuide }) {
  return (
    <div>
      <ol className="relative space-y-9 border-l border-border-soft pl-7">
        {guide.steps.map((step, index) => (
          <li key={step.title} id={stepAnchor(index)} className="relative scroll-mt-6">
            <span className="absolute -left-[41px] top-0 flex size-7 items-center justify-center rounded-full border border-border bg-surface font-mono text-xs text-muted">
              {index + 1}
            </span>
            <h2 className="pt-0.5 text-md font-semibold">{step.title}</h2>
            <p className="mt-1.5 text-sm leading-relaxed text-muted">{step.body}</p>
            {step.snippets && step.snippets.length > 0 && (
              <div className="mt-3">
                <SnippetGroup snippets={step.snippets} />
              </div>
            )}
          </li>
        ))}
      </ol>
      {guide.note && (
        <aside className="mt-9 flex gap-3 rounded-app border border-amber-fg/30 bg-amber-bg/50 px-4 py-3">
          <Lightbulb className="mt-0.5 size-4 shrink-0 text-amber-fg" />
          <div>
            <p className="text-sm font-semibold text-amber-fg">{guide.note.title}</p>
            <p className="mt-1 text-sm leading-relaxed text-muted">{guide.note.body}</p>
          </div>
        </aside>
      )}
    </div>
  )
}

function Label({ children }: { children: React.ReactNode }) {
  return (
    <p className="mb-2 text-2xs font-semibold uppercase tracking-[0.08em] text-dim">{children}</p>
  )
}

const renderCode = (code: string, lang: string) => (
  <CodeBlock code={code} plain={lang !== '' && lang !== 'sh' && lang !== 'bash'} />
)

function CommandList({ group }: { group: RefGroup }) {
  return (
    <div className="space-y-5">
      {groupCommands(group).map((command) => (
        <CommandCard key={command.path} command={command} />
      ))}
    </div>
  )
}

function CommandChips({ paths }: { paths: string[] }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {paths.map((path) => {
        const target = commandByPath(path)
        if (!target) return null
        return (
          <a
            key={path}
            href={commandHref(target)}
            title={target.summary}
            className="rounded-app-sm border border-border bg-bg px-2 py-1 font-mono text-xs text-muted transition-colors hover:border-accent-line hover:text-text"
          >
            {path}
          </a>
        )
      })}
    </div>
  )
}

function CommandCard({ command }: { command: RefCommand }) {
  const parent = command.parent ? commandByPath(command.parent) : undefined
  return (
    <section
      id={commandAnchor(command)}
      className="group/cmd scroll-mt-6 overflow-hidden rounded-app border border-border bg-surface"
    >
      <header className="border-b border-border-soft px-5 pb-3.5 pt-4">
        {parent && (
          <a
            href={commandHref(parent)}
            className="mb-1 inline-flex items-center gap-1 font-mono text-2xs text-dim hover:text-text"
          >
            clavis {parent.path}
            <ChevronRight className="size-3" />
          </a>
        )}
        <div className="flex items-start gap-3">
          <h2 className="flex min-w-0 items-center gap-2 font-mono text-md font-semibold">
            <span className="text-dim">clavis</span>
            <span className="break-all">{command.path}</span>
            <a
              href={commandHref(command)}
              aria-label={`Link to clavis ${command.path}`}
              className="text-dim opacity-0 transition-opacity hover:text-accent-text focus:opacity-100 group-hover/cmd:opacity-100"
            >
              <Hash className="size-3.5" />
            </a>
          </h2>
          <a
            href={`${publicDocsBase}${command.slug}`}
            target="_blank"
            rel="noreferrer"
            title="Open this page on the public docs site"
            className="ml-auto mt-1 inline-flex shrink-0 items-center gap-1 text-2xs text-dim hover:text-text"
          >
            Docs site <ExternalLink className="size-3" />
          </a>
        </div>
        <p className="mt-1 text-sm text-text/90">{command.summary}</p>
      </header>
      <div className="space-y-5 px-5 py-4">
        <CodeBlock code={command.usage} label="Usage" plain />
        {command.description && command.description !== command.summary && (
          <DocMarkdown
            source={command.description}
            renderLink={renderLink}
            renderCode={renderCode}
          />
        )}
        {command.subcommands.length > 0 && (
          <div>
            <Label>Commands</Label>
            <CommandChips paths={command.subcommands} />
          </div>
        )}
        {command.examples.length > 0 && (
          <div>
            <Label>{command.examples.length === 1 ? 'Example' : 'Examples'}</Label>
            <ExampleList examples={command.examples} />
          </div>
        )}
        {command.flags.length > 0 && (
          <div>
            <Label>Flags</Label>
            <FlagList flags={command.flags} />
          </div>
        )}
        {command.notes && (
          <div>
            <Label>Details</Label>
            <DocMarkdown source={command.notes} renderLink={renderLink} renderCode={renderCode} />
          </div>
        )}
        {command.related.length > 0 && (
          <div>
            <Label>Related</Label>
            <CommandChips paths={command.related} />
          </div>
        )}
      </div>
    </section>
  )
}

function ExampleList({ examples }: { examples: RefExample[] }) {
  return (
    <div className="space-y-3">
      {examples.map((example) => (
        <div key={example.run}>
          {example.title && (
            <p className="mb-1.5 flex items-center gap-2 text-xs font-medium text-text">
              <span className="size-1.5 rounded-full bg-accent" aria-hidden />
              {example.title}
            </p>
          )}
          <CodeBlock code={example.run} />
        </div>
      ))}
    </div>
  )
}

function FlagList({ flags }: { flags: RefFlag[] }) {
  return (
    <dl className="divide-y divide-border-soft overflow-hidden rounded-app-sm border border-border bg-bg">
      {flags.map((flag) => (
        <div
          key={flag.name}
          id={`flag-${flag.name}`}
          className="grid scroll-mt-6 gap-1 px-3.5 py-2.5 @xl:grid-cols-[minmax(11rem,38%)_1fr] @xl:gap-4"
        >
          <dt className="font-mono text-xs font-medium text-text">{flagSignature(flag)}</dt>
          <dd className="text-sm text-muted [&::first-letter]:uppercase">
            {flag.usage}
            {flag.default && (
              <span className="ml-1.5 whitespace-nowrap text-2xs text-dim">
                Default{' '}
                <code className="rounded bg-elev px-1 py-px font-mono text-text">
                  {flag.default}
                </code>
              </span>
            )}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function TopicBody({ topic }: { topic: RefTopic }) {
  return (
    <div className="space-y-8">
      <DocMarkdown source={topic.body} renderLink={renderLink} renderCode={renderCode} />
      {topic.examples && topic.examples.length > 0 && (
        <div>
          <Label>{topic.examples.length === 1 ? 'Example' : 'Examples'}</Label>
          <ExampleList examples={topic.examples} />
        </div>
      )}
      <a
        href={`${publicDocsBase}${topic.id}`}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1 text-xs text-dim hover:text-text"
      >
        Open on the docs site <ExternalLink className="size-3" />
      </a>
    </div>
  )
}

function PagerNav({
  previous,
  next,
}: {
  previous?: DocPage | undefined
  next?: DocPage | undefined
}) {
  return (
    <nav
      aria-label="Previous and next page"
      className="mt-14 grid gap-3 border-t border-border-soft pt-6 sm:grid-cols-2"
    >
      {previous ? (
        <a
          href={href(previous.id)}
          className="group rounded-app border border-border px-4 py-3 transition-colors hover:border-accent-line"
        >
          <span className="flex items-center gap-1 text-2xs text-dim">
            <ArrowLeft className="size-3" /> Previous
          </span>
          <span className="mt-0.5 block text-sm font-medium group-hover:text-accent-text">
            {previous.title}
          </span>
        </a>
      ) : (
        <span />
      )}
      {next && (
        <a
          href={href(next.id)}
          className="group rounded-app border border-border px-4 py-3 text-right transition-colors hover:border-accent-line"
        >
          <span className="flex items-center justify-end gap-1 text-2xs text-dim">
            Next <ArrowRight className="size-3" />
          </span>
          <span className="mt-0.5 block text-sm font-medium group-hover:text-accent-text">
            {next.title}
          </span>
        </a>
      )}
    </nav>
  )
}

type OutlineItem = { id: string; label: string; mono: boolean }

function outlineFor(page: DocPage): OutlineItem[] {
  if (page.guide) {
    return page.guide.steps.map((step, index) => ({
      id: stepAnchor(index),
      label: step.title,
      mono: false,
    }))
  }
  if (page.group) {
    return groupCommands(page.group).map((command) => ({
      id: commandAnchor(command),
      label: command.path,
      mono: true,
    }))
  }
  return []
}

function OnThisPage({ pageId, items }: { pageId: string; items: OutlineItem[] }) {
  const [active, setActive] = useState(items[0]?.id ?? '')

  // Highlight the last heading that has scrolled past the top of the view.
  useEffect(() => {
    const region = scrollRegion()
    if (!region) return
    const update = () => {
      const top = region.getBoundingClientRect().top + 96
      let current = items[0]?.id ?? ''
      for (const item of items) {
        const element = document.getElementById(item.id)
        if (element && element.getBoundingClientRect().top <= top) current = item.id
      }
      if (region.scrollTop + region.clientHeight >= region.scrollHeight - 4) {
        current = items.at(-1)?.id ?? current
      }
      setActive(current)
    }
    update()
    region.addEventListener('scroll', update, { passive: true })
    return () => {
      region.removeEventListener('scroll', update)
    }
  }, [items])

  return (
    <nav
      aria-label="On this page"
      className="scroll-quiet sticky top-0 max-h-[calc(100dvh-7rem)] overflow-y-auto"
    >
      <p className="mb-2 text-2xs font-semibold uppercase tracking-[0.08em] text-dim">
        On this page
      </p>
      <ul className="space-y-px border-l border-border-soft">
        {items.map((item) => (
          <li key={item.id}>
            <a
              href={href(pageId, item.id)}
              className={cn(
                '-ml-px block truncate border-l py-1 pl-3 text-xs transition-colors',
                item.mono && 'font-mono',
                active === item.id
                  ? 'border-accent text-accent-text'
                  : 'border-transparent text-dim hover:text-text',
              )}
            >
              {item.label}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  )
}

function SnippetGroup({ snippets }: { snippets: DocSnippet[] }) {
  const [active, setActive] = useState(0)
  const labelled = snippets.every((snippet) => snippet.label)
  const current = snippets[Math.min(active, snippets.length - 1)]

  if (!labelled || snippets.length === 1) {
    return (
      <div className="space-y-1.5">
        {snippets.map((snippet) => (
          <CodeBlock key={snippet.code} code={snippet.code} />
        ))}
      </div>
    )
  }

  if (!current) return null

  return (
    <div className="overflow-hidden rounded-app-sm border border-border bg-bg">
      <div role="tablist" className="flex gap-1 border-b border-border px-2 pt-1.5">
        {snippets.map((snippet, index) => (
          <button
            key={snippet.label}
            type="button"
            role="tab"
            aria-selected={index === active}
            onClick={() => {
              setActive(index)
            }}
            className={cn(
              '-mb-px border-b-2 px-2 pb-1.5 text-xs font-medium transition-colors',
              index === active
                ? 'border-accent text-text'
                : 'border-transparent text-dim hover:text-text',
            )}
          >
            {snippet.label}
          </button>
        ))}
      </div>
      <CodeBlock code={current.code} bare />
    </div>
  )
}

function CodeBlock({
  code,
  label,
  bare = false,
  plain = false,
}: {
  code: string
  label?: string
  bare?: boolean
  /** No `$` prompt: for usage lines and non-shell code. */
  plain?: boolean
}) {
  return (
    <div className="group relative">
      {label && (
        <span className="pointer-events-none absolute left-3 top-2 text-3xs font-semibold uppercase tracking-[0.08em] text-dim">
          {label}
        </span>
      )}
      <pre
        className={cn(
          'whitespace-pre-wrap px-3 py-2.5 pr-10 font-mono [overflow-wrap:anywhere] text-xs leading-relaxed text-text',
          !bare && 'rounded-app-sm border border-border bg-bg',
          label && 'pt-6',
        )}
      >
        {code.split('\n').map((line, index, lines) => {
          const continuation = index > 0 && (lines[index - 1] ?? '').trimEnd().endsWith('\\')
          const noPrompt = plain || continuation || /^\s/.test(line) || line.trim() === ''
          return (
            <span key={`${String(index)}-${line}`} className="block">
              {line.startsWith('#') && !plain ? (
                <span className="text-dim">{line}</span>
              ) : (
                <>
                  {!noPrompt && <span className="select-none text-dim">$ </span>}
                  {line}
                </>
              )}
            </span>
          )
        })}
      </pre>
      <CopyButton value={code} />
    </div>
  )
}

function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      window.setTimeout(() => {
        setCopied(false)
      }, 1500)
    } catch {
      // Clipboard access can be denied; leave the button as-is.
    }
  }

  return (
    <button
      type="button"
      onClick={() => {
        void copy()
      }}
      aria-label={copied ? 'Copied' : 'Copy code'}
      className="absolute right-1.5 top-1.5 rounded border border-border bg-surface p-1 text-dim opacity-0 transition-opacity hover:text-text group-hover:opacity-100 focus:opacity-100"
    >
      {copied ? <Check className="size-3.5 text-accent-text" /> : <Copy className="size-3.5" />}
    </button>
  )
}
