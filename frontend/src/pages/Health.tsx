import { Fragment, useMemo, useState } from 'react'
import { isNoData, type NoData, type RepoRow, type ReposResponse, type Status } from '../api'
import { Card, ErrorBox, NoScanYet, Pill, RepoLink, ScoreMeter, Spinner, StatusBadge } from '../components/ui'
import { useApi } from '../data'
import { ago, num } from '../format'

type SortKey = 'name' | 'healthScore' | 'status' | 'pushedAt' | 'stars' | 'openPrs' | 'openIssues' | 'primaryLanguage'
const statusRank: Record<Status, number> = { active: 0, slowing: 1, dormant: 2 }

function compare(a: RepoRow, b: RepoRow, key: SortKey): number {
  switch (key) {
    case 'name':
    case 'primaryLanguage':
      return a[key].localeCompare(b[key], undefined, { sensitivity: 'base' })
    case 'status':
      return statusRank[a.status] - statusRank[b.status]
    case 'pushedAt':
      return (a.pushedAt ? Date.parse(a.pushedAt) : 0) - (b.pushedAt ? Date.parse(b.pushedAt) : 0)
    default:
      return a[key] - b[key]
  }
}

export default function HealthPage() {
  const q = useApi<ReposResponse | NoData>('/api/repos')
  if (q.error) return <ErrorBox error={q.error} onRetry={q.reload} />
  if (!q.data) return <Spinner />
  if (isNoData(q.data)) return <NoScanYet />
  return <HealthTable repos={q.data.repos} />
}

function HealthTable({ repos }: { repos: RepoRow[] }) {
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<Status | 'all'>('all')
  const [language, setLanguage] = useState('all')
  const [showExcluded, setShowExcluded] = useState(false)
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: 'healthScore', dir: -1 })
  const [open, setOpen] = useState<number | null>(null)

  const languages = useMemo(() => [...new Set(repos.map((r) => r.primaryLanguage).filter(Boolean))].sort(), [repos])
  const rows = useMemo(() => {
    const s = search.trim().toLowerCase()
    return repos
      .filter((r) => showExcluded || !r.excluded)
      .filter((r) => status === 'all' || r.status === status)
      .filter((r) => language === 'all' || r.primaryLanguage === language)
      .filter((r) => !s || r.name.toLowerCase().includes(s) || r.description.toLowerCase().includes(s) || r.topics.some((t) => t.includes(s)))
      .sort((a, b) => compare(a, b, sort.key) * sort.dir || a.name.localeCompare(b.name))
  }, [repos, search, status, language, showExcluded, sort])

  const included = repos.filter((r) => !r.excluded)
  const counts = { active: 0, slowing: 0, dormant: 0 }
  included.forEach((r) => counts[r.status]++)
  const avg = included.length ? Math.round(included.reduce((a, r) => a + r.healthScore, 0) / included.length) : 0

  const th = (key: SortKey, label: string, align = 'text-left') => (
    <th className={`whitespace-nowrap border-b border-line px-2 py-2 font-medium ${align}`}
      aria-sort={sort.key === key ? (sort.dir === 1 ? 'ascending' : 'descending') : 'none'}>
      <button className="inline-flex items-center gap-1 hover:text-ink"
        onClick={() => setSort((s) => ({ key, dir: s.key === key ? (s.dir === 1 ? -1 : 1) : key === 'name' || key === 'primaryLanguage' ? 1 : -1 }))}>
        {label}
        <span aria-hidden className="text-[10px]">{sort.key === key ? (sort.dir === 1 ? '▲' : '▼') : ''}</span>
      </button>
    </th>
  )

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold text-ink">Repo health</h1>
        <p className="text-[13px] text-muted">
          Average <b className="text-ink">{avg}</b>/100 · <StatusBadge status="active" /> {counts.active} · <StatusBadge status="slowing" /> {counts.slowing} · <StatusBadge status="dormant" /> {counts.dormant}
        </p>
      </div>

      <Card>
        <div className="mb-4 flex flex-wrap items-center gap-2 text-[13px]">
          <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search name, description, topic"
            className="min-w-52 flex-1 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-ink placeholder:text-muted" aria-label="Search repositories" />
          <select value={status} onChange={(e) => setStatus(e.target.value as Status | 'all')} aria-label="Status"
            className="rounded-lg border border-line bg-surface-2 px-2 py-1.5 text-ink">
            <option value="all">All statuses</option><option value="active">Active</option><option value="slowing">Slowing</option><option value="dormant">Dormant</option>
          </select>
          <select value={language} onChange={(e) => setLanguage(e.target.value)} aria-label="Language"
            className="rounded-lg border border-line bg-surface-2 px-2 py-1.5 text-ink">
            <option value="all">All languages</option>
            {languages.map((l) => <option key={l}>{l}</option>)}
          </select>
          <label className="inline-flex items-center gap-1.5 text-ink-2">
            <input type="checkbox" checked={showExcluded} onChange={(e) => setShowExcluded(e.target.checked)} />
            Show forks, archived & excluded
          </label>
          <span className="ml-auto text-muted">{rows.length} repos</span>
        </div>

        <div className="-mx-4 overflow-x-auto sm:mx-0">
          <table className="w-full min-w-[760px] text-[13px]">
            <thead className="text-[12px] text-muted">
              <tr>
                <th className="w-6 border-b border-line" />
                {th('name', 'Repository')}
                {th('healthScore', 'Score')}
                {th('status', 'Status')}
                <th className="border-b border-line px-2 py-2 text-left font-medium">Has</th>
                {th('primaryLanguage', 'Language')}
                {th('pushedAt', 'Last push')}
                {th('stars', 'Stars', 'text-right')}
                {th('openPrs', 'PRs', 'text-right')}
                {th('openIssues', 'Issues', 'text-right')}
              </tr>
            </thead>
            <tbody className="tabular">
              {rows.map((r) => (
                <Fragment key={r.id}>
                  <tr className="border-b border-line hover:bg-surface-2">
                    <td className="px-1">
                      <button onClick={() => setOpen(open === r.id ? null : r.id)} aria-expanded={open === r.id}
                        aria-label={`Score breakdown for ${r.name}`} className="px-1 text-muted hover:text-ink">
                        {open === r.id ? '▾' : '▸'}
                      </button>
                    </td>
                    <td className="px-2 py-2">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <RepoLink id={r.id} name={r.name} />
                        {r.visibility === 'private' && <Pill>private</Pill>}
                        {r.isFork && <Pill>fork</Pill>}
                        {r.isArchived && <Pill>archived</Pill>}
                        {r.excluded && !r.isFork && !r.isArchived && <Pill>excluded</Pill>}
                      </div>
                      {r.description && <div className="mt-0.5 max-w-[16rem] truncate text-[12px] text-muted" title={r.description}>{r.description}</div>}
                    </td>
                    <td className="px-2"><span className="inline-flex items-center gap-2"><span className="w-7 text-right font-medium text-ink">{r.healthScore}</span><ScoreMeter score={r.healthScore} /></span></td>
                    <td className="px-2"><StatusBadge status={r.status} /></td>
                    <td className="px-2"><Flags r={r} /></td>
                    <td className="px-2 text-ink-2">{r.primaryLanguage || '—'}</td>
                    <td className="whitespace-nowrap px-2 text-ink-2">{ago(r.pushedAt)}</td>
                    <td className="px-2 text-right text-ink-2">{num(r.stars)}</td>
                    <td className="px-2 text-right text-ink-2">{r.openPrs}</td>
                    <td className="px-2 text-right text-ink-2">{r.openIssues}</td>
                  </tr>
                  {open === r.id && (
                    <tr className="border-b border-line bg-surface-2/60">
                      <td />
                      <td colSpan={9} className="px-2 py-3"><Breakdown r={r} /></td>
                    </tr>
                  )}
                </Fragment>
              ))}
            </tbody>
          </table>
          {rows.length === 0 && <p className="py-8 text-center text-[13px] text-muted">No repositories match these filters.</p>}
        </div>
      </Card>
    </div>
  )
}

function Flags({ r }: { r: RepoRow }) {
  const f: [boolean, string][] = [[r.hasReadme, 'README'], [r.hasLicense, 'License'], [r.hasCi, 'CI'], [r.hasTests, 'Tests']]
  return (
    <span className="flex gap-1">
      {f.map(([ok, label]) => (
        <span key={label} title={`${label}: ${ok ? 'yes' : 'no'}`}
          className={`rounded px-1 text-[11px] ${ok ? 'bg-surface-2 text-ink-2' : 'text-muted line-through decoration-1'}`}>
          {label}
        </span>
      ))}
    </span>
  )
}

export function Breakdown({ r }: { r: RepoRow }) {
  return (
    <ul className="grid gap-x-8 gap-y-2 sm:grid-cols-2">
      {r.healthBreakdown.map((c) => (
        <li key={c.key} className="grid grid-cols-[9.5rem_4rem_1fr] items-center gap-2 text-[13px]">
          <span className="text-ink-2">{c.label}</span>
          <ScoreMeter score={c.points} max={c.max} width="w-16" />
          <span className="text-muted"><span className="tabular text-ink">{c.points}/{c.max}</span> · {c.detail}</span>
        </li>
      ))}
    </ul>
  )
}
