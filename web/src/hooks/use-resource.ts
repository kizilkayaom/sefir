import { useCallback, useEffect, useState } from 'react'

import { api, errorMessage } from '@/lib/api'

export function useResource<T>(url: string | null) {
  const [revision, setRevision] = useState(0)
  const [result, setResult] = useState<{
    key: string
    data: T | null
    error: string
  } | null>(null)
  const key = `${url}:${revision}`
  const reload = useCallback(() => setRevision((value) => value + 1), [])

  useEffect(() => {
    if (!url) return
    const controller = new AbortController()
    api<T>(url, { signal: controller.signal }).then(
      (data) => {
        if (!controller.signal.aborted) setResult({ key, data, error: '' })
      },
      (error) => {
        if (!controller.signal.aborted) {
          setResult({ key, data: null, error: errorMessage(error) })
        }
      },
    )
    return () => controller.abort()
  }, [url, key])

  const current = result?.key === key ? result : null
  return {
    data: current?.data ?? null,
    error: current?.error ?? '',
    loading: url !== null && current === null,
    reload,
  }
}
