import { BookOpen, Search } from 'lucide-react'
import { useMemo, useState } from 'react'

import {
  commandGroups,
  environmentVariables,
  exitCodes,
  globalFlags,
  type CommandGroup,
} from '@/lib/commands'

export function DocsPage() {
  const [filter, setFilter] = useState('')

  const groups = useMemo<CommandGroup[]>(() => {
    const needle = filter.trim().toLowerCase()
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
  }, [filter])

  return (
    <div className="space-y-6">
      <header>
        <div className="flex items-center gap-2">
          <BookOpen className="size-5 text-muted" />
          <h1 className="font-head text-[24px] font-semibold tracking-tight">Command reference</h1>
        </div>
        <p className="mt-1 text-[13px] text-muted">
          Every command in the <code className="font-mono">clavis</code> binary. Run{' '}
          <code className="font-mono">clavis &lt;command&gt; --help</code> in your terminal for the
          same information.
        </p>
      </header>

      <div className="relative">
        <Search className="absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-dim" />
        <input
          className="h-9 w-full rounded-app-sm border border-border bg-surface pl-9 pr-3 text-[12.5px] text-text placeholder:text-dim focus:border-accent-line focus:outline-none"
          placeholder="Search commands, flags, and descriptions…"
          value={filter}
          onChange={(event) => {
            setFilter(event.target.value)
          }}
        />
      </div>

      {groups.map((group) => (
        <section key={group.title} className="rounded-app border border-border bg-surface">
          <div className="border-b border-border px-4 py-3">
            <h2 className="text-[13px] font-semibold">{group.title}</h2>
            <p className="text-[11px] text-dim">{group.description}</p>
          </div>
          <div className="divide-y divide-border-soft">
            {group.commands.map((command) => (
              <article key={command.name} className="px-4 py-4">
                <div className="flex flex-wrap items-baseline gap-2">
                  <h3 className="font-mono text-[12.5px] font-semibold text-text">
                    {command.name}
                  </h3>
                  <code className="rounded bg-bg px-1.5 py-0.5 font-mono text-[10.5px] text-muted">
                    {command.usage}
                  </code>
                </div>
                <p className="mt-1 text-[12px] text-muted">{command.description}</p>
                {command.flags && (
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    {command.flags.map((flag) => (
                      <code
                        key={flag}
                        className="rounded-full border border-border bg-bg px-2 py-0.5 font-mono text-[10px] text-dim"
                      >
                        {flag}
                      </code>
                    ))}
                  </div>
                )}
                {command.examples && command.examples.length > 0 && (
                  <div className="mt-2 space-y-1">
                    {command.examples.map((example) => (
                      <pre
                        key={example}
                        className="overflow-x-auto rounded-app-sm border border-border bg-bg px-2.5 py-1.5 font-mono text-[11px] text-text"
                      >
                        {example}
                      </pre>
                    ))}
                  </div>
                )}
              </article>
            ))}
          </div>
        </section>
      ))}

      {groups.length === 0 && (
        <p className="rounded-app border border-dashed border-border bg-panel px-4 py-6 text-[12px] text-muted">
          No commands match “{filter}”.
        </p>
      )}

      <section className="grid gap-4 lg:grid-cols-2">
        <div className="rounded-app border border-border bg-surface">
          <h2 className="border-b border-border px-4 py-3 text-[13px] font-semibold">
            Global flags
          </h2>
          <dl className="divide-y divide-border-soft">
            {globalFlags.map(([flag, description]) => (
              <div key={flag} className="flex items-baseline gap-3 px-4 py-2">
                <dt className="w-44 shrink-0 font-mono text-[11px] text-text">{flag}</dt>
                <dd className="text-[11.5px] text-muted">{description}</dd>
              </div>
            ))}
          </dl>
        </div>
        <div className="rounded-app border border-border bg-surface">
          <h2 className="border-b border-border px-4 py-3 text-[13px] font-semibold">
            Environment variables
          </h2>
          <dl className="divide-y divide-border-soft">
            {environmentVariables.map(([name, description]) => (
              <div key={name} className="flex items-baseline gap-3 px-4 py-2">
                <dt className="w-48 shrink-0 font-mono text-[11px] text-text">{name}</dt>
                <dd className="text-[11.5px] text-muted">{description}</dd>
              </div>
            ))}
          </dl>
        </div>
        <div className="rounded-app border border-border bg-surface">
          <h2 className="border-b border-border px-4 py-3 text-[13px] font-semibold">Exit codes</h2>
          <dl className="divide-y divide-border-soft">
            {exitCodes.map(([code, meaning]) => (
              <div key={code} className="flex items-baseline gap-3 px-4 py-2">
                <dt className="w-12 shrink-0 font-mono text-[11px] text-text">{code}</dt>
                <dd className="text-[11.5px] text-muted">{meaning}</dd>
              </div>
            ))}
          </dl>
        </div>
        <div className="rounded-app border border-border bg-surface">
          <h2 className="border-b border-border px-4 py-3 text-[13px] font-semibold">More</h2>
          <div className="space-y-2 px-4 py-3 text-[11.5px] text-muted">
            <p>
              Configuration reference lives in <code className="font-mono">docs/quickstart.md</code>
              . Security model and threat model live in{' '}
              <code className="font-mono">docs/security.md</code>.
            </p>
            <p>
              The same reference is available in the repository at{' '}
              <code className="font-mono">docs/commands.md</code>.
            </p>
          </div>
        </div>
      </section>
    </div>
  )
}
