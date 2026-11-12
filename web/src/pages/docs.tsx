import {
  BookOpen,
  Bot,
  Braces,
  Check,
  Copy,
  GitBranch,
  KeyRound,
  RefreshCw,
  Rocket,
  Search,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react'
import { useMemo, useState } from 'react'

import {
  commandGroups,
  environmentVariables,
  exitCodes,
  globalFlags,
  type CommandGroup,
} from '@/lib/commands'
import { guides, type DocGuide, type DocSnippet } from '@/lib/docs-content'
import { cn } from '@/lib/utils'

const guideIcons: Record<string, LucideIcon> = {
  start: Rocket,
  schema: Braces,
  agents: Bot,
  sync: RefreshCw,
  workflow: GitBranch,
  dynamic: KeyRound,
  security: ShieldCheck,
}

export function DocsPage() {
  const [filter, setFilter] = useState('')
  const needle = filter.trim().toLowerCase()

  const matchingGuides = useMemo<DocGuide[]>(() => {
    if (!needle) return guides
    return guides.filter((guide) => guideHaystack(guide).includes(needle))
  }, [needle])

  const groups = useMemo<CommandGroup[]>(() => {
    if (!needle) return commandGroups
    return commandGroups
      .map((group) => ({
        ...group,
        commands: group.commands.filter(
          (command) =>
            command.name.toLowerCase().includes(needle) ||
            command.usage.toLowerCase().includes(needle) ||
            command.description.toLowerCase().includes(needle),
        ),
      }))
      .filter((group) => group.commands.length > 0)
  }, [needle])

  const empty = matchingGuides.length === 0 && groups.length === 0

  return (
    <div className="space-y-6">
      <header>
        <div className="flex items-center gap-2">
          <BookOpen className="size-5 text-muted" />
          <h1 className="font-head text-[25px] font-semibold tracking-tight">Documentation</h1>
        </div>
        <p className="mt-1 max-w-2xl text-[14px] text-muted">
          Guides for the CLI, the web UI, CI, and AI agents. Commands run against this instance; the
          same reference ships in <code className="font-mono">docs/</code>.
        </p>
      </header>

      <div className="relative">
        <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-dim" />
        <input
          className="h-10 w-full rounded-app-sm border border-border bg-surface pl-10 pr-3 text-[13.5px] text-text placeholder:text-dim focus:border-accent-line focus:outline-none"
          placeholder="Search guides, commands, flags, and examples…"
          value={filter}
          onChange={(event) => {
            setFilter(event.target.value)
          }}
        />
      </div>

      {!needle && (
        <nav className="flex flex-wrap gap-2">
          {guides.map((guide) => {
            const Icon = guideIcons[guide.id] ?? BookOpen
            return (
              <a
                key={guide.id}
                href={`#guide-${guide.id}`}
                className="inline-flex items-center gap-1.5 rounded-full border border-border bg-surface px-3 py-1.5 text-[12.5px] text-muted transition-colors hover:border-accent-line hover:text-text"
              >
                <Icon className="size-3.5" />
                {guide.title}
              </a>
            )
          })}
        </nav>
      )}

      {matchingGuides.length > 0 && (
        <section className="space-y-4">
          <h2 className="text-[12px] font-semibold uppercase tracking-[0.08em] text-dim">
            {needle ? `${String(matchingGuides.length)} matching guides` : 'Guides'}
          </h2>
          {matchingGuides.map((guide) => (
            <GuideCard key={guide.id} guide={guide} />
          ))}
        </section>
      )}

      {groups.length > 0 && (
        <section className="space-y-4">
          <div className="flex items-baseline justify-between gap-3">
            <h2 className="text-[12px] font-semibold uppercase tracking-[0.08em] text-dim">
              Command reference
            </h2>
            <p className="text-[12.5px] text-dim">
              <code className="font-mono">clavis &lt;command&gt; --help</code> prints the same text.
            </p>
          </div>
          {groups.map((group) => (
            <div key={group.title} className="rounded-app border border-border bg-surface">
              <div className="border-b border-border px-4 py-3">
                <h3 className="text-[14px] font-semibold">{group.title}</h3>
                <p className="text-[12.5px] text-dim">{group.description}</p>
              </div>
              <div className="divide-y divide-border-soft">
                {group.commands.map((command) => (
                  <article key={command.name} className="px-4 py-4">
                    <div className="flex flex-wrap items-baseline gap-2">
                      <h4 className="font-mono text-[13.5px] font-semibold text-text">
                        {command.name}
                      </h4>
                      <code className="rounded bg-bg px-1.5 py-0.5 font-mono text-[11.5px] text-muted">
                        {command.usage}
                      </code>
                    </div>
                    <p className="mt-1 text-[13px] text-muted">{command.description}</p>
                    {command.flags && (
                      <div className="mt-2 flex flex-wrap gap-1.5">
                        {command.flags.map((flag) => (
                          <code
                            key={flag}
                            className="rounded-full border border-border bg-bg px-2 py-0.5 font-mono text-[11px] text-dim"
                          >
                            {flag}
                          </code>
                        ))}
                      </div>
                    )}
                    {command.examples && command.examples.length > 0 && (
                      <div className="mt-2 space-y-1">
                        {command.examples.map((example) => (
                          <CodeBlock key={example} code={example} compact />
                        ))}
                      </div>
                    )}
                  </article>
                ))}
              </div>
            </div>
          ))}
        </section>
      )}

      {empty && (
        <p className="rounded-app border border-dashed border-border bg-panel px-4 py-6 text-[13px] text-muted">
          No guides or commands match “{filter}”.
        </p>
      )}

      {!needle && (
        <section className="grid gap-4 lg:grid-cols-2">
          <div className="rounded-app border border-border bg-surface">
            <h2 className="border-b border-border px-4 py-3 text-[14px] font-semibold">
              Global flags
            </h2>
            <dl className="divide-y divide-border-soft">
              {globalFlags.map(([flag, description]) => (
                <div key={flag} className="flex items-baseline gap-3 px-4 py-2">
                  <dt className="w-44 shrink-0 font-mono text-[12px] text-text">{flag}</dt>
                  <dd className="text-[12.5px] text-muted">{description}</dd>
                </div>
              ))}
            </dl>
          </div>
          <div className="rounded-app border border-border bg-surface">
            <h2 className="border-b border-border px-4 py-3 text-[14px] font-semibold">
              Environment variables
            </h2>
            <dl className="divide-y divide-border-soft">
              {environmentVariables.map(([name, description]) => (
                <div key={name} className="flex items-baseline gap-3 px-4 py-2">
                  <dt className="w-48 shrink-0 font-mono text-[12px] text-text">{name}</dt>
                  <dd className="text-[12.5px] text-muted">{description}</dd>
                </div>
              ))}
            </dl>
          </div>
          <div className="rounded-app border border-border bg-surface">
            <h2 className="border-b border-border px-4 py-3 text-[14px] font-semibold">
              Exit codes
            </h2>
            <dl className="divide-y divide-border-soft">
              {exitCodes.map(([code, meaning]) => (
                <div key={code} className="flex items-baseline gap-3 px-4 py-2">
                  <dt className="w-12 shrink-0 font-mono text-[12px] text-text">{code}</dt>
                  <dd className="text-[12.5px] text-muted">{meaning}</dd>
                </div>
              ))}
            </dl>
          </div>
          <div className="rounded-app border border-border bg-surface">
            <h2 className="border-b border-border px-4 py-3 text-[14px] font-semibold">
              In the repository
            </h2>
            <div className="space-y-2 px-4 py-3 text-[12.5px] text-muted">
              <p>
                Configuration and self-hosting:{' '}
                <code className="font-mono">docs/quickstart.md</code>
              </p>
              <p>
                Command reference: <code className="font-mono">docs/commands.md</code>
              </p>
              <p>
                Agents and MCP: <code className="font-mono">docs/agents.md</code>
              </p>
              <p>
                Security model: <code className="font-mono">docs/security.md</code>
              </p>
            </div>
          </div>
        </section>
      )}
    </div>
  )
}

function GuideCard({ guide }: { guide: DocGuide }) {
  const Icon = guideIcons[guide.id] ?? BookOpen
  return (
    <article
      id={`guide-${guide.id}`}
      className="scroll-mt-6 rounded-app border border-border bg-surface"
    >
      <header className="border-b border-border px-4 py-3">
        <div className="flex items-center gap-2">
          <span className="flex size-7 items-center justify-center rounded-app-sm bg-accent-soft text-accent-text">
            <Icon className="size-4" />
          </span>
          <h3 className="text-[15px] font-semibold">{guide.title}</h3>
        </div>
        <p className="mt-1.5 text-[13px] text-muted">{guide.summary}</p>
      </header>

      <ol className="divide-y divide-border-soft">
        {guide.steps.map((step, index) => (
          <li key={step.title} className="px-4 py-4">
            <div className="flex items-baseline gap-2.5">
              <span className="flex size-5 shrink-0 items-center justify-center rounded-full border border-border bg-bg font-mono text-[11px] text-muted">
                {index + 1}
              </span>
              <h4 className="text-[13.5px] font-semibold">{step.title}</h4>
            </div>
            <div className="mt-1.5 pl-[30px]">
              <p className="text-[13px] text-muted">{step.body}</p>
              {step.snippets && step.snippets.length > 0 && (
                <div className="mt-2.5">
                  <SnippetGroup snippets={step.snippets} />
                </div>
              )}
            </div>
          </li>
        ))}
      </ol>

      {guide.note && (
        <div className="border-t border-border bg-bg px-4 py-3">
          <p className="text-[13px] font-semibold text-amber-fg">{guide.note.title}</p>
          <p className="mt-1 text-[12.5px] text-muted">{guide.note.body}</p>
        </div>
      )}
    </article>
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
    <div>
      <div className="mb-1.5 flex flex-wrap gap-1.5">
        {snippets.map((snippet, index) => (
          <button
            key={snippet.label}
            type="button"
            onClick={() => {
              setActive(index)
            }}
            className={cn(
              'rounded-full border px-2.5 py-1 text-[12px] font-medium transition-colors',
              index === active
                ? 'border-accent-line bg-accent-soft text-accent-text'
                : 'border-border bg-bg text-muted hover:text-text',
            )}
          >
            {snippet.label}
          </button>
        ))}
      </div>
      <CodeBlock code={current.code} />
    </div>
  )
}

function CodeBlock({ code, compact = false }: { code: string; compact?: boolean }) {
  return (
    <div className="group relative">
      <pre
        className={cn(
          'overflow-x-auto rounded-app-sm border border-border bg-bg pr-10 font-mono text-text',
          compact ? 'px-2.5 py-1.5 text-[12px]' : 'px-3 py-2 text-[12.5px]',
        )}
      >
        {code}
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

function guideHaystack(guide: DocGuide): string {
  return [
    guide.title,
    guide.summary,
    guide.tags.join(' '),
    guide.note?.title ?? '',
    guide.note?.body ?? '',
    ...guide.steps.flatMap((step) => [
      step.title,
      step.body,
      ...(step.snippets ?? []).flatMap((snippet) => [snippet.label ?? '', snippet.code]),
    ]),
  ]
    .join(' ')
    .toLowerCase()
}
