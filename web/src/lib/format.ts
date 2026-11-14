import { useSyncExternalStore } from 'react'

/*
 * Date and time formatting with a per-browser preference (Settings →
 * Appearance). Every date in the app goes through these helpers so the
 * choice applies everywhere.
 */

export type DateFormat = 'auto' | 'dmy-dot' | 'dmy-slash' | 'mdy' | 'iso'
export type TimeFormat = 'auto' | '24h' | '12h'

export const dateFormats: { value: DateFormat; label: string }[] = [
  { value: 'auto', label: 'Browser language' },
  { value: 'dmy-dot', label: '31.12.2026' },
  { value: 'dmy-slash', label: '31/12/2026' },
  { value: 'mdy', label: '12/31/2026' },
  { value: 'iso', label: '2026-12-31' },
]
export const timeFormats: { value: TimeFormat; label: string }[] = [
  { value: 'auto', label: 'Browser language' },
  { value: '24h', label: '24-hour (17:30)' },
  { value: '12h', label: '12-hour (5:30 PM)' },
]

const dateKey = 'clavis.dateFormat'
const timeKey = 'clavis.timeFormat'
const event = 'clavis:format'

type Prefs = { date: DateFormat; time: TimeFormat }

function read(): Prefs {
  let date: DateFormat = 'auto'
  let time: TimeFormat = 'auto'
  try {
    const savedDate = localStorage.getItem(dateKey)
    if (dateFormats.some((option) => option.value === savedDate)) date = savedDate as DateFormat
    const savedTime = localStorage.getItem(timeKey)
    if (timeFormats.some((option) => option.value === savedTime)) time = savedTime as TimeFormat
  } catch {
    // Storage can be unavailable; use the browser's formats.
  }
  return { date, time }
}

let prefs = read()

export function setFormatPrefs(next: Partial<Prefs>) {
  prefs = { ...prefs, ...next }
  try {
    localStorage.setItem(dateKey, prefs.date)
    localStorage.setItem(timeKey, prefs.time)
  } catch {
    // The choice still applies for this visit.
  }
  window.dispatchEvent(new CustomEvent(event))
}

function subscribe(callback: () => void) {
  window.addEventListener(event, callback)
  return () => {
    window.removeEventListener(event, callback)
  }
}

/** The current preferences; components using it re-render when they change. */
export function useFormatPrefs(): Prefs {
  return useSyncExternalStore(subscribe, () => prefs)
}

type DateInput = string | number | Date | null | undefined

function toDate(value: DateInput): Date | null {
  if (value === null || value === undefined || value === '') return null
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

const pad = (n: number) => String(n).padStart(2, '0')

/** A date such as 31.12.2026, following the preference. */
export function formatDate(value: DateInput, fallback = '—'): string {
  const date = toDate(value)
  if (!date) return fallback
  const d = pad(date.getDate())
  const m = pad(date.getMonth() + 1)
  const y = String(date.getFullYear())
  switch (prefs.date) {
    case 'dmy-dot':
      return `${d}.${m}.${y}`
    case 'dmy-slash':
      return `${d}/${m}/${y}`
    case 'mdy':
      return `${m}/${d}/${y}`
    case 'iso':
      return `${y}-${m}-${d}`
    default:
      return date.toLocaleDateString()
  }
}

/** A time such as 17:30, following the preference. */
export function formatTime(value: DateInput, fallback = '—', seconds = false): string {
  const date = toDate(value)
  if (!date) return fallback
  if (prefs.time === 'auto') {
    return date.toLocaleTimeString(undefined, {
      hour: 'numeric',
      minute: '2-digit',
      ...(seconds ? { second: '2-digit' } : {}),
    })
  }
  return date.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    ...(seconds ? { second: '2-digit' } : {}),
    hour12: prefs.time === '12h',
  })
}

type Order = 'dmy' | 'mdy' | 'ymd'

/** The day/month/year order of the preferred date format. */
function dateOrder(): Order {
  switch (prefs.date) {
    case 'dmy-dot':
    case 'dmy-slash':
      return 'dmy'
    case 'mdy':
      return 'mdy'
    case 'iso':
      return 'ymd'
    default: {
      const parts = new Intl.DateTimeFormat().formatToParts(new Date(2026, 11, 31))
      const types = parts.map((part) => part.type).filter((type) => type !== 'literal')
      if (types[0] === 'year') return 'ymd'
      return types[0] === 'month' ? 'mdy' : 'dmy'
    }
  }
}

/** What to type in a date field, for example "dd.mm.yyyy". */
export function datePlaceholder(): string {
  const sample = formatDate(new Date(2026, 11, 31))
  return sample.replace('2026', 'yyyy').replace('12', 'mm').replace('31', 'dd')
}

/** A date-only value ("2026-12-31") shown in the preferred format. */
export function formatISODate(iso: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso)
  if (!match) return ''
  return formatDate(new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3])))
}

/**
 * Reads a typed date in the preferred order (any of . / - as separators) and
 * returns "2026-12-31", or null when it isn't a real date.
 */
export function parseDateInput(text: string): string | null {
  const parts = text.trim().split(/[./\-\s]+/)
  if (parts.length !== 3 || parts.some((part) => !/^\d+$/.test(part))) return null
  const [a, b, c] = parts.map(Number) as [number, number, number]
  // A four-digit first part is always year-first, whatever the setting.
  const order: Order = (parts[0] ?? '').length === 4 ? 'ymd' : dateOrder()
  const [year, month, day] = order === 'ymd' ? [a, b, c] : order === 'mdy' ? [c, a, b] : [c, b, a]
  if (year < 1000 || year > 9999) return null
  const date = new Date(year, month - 1, day)
  if (date.getFullYear() !== year || date.getMonth() !== month - 1 || date.getDate() !== day)
    return null
  return `${String(year)}-${pad(month)}-${pad(day)}`
}

/** Date and time, for example 31.12.2026 17:30. */
export function formatDateTime(value: DateInput, fallback = '—', seconds = false): string {
  const date = toDate(value)
  if (!date) return fallback
  return `${formatDate(date)} ${formatTime(date, fallback, seconds)}`
}
