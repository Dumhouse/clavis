const storageKey = 'clavis.lastProject'

/** The project the person opened most recently, used as a default elsewhere. */
export function lastProject(): string | null {
  try {
    return localStorage.getItem(storageKey)
  } catch {
    return null
  }
}

export function rememberProject(slug: string) {
  try {
    localStorage.setItem(storageKey, slug)
  } catch {
    // Storage can be unavailable (private mode); the default just falls back.
  }
}
