import { ChevronDown, Lightbulb } from 'lucide-react'
import { useState, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

export type IntroStep = { title: string; body: ReactNode }

/**
 * PageIntro explains what a page is for in plain words: one sentence, a few
 * numbered steps, and an optional "use it when" line. It starts open the first
 * time and remembers when someone collapses it.
 */
export function PageIntro({
  id,
  summary,
  steps,
  when,
  footer,
}: {
  /** Storage key; one per page. */
  id: string
  summary: ReactNode
  steps: IntroStep[]
  when?: ReactNode
  footer?: ReactNode
}) {
  const storageKey = `clavis.intro.${id}`
  const [open, setOpen] = useState(() => {
    try {
      return window.localStorage.getItem(storageKey) !== 'closed'
    } catch {
      return true
    }
  })

  function toggle() {
    const next = !open
    setOpen(next)
    try {
      window.localStorage.setItem(storageKey, next ? 'open' : 'closed')
    } catch {
      // Storage can be unavailable (private mode); the toggle still works.
    }
  }

  return (
    <section
      aria-label="How this page works"
      className={cn(
        'rounded-app border transition-colors',
        open ? 'border-accent-line/60 bg-accent-soft/25' : 'border-border-soft bg-transparent',
      )}
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={toggle}
        className="flex w-full items-center gap-2 px-4 py-2.5 text-left"
      >
        <Lightbulb className="size-4 shrink-0 text-accent-text" />
        <span className="text-sm font-medium">How this works</span>
        {!open && <span className="truncate text-sm text-dim">— {summary}</span>}
        <ChevronDown
          className={cn(
            'ml-auto size-4 shrink-0 text-dim transition-transform',
            open && 'rotate-180',
          )}
        />
      </button>
      {open && (
        <div className="px-4 pb-4">
          <p className="max-w-3xl text-sm leading-relaxed text-muted">{summary}</p>
          <ol className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {steps.map((step, index) => (
              <li key={step.title} className="flex gap-2.5">
                <span className="flex size-6 shrink-0 items-center justify-center rounded-full border border-accent-line bg-surface font-mono text-xs text-accent-text">
                  {index + 1}
                </span>
                <span className="min-w-0">
                  <span className="block text-sm font-medium">{step.title}</span>
                  <span className="mt-0.5 block text-xs leading-relaxed text-muted">
                    {step.body}
                  </span>
                </span>
              </li>
            ))}
          </ol>
          {when && (
            <p className="mt-3 text-xs text-muted">
              <span className="font-semibold text-text">Use it when: </span>
              {when}
            </p>
          )}
          {footer && <div className="mt-3">{footer}</div>}
        </div>
      )}
    </section>
  )
}

/** Small inline code for intros. */
export function Cmd({ children }: { children: ReactNode }) {
  return <code className="rounded bg-bg px-1 py-0.5 font-mono text-2xs text-text">{children}</code>
}
