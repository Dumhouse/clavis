import { X } from 'lucide-react'
import { useEffect, useId, useRef, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

/**
 * Modal renders a native <dialog> opened with showModal(), so the browser
 * handles the top layer, focus containment, the inert background and Escape.
 * The first heading inside becomes the accessible name.
 *
 * Render it conditionally ({open && <Modal …/>}); it opens on mount.
 */
export function Modal({
  onClose,
  className,
  variant = 'center',
  label,
  dismissible = true,
  children,
}: {
  onClose: () => void
  className?: string
  variant?: 'center' | 'sheet'
  /** Accessible name when the content has no heading. */
  label?: string
  /** When false, Escape does not close (for flows that must finish). */
  dismissible?: boolean
  children: ReactNode
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const onCloseRef = useRef(onClose)
  const dismissibleRef = useRef(dismissible)
  useEffect(() => {
    onCloseRef.current = onClose
    dismissibleRef.current = dismissible
  })
  const fallbackId = useId()

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (!dialog.open) dialog.showModal()
    if (!label) {
      const heading = dialog.querySelector('h1, h2, h3')
      if (heading) {
        if (!heading.id) heading.id = fallbackId
        dialog.setAttribute('aria-labelledby', heading.id)
      }
    }
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    return () => {
      if (dialog.open) dialog.close()
      previous?.focus()
    }
  }, [label, fallbackId])

  return (
    <dialog
      ref={ref}
      className="app-modal"
      data-variant={variant}
      aria-label={label}
      aria-modal="true"
      onCancel={(event) => {
        event.preventDefault()
        if (dismissibleRef.current) onCloseRef.current()
      }}
    >
      <div
        className={cn(
          'flex flex-col border border-border bg-surface shadow-app',
          variant === 'center'
            ? 'max-h-[calc(100dvh-2rem)] w-full max-w-lg overflow-y-auto rounded-app p-5'
            : 'h-full w-full max-w-xl border-y-0 border-r-0 p-0',
          className,
        )}
      >
        {children}
      </div>
    </dialog>
  )
}

/** Standard modal header: title, optional description, and a close button. */
export function ModalHeader({
  title,
  description,
  onClose,
  className,
  children,
}: {
  title: ReactNode
  description?: ReactNode
  onClose?: () => void
  className?: string
  children?: ReactNode
}) {
  return (
    <div className={cn('flex items-start justify-between gap-3', className)}>
      <div className="min-w-0">
        <h2 className="font-head text-lg font-semibold">{title}</h2>
        {description && <p className="mt-0.5 text-sm text-muted">{description}</p>}
        {children}
      </div>
      {onClose && (
        <Button variant="ghost" size="icon" onClick={onClose} aria-label="Close">
          <X className="size-4" />
        </Button>
      )}
    </div>
  )
}

/** Right-aligned action row at the bottom of a modal. */
export function ModalFooter({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn('mt-4 flex justify-end gap-2', className)}>{children}</div>
}
