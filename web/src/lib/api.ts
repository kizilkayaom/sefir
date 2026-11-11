let csrfToken = ''

export function setCSRFToken(token: string) {
  csrfToken = token
}

export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  if (options.body instanceof FormData) {
    headers.delete('Content-Type')
  } else if (options.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }
  if (options.method && options.method !== 'GET') {
    headers.set('X-CSRF-Token', csrfToken)
  }
  const response = await fetch(path, {
    ...options,
    headers,
    credentials: 'same-origin',
  })
  if (response.status === 401 && path !== '/api/login') {
    setCSRFToken('')
    window.dispatchEvent(new Event('sefir-session-expired'))
  }
  const body = await response.json().catch(() => null)
  if (!response.ok) {
    throw new Error(body?.error || 'The request failed. Please try again.')
  }
  if (body === null) throw new Error('The server returned an invalid response.')
  return body as T
}

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'Something went wrong. Please try again.'
}
