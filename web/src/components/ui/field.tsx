import {
  useId,
  type ComponentProps,
  type ReactElement,
  type ReactNode,
  cloneElement,
  isValidElement,
} from 'react'

import { cn } from '@/lib/utils'

const control =
  'w-full rounded-app-sm border border-border bg-bg text-sm text-text placeholder:text-dim transition-colors hover:border-dim/60 focus:border-accent-line focus:outline-none focus-visible:outline-none focus:ring-2 focus:ring-accent-soft disabled:opacity-60 read-only:text-muted aria-[invalid=true]:border-danger'

export const controlClass = control

export function Input({
  className,
  mono,
  ...props
}: ComponentProps<'input'> & { mono?: boolean }) {
  return <input className={cn(control, 'h-9 px-2.5', mono && 'font-mono', className)} {...props} />
}

export function Textarea({
  className,
  mono,
  ...props
}: ComponentProps<'textarea'> & { mono?: boolean }) {
  return (
    <textarea className={cn(control, 'px-2.5 py-2', mono && 'font-mono', className)} {...props} />
  )
}

export function Select({
  className,
  size = 'md',
  ...props
}: Omit<ComponentProps<'select'>, 'size'> & { size?: 'sm' | 'md' }) {
  return (
    <select
      className={cn(
        control,
        size === 'sm' ? 'h-7 w-auto px-1.5 text-xs' : 'h-9 px-2',
        'cursor-pointer',
        className,
      )}
      {...props}
    />
  )
}

/**
 * Field wires a label, hint and error to one control: it sets id,
 * aria-describedby and aria-invalid on the child.
 */
export function Field({
  label,
  hint,
  error,
  className,
  children,
}: {
  label: ReactNode
  hint?: ReactNode
  error?: ReactNode
  className?: string
  children: ReactElement<{
    id?: string | undefined
    'aria-describedby'?: string | undefined
    'aria-invalid'?: boolean | undefined
  }>
}) {
  const id = useId()
  const hintId = `${id}-hint`
  const errorId = `${id}-error`
  const describedBy = [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(' ')
  const control = isValidElement(children)
    ? cloneElement(children, {
        id: children.props.id ?? id,
        'aria-describedby': describedBy || undefined,
        'aria-invalid': error ? true : undefined,
      })
    : children
  return (
    <div className={cn('block', className)}>
      <label
        htmlFor={children.props.id ?? id}
        className="mb-1 block text-xs font-medium text-muted"
      >
        {label}
      </label>
      {control}
      {hint && !error && (
        <p id={hintId} className="mt-1 text-2xs text-dim">
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} className="mt-1 text-2xs text-danger">
          {error}
        </p>
      )}
    </div>
  )
}

/** Inline error/notice box. */
export function Notice({
  tone = 'danger',
  className,
  children,
}: {
  tone?: 'danger' | 'warning' | 'info' | 'success'
  className?: string
  children: ReactNode
}) {
  return (
    <div
      role={tone === 'danger' ? 'alert' : 'status'}
      className={cn(
        'rounded-app-sm px-3 py-2 text-xs',
        tone === 'danger' && 'bg-danger-bg text-danger',
        tone === 'warning' && 'bg-amber-bg text-amber-fg',
        tone === 'info' && 'bg-blue-bg text-blue-fg',
        tone === 'success' && 'bg-success-bg text-success',
        className,
      )}
    >
      {children}
    </div>
  )
}
