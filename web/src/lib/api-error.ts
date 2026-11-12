export function apiErrorMessage(
  error: unknown,
  fallback = 'Something went wrong. Try again.',
): string {
  if (!error) return fallback
  if (typeof error === 'string') return error
  if (typeof error === 'object') {
    const value = error as {
      detail?: unknown
      title?: unknown
      message?: unknown
      request_id?: unknown
    }
    let message = fallback
    if (typeof value.detail === 'string' && value.detail) message = value.detail
    else if (typeof value.message === 'string' && value.message) message = value.message
    else if (typeof value.title === 'string' && value.title) message = value.title
    if (typeof value.request_id === 'string' && value.request_id) {
      return `${message} (request ${value.request_id.slice(0, 18)})`
    }
    return message
  }
  return fallback
}
