const storageKey = 'clavis.openKey'

/** Ask the next secrets page that mounts to open this key's detail panel. */
export function requestOpenKey(key: string) {
  try {
    sessionStorage.setItem(storageKey, key)
  } catch {
    // Without storage the page simply opens without the panel.
  }
  window.dispatchEvent(new CustomEvent('clavis:open-key', { detail: key }))
}

/** Read and clear a pending key request. */
export function takeOpenKey(): string | null {
  try {
    const key = sessionStorage.getItem(storageKey)
    if (key) sessionStorage.removeItem(storageKey)
    return key
  } catch {
    return null
  }
}
