import { Info } from 'lucide-react'
import { useEffect, useId, useRef, useState, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

/**
 * Small click-to-open explanation for a single concept. Complements the
 * page-level PageIntro cards with help exactly where a control lives.
 */
export function InfoHint({
  label,
  children,
  align = 'left',
  className,
}: {
  /** Accessible name for the trigger and popover. */
  label: string
  children: ReactNode
  align?: 'left' | 'right'
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const id = useId()
  const ref = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    if (!open) return
    function onPointer(event: MouseEvent) {
      if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false)
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <span ref={ref} className={cn('relative inline-flex align-middle', className)}>
      <button
        type="button"
        aria-label={label}
        aria-expanded={open}
        aria-controls={id}
        onClick={() => {
          setOpen((value) => !value)
        }}
        className="inline-flex size-5 cursor-help items-center justify-center rounded-full text-dim transition-colors hover:bg-hover hover:text-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      >
        <Info className="size-3.5" />
      </button>
      {open && (
        <span
          id={id}
          role="dialog"
          aria-label={label}
          className={cn(
            'absolute top-full z-40 mt-1.5 w-64 rounded-app-sm border border-border bg-surface p-2.5 text-2xs font-normal leading-relaxed text-muted shadow-app',
            align === 'right' ? 'right-0' : 'left-0',
          )}
        >
          {children}
        </span>
      )}
    </span>
  )
}
