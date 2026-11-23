import { lazy, Suspense, useCallback, useEffect, useState, type ReactNode } from 'react'
import { Navigate, Route, Routes, useNavigate } from 'react-router'
import { api, type Me } from './api'
import DataProvider from './components/DataProvider'
import Layout from './components/Layout'
import { Spinner } from './components/ui'
import LoginPage from './pages/Login'

// Pages load on demand so the chart library isn't on the login/health path.
const AnalyticsPage = lazy(() => import('./pages/Analytics'))
const HealthPage = lazy(() => import('./pages/Health'))
const RepoPage = lazy(() => import('./pages/Repo'))
const DigestPage = lazy(() => import('./pages/Digest'))
const SettingsPage = lazy(() => import('./pages/Settings'))
const PRsPage = lazy(() => import('./pages/PRs'))

const page = (el: ReactNode) => <Suspense fallback={<Spinner />}>{el}</Suspense>

export default function App() {
  const [me, setMe] = useState<Me | null | undefined>(undefined)
  const navigate = useNavigate()

  useEffect(() => {
    api<Me>('/api/me').then(setMe).catch(() => setMe(null))
  }, [])

  const onAuthError = useCallback(() => {
    setMe(null)
    navigate('/login', { replace: true })
  }, [navigate])

  const logout = async () => {
    await api('/api/auth/logout', { method: 'POST' }).catch(() => {})
    setMe(null)
    navigate('/login', { replace: true })
  }

  if (me === undefined) return <div className="mx-auto max-w-6xl px-4"><Spinner /></div>

  return (
    <Routes>
      <Route path="/login" element={me ? <Navigate to="/" replace /> : <LoginPage />} />
      <Route
        element={
          me ? (
            <DataProvider onAuthError={onAuthError}>
              <Layout me={me} onLogout={logout} />
            </DataProvider>
          ) : (
            <Navigate to="/login" replace />
          )
        }
      >
        <Route index element={page(<AnalyticsPage />)} />
        <Route path="health" element={page(<HealthPage />)} />
        <Route path="repos/:id" element={page(<RepoPage />)} />
        <Route path="digest" element={page(<DigestPage />)} />
        <Route path="prs" element={page(<PRsPage />)} />
        <Route path="settings" element={page(<SettingsPage />)} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
