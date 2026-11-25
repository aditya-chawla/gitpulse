import { useMemo, useState, type FormEvent } from 'react'
import { Link } from 'react-router'
import {
  api, ApiError, isNoData, type ActionItem, type NoData, type RepoInsights, type RepoRow, type ReposResponse, type Status, type WatchedRepo,
} from '../api'
import { useAdvisor } from '../advisor'
import { Card, Empty, ErrorBox, Pill, ScoreMeter, Spinner, StatTile, StatusBadge, TrendBadge } from '../components/ui'
import { useApi, useData } from '../data'
import { ago, num } from '../format'

type SortKey = 'healthScore' | 'commitCount30d' | 'stars' | 'openPrs' | 'pushedAt' | 'name'

const severityLabel: Record<number, { text: string; cls: string }> = {
  5: { text: 'High risk', cls: 'border-critical/40 bg-critical/10 text-critical' },
  4: { text: 'Warning', cls: 'border-amber-500/40 bg-amber-500/10 text-amber-500' },
  3: { text: 'Watch', cls: 'border-sky-500/40 bg-sky-500/10 text-sky-500' },
  2: { text: 'Info', cls: 'border-line bg-surface-2 text-muted' },
  1: { text: 'Info', cls: 'border-line bg-surface-2 text-muted' },
}

export default function WatchedPage() {
  const { invalidate } = useData()
  const { askWith } = useAdvisor()
  const reposQ = useApi<ReposResponse | NoData>('/api/repos?source=watched')
  const insightsQ = useApi<{ repos: RepoInsights[] }>('/api/insights?source=watched')
  const watchlistQ = useApi<{ repos: WatchedRepo[] }>('/api/watchlist')
  const signalsQ = useApi<{ items: ActionItem[] }>('/api/actions?limit=20&source=watched')

  const [input, setInput] = useState('')
  const [adding, setAdding] = useState(false)
  const [err, setErr] = useState<string>()
  const [removingId, setRemovingId] = useState<number | null>(null)

  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<Status | 'all'>('all')
  const [language, setLanguage] = useState('all')
  const [sort, setSort] = useState<SortKey>('healthScore')

  const add = async (e: FormEvent) => {
    e.preventDefault()
    if (!input.trim()) return
    setAdding(true)
    setErr(undefined)
    try {
      await api('/api/watchlist', { method: 'POST', body: JSON.stringify({ repo: input.trim() }) })
      setInput('')
      invalidate()
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : 'Failed to add repository')
    } finally {
      setAdding(false)
    }
  }

  const remove = async (watchId: number) => {
    setRemovingId(watchId)
    setErr(undefined)
    try {
      await api(`/api/watchlist/${watchId}`, { method: 'DELETE' })
      invalidate()
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : 'Failed to remove repository')
    } finally {
      setRemovingId(null)
    }
  }

  const watchedList = useMemo(() => watchlistQ.data?.repos ?? [], [watchlistQ.data])
  const scannedRepos = useMemo(() => {
    if (!reposQ.data || isNoData(reposQ.data)) return []
    return reposQ.data.repos
  }, [reposQ.data])

  const watchIdByFullName = useMemo(() => {
    const m = new Map<string, WatchedRepo>()
    watchedList.forEach((w) => m.set(w.fullName.toLowerCase(), w))
    return m
  }, [watchedList])

  const insightsByRepoId = useMemo(() => {
    const m = new Map<number, RepoInsights>()
    ;(insightsQ.data?.repos ?? []).forEach((ins) => m.set(ins.repoId, ins))
    return m
  }, [insightsQ.data])

  const unscanned = useMemo(() => {
    const scannedNames = new Set(scannedRepos.map((r) => r.fullName.toLowerCase()))
    return watchedList.filter((w) => !scannedNames.has(w.fullName.toLowerCase()))
  }, [watchedList, scannedRepos])

  const languages = useMemo(
    () => [...new Set(scannedRepos.map((r) => r.primaryLanguage).filter(Boolean))].sort(),
    [scannedRepos],
  )

  const filtered = useMemo(() => {
    const s = search.trim().toLowerCase()
    return scannedRepos
      .filter((r) => status === 'all' || r.status === status)
      .filter((r) => language === 'all' || r.primaryLanguage === language)
      .filter((r) => !s || r.fullName.toLowerCase().includes(s) || r.description.toLowerCase().includes(s) || r.topics.some((t) => t.includes(s)))
      .sort((a, b) => {
        if (sort === 'name') return a.fullName.localeCompare(b.fullName)
        if (sort === 'pushedAt') return (b.pushedAt ? Date.parse(b.pushedAt) : 0) - (a.pushedAt ? Date.parse(a.pushedAt) : 0)
        return (b[sort] ?? 0) - (a[sort] ?? 0)
      })
  }, [scannedRepos, search, status, language, sort])

  if (reposQ.error) return <ErrorBox error={reposQ.error} onRetry={reposQ.reload} />
  if (watchlistQ.error) return <ErrorBox error={watchlistQ.error} onRetry={watchlistQ.reload} />
  if (!reposQ.data || !watchlistQ.data) return <Spinner />

  const activeCount = scannedRepos.filter((r) => r.status === 'active').length
  const slippingCount = scannedRepos.filter((r) => r.direction === 'slipping').length
  const avgHealth = scannedRepos.length
    ? Math.round(scannedRepos.reduce((sum, r) => sum + r.healthScore, 0) / scannedRepos.length)
    : 0
  const totalCommits30d = scannedRepos.reduce((sum, r) => sum + (r.commitCount30d ?? 0), 0)
  const signals = signalsQ.data?.items ?? []

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <div>
          <h1 className="text-xl font-semibold text-ink">Watched repositories</h1>
          <p className="text-[13px] text-muted">
            Evaluate public open-source repositories for activity, contributor spread, and maintenance health before or while you contribute.
          </p>
        </div>
        <span className="text-[12px] text-muted">{watchedList.length} tracked</span>
      </div>

      <Card title="Track a public repository" subtitle="Enter owner/repo or a GitHub URL. Read-only snapshots update on every scan.">
        <form onSubmit={add} className="flex flex-wrap gap-2">
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="owner/repo or https://github.com/owner/repo"
            className="min-w-64 flex-1 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-[13px] text-ink placeholder:text-muted"
            aria-label="Repository to watch"
          />
          <button
            type="submit"
            disabled={adding || !input.trim()}
            className="rounded-lg bg-accent px-3.5 py-1.5 text-[13px] font-medium text-white hover:opacity-90 disabled:opacity-50"
          >
            {adding ? 'Adding…' : 'Watch repo'}
          </button>
        </form>
        {err && <p role="alert" className="mt-2 text-[13px] text-critical">{err}</p>}
        {unscanned.length > 0 && (
          <div className="mt-3 rounded-lg border border-line bg-surface-2/60 p-3 text-[13px]">
            <p className="font-medium text-ink">Pending next scan</p>
            <ul className="mt-1.5 divide-y divide-line">
              {unscanned.map((w) => (
                <li key={w.id} className="flex flex-wrap items-center justify-between gap-2 py-1.5 first:pt-0 last:pb-0">
                  <div>
                    <a href={`https://github.com/${w.fullName}`} target="_blank" rel="noreferrer" className="font-medium text-ink hover:text-accent hover:underline">
                      {w.fullName}
                    </a>
                    {w.lastError && <p className="text-[12px] text-critical">{w.lastError}</p>}
                  </div>
                  <button
                    type="button"
                    disabled={removingId === w.id}
                    onClick={() => remove(w.id)}
                    className="rounded-lg border border-line px-2 py-0.5 text-[12px] text-muted hover:text-critical disabled:opacity-50"
                  >
                    Remove
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </Card>

      {scannedRepos.length > 0 && (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatTile label="Watched repos" value={num(scannedRepos.length)} sub={`${activeCount} active`} />
          <StatTile label="Average health" value={avgHealth} sub="out of 100" />
          <StatTile label="Commits (30d)" value={num(totalCommits30d)} sub="across watched repos" />
          <StatTile
            label="Slipping"
            value={slippingCount}
            sub={slippingCount === 0 ? 'none slipping' : 'showing decay signals'}
          />
        </div>
      )}

      {scannedRepos.length === 0 ? (
        <Card>
          <Empty>
            {watchedList.length === 0
              ? 'No watched repositories yet. Add a public repository above and click “Scan now” to inspect its health and contributor activity.'
              : 'Watched repositories are queued. Click “Scan now” in the top right to fetch their latest snapshots.'}
          </Empty>
        </Card>
      ) : (
        <Card title="Open-source health & contributor readiness">
          <div className="mb-4 flex flex-wrap items-center gap-2 text-[13px]">
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search watched repos…"
              className="min-w-48 flex-1 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-ink placeholder:text-muted"
              aria-label="Search watched repositories"
            />
            <select value={status} onChange={(e) => setStatus(e.target.value as Status | 'all')} aria-label="Status"
              className="rounded-lg border border-line bg-surface-2 px-2 py-1.5 text-ink">
              <option value="all">All statuses</option>
              <option value="active">Active</option>
              <option value="slowing">Slowing</option>
              <option value="dormant">Dormant</option>
            </select>
            <select value={language} onChange={(e) => setLanguage(e.target.value)} aria-label="Language"
              className="rounded-lg border border-line bg-surface-2 px-2 py-1.5 text-ink">
              <option value="all">All languages</option>
              {languages.map((l) => <option key={l}>{l}</option>)}
            </select>
            <select value={sort} onChange={(e) => setSort(e.target.value as SortKey)} aria-label="Sort by"
              className="rounded-lg border border-line bg-surface-2 px-2 py-1.5 text-ink">
              <option value="healthScore">Sort: Health score</option>
              <option value="commitCount30d">Sort: 30d commits</option>
              <option value="stars">Sort: Stars</option>
              <option value="openPrs">Sort: Open PRs</option>
              <option value="pushedAt">Sort: Last push</option>
              <option value="name">Sort: Name</option>
            </select>
          </div>

          <div className="grid gap-3 md:grid-cols-2">
            {filtered.map((r) => (
              <WatchedRepoCard
                key={r.id}
                repo={r}
                insight={insightsByRepoId.get(r.id)}
                watchEntry={watchIdByFullName.get(r.fullName.toLowerCase())}
                removing={removingId === watchIdByFullName.get(r.fullName.toLowerCase())?.id}
                onRemove={remove}
                onExplain={() =>
                  askWith({
                    role: 'contributor',
                    repoId: r.id,
                    repoName: r.fullName,
                    question: `Explain the contributor readiness, diversity, and PR responsiveness for ${r.fullName}. Should I contribute?`,
                  })
                }
              />
            ))}
          </div>
          {filtered.length === 0 && <Empty>No watched repositories match the current filters.</Empty>}
        </Card>
      )}

      {signals.length > 0 && (
        <Card
          title="Project signals"
          subtitle="Read-only activity, CI, and PR backlog observations across watched repositories"
        >
          <ul className="divide-y divide-line">
            {signals.map((it) => {
              const sev = severityLabel[it.severity] ?? severityLabel[2]
              return (
                <li key={it.id} className="flex flex-wrap items-center justify-between gap-3 py-2.5 first:pt-0 last:pb-0">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className={`rounded border px-1.5 py-0.5 text-[11px] font-medium ${sev.cls}`}>
                        {sev.text}
                      </span>
                      <Link to={`/repos/${it.repoId}`} className="text-[12px] font-medium text-muted hover:text-accent hover:underline">
                        {it.repoName}
                      </Link>
                    </div>
                    <p className="mt-1 text-[13px] text-ink">{it.title}</p>
                  </div>
                  {it.url && (
                    <a
                      href={it.url}
                      target="_blank"
                      rel="noreferrer"
                      className="shrink-0 rounded-lg border border-line bg-surface-2 px-2.5 py-1 text-[12px] text-ink-2 hover:text-accent"
                    >
                      Inspect on GitHub ↗
                    </a>
                  )}
                </li>
              )
            })}
          </ul>
        </Card>
      )}
    </div>
  )
}

function WatchedRepoCard({
  repo: r,
  insight,
  watchEntry,
  removing,
  onRemove,
  onExplain,
}: {
  repo: RepoRow
  insight?: RepoInsights
  watchEntry?: WatchedRepo
  removing: boolean
  onRemove: (id: number) => void
  onExplain: () => void
}) {
  const c7 = r.commitCount7d ?? 0
  const c30 = r.commitCount30d ?? 0
  const authors = r.uniqueAuthors30d ?? 0
  const topPct = Math.round(r.topAuthorPct ?? 0)
  const busFactorWarning = authors > 0 && topPct >= 80

  return (
    <div className="flex flex-col justify-between rounded-xl border border-line bg-surface-2/40 p-4">
      <div>
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <Link to={`/repos/${r.id}`} className="font-semibold text-ink hover:text-accent hover:underline">
                {r.fullName}
              </Link>
              <a
                href={`https://github.com/${r.fullName}`}
                target="_blank"
                rel="noreferrer"
                className="text-[12px] text-muted hover:text-accent"
                title="Open on GitHub"
              >
                ↗
              </a>
            </div>
            {r.description && <p className="mt-1 line-clamp-2 text-[12px] text-muted">{r.description}</p>}
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <StatusBadge status={r.status} />
            <TrendBadge direction={r.direction} />
          </div>
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
          <ScoreMeter score={r.healthScore} />
          {insight && (
            <span className="rounded-md border border-line bg-surface px-2 py-0.5 text-[11px] font-medium text-ink">
              Readiness: {insight.contributorReadiness}/100 ({insight.readinessVerdict})
            </span>
          )}
          <span className="text-[12px] text-muted tabular">★ {num(r.stars)} · ⑂ {num(r.forks)}</span>
          {r.primaryLanguage && <Pill>{r.primaryLanguage}</Pill>}
          {r.license && <Pill>{r.license}</Pill>}
        </div>

        <div className="mt-3 grid grid-cols-2 gap-2 rounded-lg border border-line bg-surface p-2.5 text-[12px] tabular">
          <div>
            <span className="text-muted">Commit cadence</span>
            <div className="font-medium text-ink">
              {c7} / 7d · {c30} / 30d
            </div>
          </div>
          <div>
            <span className="text-muted">Contributors (30d)</span>
            <div className={`font-medium ${busFactorWarning ? 'text-amber-500' : 'text-ink'}`}>
              {authors} {authors === 1 ? 'author' : 'authors'}
              {insight ? ` · div ${insight.diversityScore.toFixed(2)}` : authors > 0 ? ` · top ${topPct}%` : ''}
            </div>
          </div>
          <div>
            <span className="text-muted">PR responsiveness</span>
            <div className="font-medium text-ink">
              {r.openPrs} PRs
              {insight && r.openPrs > 0 ? ` · ${insight.responsiveness} (${insight.prMedianAgeDays.toFixed(0)}d med)` : ''}
            </div>
          </div>
          <div>
            <span className="text-muted">CI &amp; 30d forecast</span>
            <div className="font-medium text-ink">
              {r.hasCi ? (r.failedRuns ? `${r.failedRuns} failed CI` : 'CI passing') : 'No CI'}
              {insight && insight.sampleCount >= 2 ? ` · proj ${insight.projectedHealth30d}/100` : ''}
            </div>
          </div>
        </div>
        {insight && insight.anomalies.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1.5">
            {insight.anomalies.map((a) => (
              <span key={a.metric} className="rounded border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 text-[11px] text-amber-500">
                {a.summary}
              </span>
            ))}
          </div>
        )}
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 border-t border-line pt-2.5 text-[12px] text-muted">
        <span>Last push {ago(r.pushedAt)}</span>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={onExplain}
            className="font-medium text-accent hover:underline"
          >
            Explain ✦
          </button>
          <Link to={`/repos/${r.id}`} className="font-medium text-ink-2 hover:text-accent hover:underline">
            Full details
          </Link>
          {watchEntry && (
            <button
              type="button"
              disabled={removing}
              onClick={() => onRemove(watchEntry.id)}
              className="text-muted hover:text-critical disabled:opacity-50"
            >
              Remove
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
