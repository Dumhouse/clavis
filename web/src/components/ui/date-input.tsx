import { CalendarDays, X } from 'lucide-react'
import { useRef, useState } from 'react'

import { controlClass } from '@/components/ui/field'
import { datePlaceholder, formatISODate, parseDateInput, useFormatPrefs } from '@/lib/format'
import { cn } from '@/lib/utils'

/**
 * A date field that shows and reads dates in the format chosen in Settings →
 * Appearance (a native date input always uses the browser's language). The
 * value is "2026-12-31" or "" for none. The calendar button opens the
 * browser's own picker.
 */
export function DateInput({
  value,
  onChange,
  id,
  readOnly,
  min,
  className,
  'aria-describedby': describedBy,
  'aria-invalid': invalid,
}: {
  value: string
  onChange: (value: string) => void
  id?: string | undefined
  readOnly?: boolean
  min?: string
  className?: string
  'aria-describedby'?: string | undefined
  'aria-invalid'?: boolean | undefined
}) {
  // Re-render when the format preference changes.
  useFormatPrefs()
  const picker = useRef<HTMLInputElement>(null)
  const [text, setText] = useState(() => formatISODate(value))
  const [bad, setBad] = useState(false)

  // Follow the value when it changes from outside (a picker choice, a reset,
  // a format change), but not while the typed text already means that value.
  const shown = parseDateInput(text) === value || (bad && text !== '') ? text : formatISODate(value)
  if (shown !== text) setText(shown)

  function commit(next: string) {
    setText(next)
    if (next.trim() === '') {
      setBad(false)
      onChange('')
      return
    }
    const parsed = parseDateInput(next)
    setBad(parsed === null)
    if (parsed !== null) onChange(parsed)
  }

  return (
    <div className={cn('relative flex items-center', className)}>
      <input
        id={id}
        type="text"
        inputMode="numeric"
        autoComplete="off"
        value={text}
        readOnly={readOnly}
        placeholder={datePlaceholder()}
        aria-describedby={describedBy}
        aria-invalid={invalid || bad ? true : undefined}
        title={bad ? `Use ${datePlaceholder()}` : undefined}
        onChange={(event) => {
          commit(event.target.value)
        }}
        onBlur={() => {
          const parsed = parseDateInput(text)
          if (parsed) setText(formatISODate(parsed))
        }}
        className={cn(controlClass, 'h-9 w-full px-2.5 pr-16', bad && 'border-danger')}
      />
      {!readOnly && (
        <div className="absolute right-1 flex items-center">
          {value && (
            <button
              type="button"
              aria-label="Clear date"
              className="rounded p-1 text-dim hover:bg-hover hover:text-text"
              onClick={() => {
                commit('')
              }}
            >
              <X className="size-3.5" />
            </button>
          )}
          <button
            type="button"
            aria-label="Pick a date"
            className="rounded p-1 text-dim hover:bg-hover hover:text-text"
            onClick={() => {
              try {
                picker.current?.showPicker()
              } catch {
                picker.current?.focus()
              }
            }}
          >
            <CalendarDays className="size-3.5" />
          </button>
          {/* Holds the browser's picker; kept in layout so it can open. */}
          <input
            ref={picker}
            type="date"
            tabIndex={-1}
            aria-hidden="true"
            value={value}
            min={min}
            className="pointer-events-none absolute right-0 bottom-0 size-px opacity-0"
            onChange={(event) => {
              setBad(false)
              setText(formatISODate(event.target.value))
              onChange(event.target.value)
            }}
          />
        </div>
      )}
    </div>
  )
}
