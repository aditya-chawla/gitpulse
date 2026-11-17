import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { DataContext } from '../data'

export default function DataProvider({ children, onAuthError }: { children: ReactNode; onAuthError: () => void }) {
  const [version, setVersion] = useState(0)
  const invalidate = useCallback(() => setVersion((v) => v + 1), [])
  const value = useMemo(() => ({ version, invalidate, onAuthError }), [version, invalidate, onAuthError])
  return <DataContext.Provider value={value}>{children}</DataContext.Provider>
}
