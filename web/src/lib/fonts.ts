/*
 * Font preference (Settings → Appearance), saved in this browser and on the account. It
 * overrides the --font / --font-mono variables the styles use. "Style
 * default" keeps what Classic (Geist) or Warm (Manrope) sets.
 *
 * Only bundled fonts and the system's own fonts are offered, so nothing is
 * loaded from a third party; a bundled font's files are fetched from this
 * server only once it is chosen. To add one: install its @fontsource
 * package, import it in main.tsx and add an entry below.
 */

export type FontOption = { value: string; label: string; stack: string | null; note?: string }

export const uiFonts: FontOption[] = [
  {
    value: 'default',
    label: 'Style default',
    stack: null,
    note: 'Geist (Classic) or Manrope (Warm)',
  },
  { value: 'geist', label: 'Geist', stack: "'Geist Variable', 'Geist', system-ui, sans-serif" },
  {
    value: 'manrope',
    label: 'Manrope',
    stack: "'Manrope Variable', 'Manrope', system-ui, sans-serif",
  },
  {
    value: 'inter',
    label: 'Inter',
    stack: "'Inter Variable', 'Inter', system-ui, sans-serif",
    note: 'neutral, very readable at small sizes',
  },
  {
    value: 'plex-sans',
    label: 'IBM Plex Sans',
    stack: "'IBM Plex Sans', system-ui, sans-serif",
    note: 'technical, slightly narrow',
  },
  {
    value: 'system',
    label: 'System',
    stack: "system-ui, -apple-system, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif",
    note: 'SF Pro on macOS, Segoe UI on Windows',
  },
]

export const monoFonts: FontOption[] = [
  { value: 'default', label: 'Style default', stack: null },
  {
    value: 'jetbrains',
    label: 'JetBrains Mono',
    stack: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace",
    note: 'tall letters, clear 0/O and 1/l/I',
  },
  {
    value: 'plex-mono',
    label: 'IBM Plex Mono',
    stack: "'IBM Plex Mono', ui-monospace, monospace",
    note: 'pairs with IBM Plex Sans',
  },
  {
    value: 'system',
    label: 'System monospace',
    stack: "ui-monospace, 'SF Mono', SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace",
    note: 'SF Mono on macOS, Consolas on Windows',
  },
  { value: 'menlo', label: 'Menlo / DejaVu', stack: "Menlo, 'DejaVu Sans Mono', monospace" },
  { value: 'courier', label: 'Courier', stack: "'Courier New', Courier, monospace" },
]

const uiKey = 'clavis.font'
const monoKey = 'clavis.fontMono'

function stored(key: string, options: FontOption[]): string {
  try {
    const saved = localStorage.getItem(key)
    if (options.some((option) => option.value === saved)) return saved ?? 'default'
  } catch {
    // Storage can be unavailable.
  }
  return 'default'
}

export function fontPrefs(): { ui: string; mono: string } {
  return { ui: stored(uiKey, uiFonts), mono: stored(monoKey, monoFonts) }
}

/** Applies the saved fonts; call once at startup and after a change. */
export function applyFonts() {
  const { ui, mono } = fontPrefs()
  const root = document.documentElement.style
  const uiStack = uiFonts.find((option) => option.value === ui)?.stack
  const monoStack = monoFonts.find((option) => option.value === mono)?.stack
  if (uiStack) {
    root.setProperty('--font', uiStack)
    root.setProperty('--font-head', uiStack)
  } else {
    root.removeProperty('--font')
    root.removeProperty('--font-head')
  }
  if (monoStack) root.setProperty('--font-mono', monoStack)
  else root.removeProperty('--font-mono')
}

export function setFontPrefs(next: { ui?: string; mono?: string }) {
  try {
    if (next.ui) localStorage.setItem(uiKey, next.ui)
    if (next.mono) localStorage.setItem(monoKey, next.mono)
  } catch {
    // Applies for this visit only.
  }
  applyFonts()
}

/** Applies the fonts saved on the account (empty means not chosen yet). */
export function applyAccountFontPrefs(ui?: string, mono?: string) {
  const next: { ui?: string; mono?: string } = {}
  if (uiFonts.some((option) => option.value === ui)) next.ui = ui as string
  if (monoFonts.some((option) => option.value === mono)) next.mono = mono as string
  if (next.ui !== undefined || next.mono !== undefined) setFontPrefs(next)
}
