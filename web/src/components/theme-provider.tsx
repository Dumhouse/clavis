import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { applyAccountFontPrefs } from '@/lib/fonts'
import { applyAccountFormatPrefs } from '@/lib/format'

export type Theme = 'dark' | 'light'
export type ThemeMode = 'system' | Theme
export type Appearance = 'classic' | 'warm'

type ThemeContextValue = {
  theme: Theme
  mode: ThemeMode
  appearance: Appearance
  setTheme: (theme: ThemeMode) => void
  setAppearance: (appearance: Appearance) => void
  toggleTheme: () => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

function storedMode(): ThemeMode {
  const saved = localStorage.getItem('clavis.theme')
  if (saved === 'dark' || saved === 'light' || saved === 'system') return saved
  return 'system'
}

/** Applies preferences loaded from the server and notifies ThemeProvider. */
export function applyServerPreferences(preferences: {
  theme?: string
  appearance?: string
  date_format?: string
  time_format?: string
  font?: string
  font_mono?: string
}) {
  // Date, time and font choices follow the account across browsers.
  applyAccountFormatPrefs(preferences.date_format, preferences.time_format)
  applyAccountFontPrefs(preferences.font, preferences.font_mono)
  const theme = preferences.theme
  const appearance = preferences.appearance
  if (theme === 'system' || theme === 'dark' || theme === 'light') {
    localStorage.setItem('clavis.theme', theme)
  }
  if (appearance === 'classic' || appearance === 'warm') {
    localStorage.setItem('clavis.appearance', appearance)
  }
  window.dispatchEvent(new CustomEvent('clavis:preferences', { detail: { theme, appearance } }))
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setMode] = useState<ThemeMode>(storedMode)
  const [systemDark, setSystemDark] = useState(
    () => !window.matchMedia('(prefers-color-scheme: light)').matches,
  )
  const [appearance, setAppearanceState] = useState<Appearance>(() => {
    const saved = localStorage.getItem('clavis.appearance')
    return saved === 'warm' ? 'warm' : 'classic'
  })

  const theme: Theme = mode === 'system' ? (systemDark ? 'dark' : 'light') : mode

  useEffect(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = (event: MediaQueryListEvent) => {
      setSystemDark(event.matches)
    }
    setSystemDark(query.matches)
    query.addEventListener('change', handler)
    return () => {
      query.removeEventListener('change', handler)
    }
  }, [])

  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])

  useEffect(() => {
    localStorage.setItem('clavis.theme', mode)
  }, [mode])

  useEffect(() => {
    document.documentElement.dataset.style = appearance
    localStorage.setItem('clavis.appearance', appearance)
  }, [appearance])

  useEffect(() => {
    const handler = (event: Event) => {
      const detail = (event as CustomEvent<{ theme?: string; appearance?: string }>).detail
      if (detail.theme === 'system' || detail.theme === 'dark' || detail.theme === 'light') {
        setMode(detail.theme)
      }
      if (detail.appearance === 'classic' || detail.appearance === 'warm') {
        setAppearanceState(detail.appearance)
      }
    }
    window.addEventListener('clavis:preferences', handler)
    return () => {
      window.removeEventListener('clavis:preferences', handler)
    }
  }, [])

  const toggleTheme = useCallback(() => {
    setMode(theme === 'dark' ? 'light' : 'dark')
  }, [theme])

  const value = useMemo<ThemeContextValue>(
    () => ({
      theme,
      mode,
      appearance,
      setTheme: setMode,
      setAppearance: setAppearanceState,
      toggleTheme,
    }),
    [theme, mode, appearance, toggleTheme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const value = useContext(ThemeContext)
  if (!value) throw new Error('useTheme must be used inside ThemeProvider')
  return value
}
