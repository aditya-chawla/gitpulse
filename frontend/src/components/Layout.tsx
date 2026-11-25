import { useEffect, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router'
import { api, ApiError, type AdvisorRole, type AppStatus, type Me, type Scan } from '../api'
import { useApi, useData } from '../data'
import { isDemoMode } from '../demo'
import { ago } from '../format'
import { useAdvisor } from '../advisor'
import AdvisorPanel from './AdvisorPanel'

const maintainerNav = [
  { to: '/', label: 'Analytics', end: true },
  { to: '/health', label: 'Repo health' },
  { to: '/digest', label: 'Digest' },
  { to: '/prs', label: 'Pull requests' },
  { to: '/releases', label: 'Releases' },
]

const explorerNav = [
  { to: '/watched', label: 'Watched Radar', end: true },
]

export default function Layout({ me, onLogout }: { me: Me; onLogout: () => void }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { open, setOpen } = useAdvisor()

  const isExplorer = location.pathname.startsWith('/watched')
  const role: AdvisorRole = isExplorer ? 'contributor' : 'maintainer'
  const nav = isExplorer ? explorerNav : maintainerNav

  return (
    <div className="flex min-h-screen flex-col">
      <header className="sticky top-0 z-40 border-b border-line bg-page/90 backdrop-blur">
        <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-x-4 gap-y-2 px-4 py-2.5">
          <div className="flex items-center gap-3">
            <Link to={isExplorer ? '/watched' : '/'} className="flex items-center gap-2 font-semibold text-ink">
              <img src="/favicon.svg" alt="" className="h-6 w-6" />
              gitpulse
            </Link>
            <div className="inline-flex rounded-lg border border-line bg-surface p-0.5 text-[12px]" role="group" aria-label="Workspace mode">
              <button
                type="button"
                onClick={() => navigate('/')}
                className={`rounded-md px-2.5 py-1 font-medium transition-colors ${!isExplorer ? 'bg-surface-2 text-ink' : 'text-muted hover:text-ink'}`}
              >
                My Repos
              </button>
              <button
                type="button"
                onClick={() => navigate('/watched')}
                className={`rounded-md px-2.5 py-1 font-medium transition-colors ${isExplorer ? 'bg-surface-2 text-ink' : 'text-muted hover:text-ink'}`}
              >
                OSS Explorer
              </button>
            </div>
          </div>
          <nav className="order-3 -mx-1 flex w-full gap-1 overflow-x-auto sm:order-none sm:w-auto">
            {nav.map((n) => (
              <NavLink key={n.to} to={n.to} end={n.end}
                className={({ isActive }) => `whitespace-nowrap rounded-md px-2.5 py-1.5 text-[13px] font-medium ${isActive ? 'bg-surface-2 text-ink' : 'text-muted hover:text-ink'}`}>
                {n.label}
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto flex items-center gap-2.5">
            <button
              type="button"
              onClick={() => setOpen(!open)}
              aria-pressed={open}
              className={`rounded-lg border px-2.5 py-1.5 text-[12px] font-medium transition-colors ${
                open ? 'border-accent bg-accent/15 text-accent' : 'border-line bg-surface text-ink hover:bg-surface-2'
              }`}
            >
              ✦ Ask AI
            </button>
            <ScanButton />
            <Link
              to="/settings"
              title="Settings"
              aria-label="Settings"
              className={`rounded-md px-2 py-1 text-[14px] ${location.pathname === '/settings' ? 'bg-surface-2 text-ink' : 'text-muted hover:text-ink'}`}
            >
              ⚙
            </Link>
            <ThemeToggle />
            <UserMenu me={me} onLogout={onLogout} />
          </div>
        </div>
      </header>
      <div className="flex flex-1">
        <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6">
          {isDemoMode() && (
            <div className="mb-5 flex flex-wrap items-center justify-between gap-2 rounded-xl border border-accent/30 bg-accent/10 px-4 py-2.5 text-[13px] text-ink">
              <span>
                <b>Live Demo (Read-Only):</b> Exploring sample personal repos (<i>My Repos</i>) and public OSS repos (<i>OSS Explorer</i>). Mutations are disabled.
              </span>
              <button type="button" onClick={onLogout} className="font-medium text-accent hover:underline">
                Exit demo
              </button>
            </div>
          )}
          <InstallBanner />
          <Outlet />
        </main>
        <AdvisorPanel role={role} />
      </div>
    </div>
  )
}

/** Scans need the GitHub App; say so prominently when it is missing. */
function InstallBanner() {
  const { data } = useApi<AppStatus>('/api/app')
  const missing = data?.missingPermissions ?? []
  if (!data || (data.installed && !data.suspended && missing.length === 0)) return null
  if (data.installed && !data.suspended) {
    return (
      <div role="alert" className="mb-5 rounded-xl border border-warning/40 bg-warning/10 px-4 py-3 text-[14px] text-ink">
        The GitHub App is missing permissions scans need: <span className="font-mono text-[13px]">{missing.join(', ')}</span>.
        Add them under the app&rsquo;s <em>Permissions &amp; events</em> settings, then accept the change on the{' '}
        {data.configureUrl ? <a href={data.configureUrl} className="font-medium text-accent hover:underline">installation page</a> : 'installation page'}.
      </div>
    )
  }
  return (
    <div role="alert" className="mb-5 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border border-warning/40 bg-warning/10 px-4 py-3 text-[14px] text-ink">
      <span>
        {data.suspended
          ? 'The gitpulse GitHub App is suspended, so scans cannot read your repositories.'
          : 'Install the gitpulse GitHub App so scans can read your repositories.'}
      </span>
      {(data.suspended ? data.configureUrl : data.installUrl) && (
        <a href={data.suspended ? data.configureUrl : data.installUrl} className="font-medium text-accent hover:underline">
          {data.suspended ? 'Open app settings' : 'Install on GitHub'}
        </a>
      )}
    </div>
  )
}

/** Triggers a scan and polls its progress; refreshes all data when done. */
function ScanButton() {
  const { invalidate } = useData()
  const [scan, setScan] = useState<Scan | null>(null)
  const [error, setError] = useState<string>()
  const [restart, setRestart] = useState(0) // bump to restart polling
  const sawRunning = useRef(false)

  // Poll while a scan runs (whoever started it: UI or the cron). When a
  // running scan finishes, refresh every page's data.
  useEffect(() => {
    let cancelled = false
    let timer: number | undefined
    async function tick() {
      try {
        const s = await api<Scan | null>('/api/scans/latest')
        if (cancelled) return
        setScan(s)
        if (s?.status === 'running') {
          sawRunning.current = true
          timer = window.setTimeout(tick, 2000)
        } else if (sawRunning.current) {
          sawRunning.current = false
          invalidate()
        }
      } catch {
        if (!cancelled) timer = window.setTimeout(tick, 5000)
      }
    }
    tick()
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [restart, invalidate])

  const start = async () => {
    setError(undefined)
    try {
      await api('/api/scan', { method: 'POST' })
      sawRunning.current = true
      setRestart((n) => n + 1)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not start scan')
    }
  }

  const running = scan?.status === 'running'
  const pct = running && scan.reposTotal > 0 ? Math.round((scan.reposDone / scan.reposTotal) * 100) : 0
  return (
    <div className="flex items-center gap-3">
      <span className="hidden text-right text-[12px] leading-tight text-muted md:block" aria-live="polite">
        {running ? (
          <>
            <span className="text-ink-2">{scan.phase}</span>
            <br />
            {scan.reposTotal > 0 ? `${scan.reposDone} / ${scan.reposTotal} repos` : 'starting…'}
          </>
        ) : scan?.status === 'failed' ? (
          <span title={scan.error}>Last scan failed {ago(scan.startedAt)}</span>
        ) : scan ? (
          <>Scanned {ago(scan.finishedAt ?? scan.startedAt)}</>
        ) : null}
        {error && <span className="text-critical">{error}</span>}
      </span>
      <button
        onClick={start}
        disabled={running}
        className="relative overflow-hidden rounded-lg bg-accent px-3 py-1.5 text-[13px] font-medium text-white disabled:cursor-progress"
      >
        {running && <span className="absolute inset-y-0 left-0 bg-white/25 transition-all" style={{ width: `${pct}%` }} />}
        <span className="relative">{running ? `Scanning ${pct}%` : 'Scan now'}</span>
      </button>
    </div>
  )
}

type Theme = 'system' | 'light' | 'dark'

function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(() => {
    try {
      return (localStorage.getItem('gitpulse-theme') as Theme) || 'system'
    } catch {
      return 'system'
    }
  })
  useEffect(() => {
    const root = document.documentElement
    if (theme === 'system') delete root.dataset.theme
    else root.dataset.theme = theme
    try {
      localStorage.setItem('gitpulse-theme', theme)
    } catch {
      /* storage unavailable */
    }
  }, [theme])
  const next: Record<Theme, Theme> = { system: 'light', light: 'dark', dark: 'system' }
  const icon = { system: '◐', light: '☀', dark: '☾' }[theme]
  return (
    <button onClick={() => setTheme(next[theme])} title={`Theme: ${theme}`} aria-label={`Theme: ${theme}. Click to change.`}
      className="rounded-md px-1.5 py-1 text-[15px] text-muted hover:text-ink">
      {icon}
    </button>
  )
}

function UserMenu({ me, onLogout }: { me: Me; onLogout: () => void }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="relative">
      <button onClick={() => setOpen((o) => !o)} aria-expanded={open} className="flex items-center rounded-full">
        <img src={me.avatarUrl} alt={me.login} className="h-7 w-7 rounded-full border border-line" />
      </button>
      {open && (
        <div className="absolute right-0 mt-2 w-48 rounded-lg border border-line bg-surface p-1 text-[13px] shadow-lg">
          <div className="px-3 py-2 text-muted">Signed in as <b className="text-ink">{me.login}</b></div>
          <button onClick={onLogout} className="w-full rounded-md px-3 py-2 text-left text-ink-2 hover:bg-surface-2">Sign out</button>
        </div>
      )}
    </div>
  )
}
