import { createContext, useContext, useEffect, useState } from 'react'
import { api, AuthError } from './api'

// A "data version" bumps after a scan finishes or settings change, so every
// mounted query refetches. Cached responses stay visible while refetching.
export interface DataCtx {
  version: number
  invalidate: () => void
  onAuthError: () => void
}

export const DataContext = createContext<DataCtx>({ version: 0, invalidate: () => {}, onAuthError: () => {} })
const cache = new Map<string, unknown>()

export const useData = () => useContext(DataContext)

export interface Query<T> {
  data: T | undefined
  error: Error | undefined
  loading: boolean
  reload: () => void
}

export function useApi<T>(path: string | null): Query<T> {
  const { version, onAuthError } = useData()
  const [nonce, setNonce] = useState(0)
  const key = path ? `${path}#${version}#${nonce}` : null
  const [state, setState] = useState<{ key: string | null; data?: T; error?: Error }>({ key: null })

  useEffect(() => {
    if (!path || !key) return
    let cancelled = false
    api<T>(path)
      .then((d) => {
        cache.set(path, d)
        if (!cancelled) setState({ key, data: d })
      })
      .catch((e: Error) => {
        if (cancelled) return
        if (e instanceof AuthError) onAuthError()
        setState({ key, error: e })
      })
    return () => {
      cancelled = true
    }
  }, [path, key, onAuthError])

  const fresh = state.key === key
  const data = fresh && state.data !== undefined ? state.data : path ? (cache.get(path) as T | undefined) : undefined
  return { data, error: fresh ? state.error : undefined, loading: !fresh, reload: () => setNonce((n) => n + 1) }
}
