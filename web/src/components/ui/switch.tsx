import { cn } from '@/lib/utils'

/** Switch is an on/off toggle (role="switch") with a correctly anchored knob. */
export function Switch({
  checked,
  onChange,
  label,
  disabled,
  title,
  className,
}: {
  checked: boolean
  onChange: (checked: boolean) => void
  label: string
  disabled?: boolean | undefined
  title?: string | undefined
  className?: string | undefined
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      title={title}
      disabled={disabled}
      onClick={() => {
        onChange(!checked)
      }}
      className={cn(
        'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-transparent p-0.5 transition-colors',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-line focus-visible:ring-offset-1 focus-visible:ring-offset-bg',
        'disabled:cursor-not-allowed disabled:opacity-60',
        checked ? 'bg-accent' : 'bg-border',
        className,
      )}
    >
      <span
        aria-hidden
        className={cn(
          'block size-3.5 rounded-full bg-white shadow-sm transition-transform duration-150',
          checked ? 'translate-x-4' : 'translate-x-0',
        )}
      />
    </button>
  )
}
