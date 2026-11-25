import { lazy, Suspense, useCallback, useEffect, useState, type ReactNode } from 'react'
import { Navigate, Route, Routes, useNavigate } from 'react-router'
import { api, type Me } from './api'
import { AdvisorProvider } from './components/AdvisorContext'
import DataProvider from './components/DataProvider'
import Layout from './components/Layout'
import { Spinner } from './components/ui'
import { enterDemoMode, exitDemoMode } from './demo'
import LoginPage from './pages/Login'

// Pages load on demand so the chart library isn't on the login/health path.
const AnalyticsPage = lazy(() => import('./pages/Analytics'))
const HealthPage = lazy(() => import('./pages/Health'))
const WatchedPage = lazy(() => import('./pages/Watched'))
const RepoPage = lazy(() => import('./pages/Repo'))
const DigestPage = lazy(() => import('./pages/Digest'))
const SettingsPage = lazy(() => import('./pages/Settings'))
const PRsPage = lazy(() => import('./pages/PRs'))
const ReleasesPage = lazy(() => import('./pages/Releases'))

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
    exitDemoMode()
    setMe(null)
    navigate('/login', { replace: true })
  }

  const startDemo = () => {
    enterDemoMode()
    api<Me>('/api/me')
      .then((u) => {
        setMe(u)
        navigate('/', { replace: true })
      })
      .catch(() => setMe(null))
  }

  if (me === undefined) return <div className="mx-auto max-w-6xl px-4"><Spinner /></div>

  return (
    <Routes>
      <Route path="/login" element={me ? <Navigate to="/" replace /> : <LoginPage onDemo={startDemo} />} />
      <Route
        element={
          me ? (
            <DataProvider onAuthError={onAuthError}>
              <AdvisorProvider>
                <Layout me={me} onLogout={logout} />
              </AdvisorProvider>
            </DataProvider>
          ) : (
            <Navigate to="/login" replace />
          )
        }
      >
        <Route index element={page(<AnalyticsPage />)} />
        <Route path="health" element={page(<HealthPage />)} />
        <Route path="watched" element={page(<WatchedPage />)} />
        <Route path="repos/:id" element={page(<RepoPage />)} />
        <Route path="digest" element={page(<DigestPage />)} />
        <Route path="prs" element={page(<PRsPage />)} />
        <Route path="releases" element={page(<ReleasesPage />)} />
        <Route path="settings" element={page(<SettingsPage />)} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
