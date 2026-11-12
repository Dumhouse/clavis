import { Check } from 'lucide-react'
import { useEffect, useId, useRef, useState, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

export type MenuItem =
  | {
      label: ReactNode
      icon?: ReactNode
      onSelect: () => void
      danger?: boolean
      disabled?: boolean
      hint?: ReactNode
      /** Second line under the label. */
      description?: ReactNode
      /** Makes the item a radio option (menuitemradio) with a check when true. */
      checked?: boolean
    }
  | 'separator'

/**
 * Menu is a small accessible dropdown: the trigger gets aria-haspopup and
 * aria-expanded, items are reachable with arrow keys, Escape closes and
 * returns focus to the trigger.
 */
export function Menu({
  trigger,
  items,
  align = 'end',
  label,
  triggerClassName,
  menuClassName,
}: {
  trigger: ReactNode
  items: MenuItem[]
  align?: 'start' | 'end'
  label: string
  triggerClassName?: string
  menuClassName?: string
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const id = useId()

  useEffect(() => {
    if (!open) return
    const first = menuRef.current?.querySelector<HTMLButtonElement>(
      '[role^="menuitem"]:not([disabled])',
    )
    first?.focus()
    function onPointer(event: PointerEvent) {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    return () => {
      document.removeEventListener('pointerdown', onPointer)
    }
  }, [open])

  function onKeyDown(event: React.KeyboardEvent) {
    const nodes = [
      ...(menuRef.current?.querySelectorAll<HTMLButtonElement>(
        '[role^="menuitem"]:not([disabled])',
      ) ?? []),
    ]
    const index = nodes.indexOf(document.activeElement as HTMLButtonElement)
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      setOpen(false)
      triggerRef.current?.focus()
    } else if (event.key === 'ArrowDown') {
      event.preventDefault()
      nodes[(index + 1) % nodes.length]?.focus()
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      nodes[(index - 1 + nodes.length) % nodes.length]?.focus()
    } else if (event.key === 'Tab') {
      setOpen(false)
    }
  }

  return (
    <div ref={rootRef} className="relative inline-flex" onKeyDown={open ? onKeyDown : undefined}>
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-label={label}
        className={triggerClassName}
        onClick={() => {
          setOpen((value) => !value)
        }}
      >
        {trigger}
      </button>
      {open && (
        <div
          ref={menuRef}
          id={id}
          role="menu"
          aria-label={label}
          className={cn(
            'absolute top-full z-40 mt-1 min-w-48 rounded-app-sm border border-border bg-elev p-1 shadow-app',
            align === 'end' ? 'right-0' : 'left-0',
            menuClassName,
          )}
        >
          {items.map((item, index) =>
            item === 'separator' ? (
              <div key={`sep-${String(index)}`} role="separator" className="my-1 h-px bg-border" />
            ) : (
              <button
                key={index}
                type="button"
                role={item.checked === undefined ? 'menuitem' : 'menuitemradio'}
                aria-checked={item.checked}
                disabled={item.disabled}
                className={cn(
                  'flex w-full gap-2 rounded px-2 text-left text-sm focus:bg-hover focus:outline-none disabled:opacity-50',
                  item.description ? 'items-start py-1.5' : 'min-h-8 items-center py-1',
                  item.danger ? 'text-danger' : 'text-text',
                  'hover:bg-hover',
                )}
                onClick={() => {
                  setOpen(false)
                  item.onSelect()
                }}
              >
                {item.icon && (
                  <span
                    className={cn(
                      'flex shrink-0 text-dim [&>svg]:size-3.5',
                      item.description && 'mt-[3px]',
                    )}
                  >
                    {item.icon}
                  </span>
                )}
                <span className="min-w-0 flex-1">
                  <span className="block">{item.label}</span>
                  {item.description && (
                    <span className="mt-0.5 block text-xs leading-snug text-dim">
                      {item.description}
                    </span>
                  )}
                </span>
                {item.hint && <span className="text-2xs text-dim">{item.hint}</span>}
                {item.checked !== undefined && (
                  <Check
                    aria-hidden
                    className={cn(
                      'size-3.5 shrink-0 text-accent-text',
                      item.description && 'mt-[3px]',
                      !item.checked && 'invisible',
                    )}
                  />
                )}
              </button>
            ),
          )}
        </div>
      )}
    </div>
  )
}
