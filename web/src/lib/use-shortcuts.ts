import { useEffect, useRef } from 'react'

/**
 * Page-level single-key shortcuts (the ones the hint bar advertises).
 * Keys are matched case-insensitively and ignored while typing, with a
 * modifier held, or while a dialog is open.
 */
export function useShortcuts(handlers: Record<string, () => void>, enabled = true) {
  const latest = useRef(handlers)

  useEffect(() => {
    latest.current = handlers
  })

  useEffect(() => {
    if (!enabled) return
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const typing =
        target?.tagName === 'INPUT' ||
        target?.tagName === 'TEXTAREA' ||
        target?.tagName === 'SELECT' ||
        target?.isContentEditable === true
      if (typing || event.metaKey || event.ctrlKey || event.altKey) return
      if (document.querySelector('dialog[open], [role="dialog"]')) return
      const handler = latest.current[event.key.toLowerCase()]
      if (!handler) return
      event.preventDefault()
      handler()
    }
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('keydown', onKey)
    }
  }, [enabled])
}
