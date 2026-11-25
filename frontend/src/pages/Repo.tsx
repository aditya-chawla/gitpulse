import { useEffect, useMemo } from 'react'
import { Link, useParams } from 'react-router'
import { ApiError, isNoData, type Analytics, type DeltaSet, type NoData, type RepoDetail, type RepoInsights } from '../api'
import { foldLanguages, languageColors, OTHER, OTHER_COLOR, scoreColor } from '../colors'
import { useAdvisor } from '../advisor'
import { HistoryChart, LanguageTable, SegmentedBar, WeeklyChart } from '../components/charts'
import { Card, ChartCard, Empty, ErrorBox, Legend, NoScanYet, Pill, Spinner, StatusBadge, Table, TrendBadge } from '../components/ui'
import { useApi } from '../data'
import { ago, bytes, date, num, shortDate } from '../format'
import { Breakdown } from './Health'

export default function RepoPage() {
  const { id } = useParams()
  const q = useApi<RepoDetail | NoData>(`/api/repos/${id}`)
  const insQ = useApi<RepoInsights>(`/api/insights/${id}`)
  const an = useApi<Analytics | NoData>('/api/analytics')
  if (q.error) {
    if (q.error instanceof ApiError && q.error.status === 404) {
      return <Card><Empty>That repository isn’t in the latest scan. <Link to="/health" className="text-accent hover:underline">Back to repo health</Link></Empty></Card>
    }
    return <ErrorBox error={q.error} onRetry={q.reload} />
  }
  if (!q.data) return <Spinner />
  if (isNoData(q.data)) return <NoScanYet />
  const order = an.data && !isNoData(an.data) ? an.data.languages.map((l) => l.language) : q.data.languages.map((l) => l.language)
  return <Detail d={q.data} insights={insQ.data} order={order} />
}

function Detail({ d, insights, order }: { d: RepoDetail; insights?: RepoInsights; order: string[] }) {
  const r = d.repo
  const { setRepoContext, askWith } = useAdvisor()
  useEffect(() => {
    setRepoContext(r.id, r.fullName)
    return () => setRepoContext(undefined, undefined)
  }, [r.id, r.fullName, setRepoContext])

  const color = useMemo(() => languageColors(order), [order])
  const parts = foldLanguages(d.languages, order)
  const prs = d.items.filter((i) => i.kind === 'pr')
  const issues = d.items.filter((i) => i.kind === 'issue')
  const stale = d.branches.filter((b) => b.stale)

  return (
    <div className="space-y-5">
      <div>
        <Link to={r.source === 'watched' ? '/watched' : '/health'} className="text-[13px] text-muted hover:text-accent">
          {r.source === 'watched' ? '← Watched Radar' : '← Repo health'}
        </Link>
        <div className="mt-2 flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="flex flex-wrap items-center gap-2 text-xl font-semibold text-ink">
              {r.name}
              {r.source === 'watched' && <Pill>watched · read-only</Pill>}
              {r.visibility !== 'public' && <Pill>{r.visibility}</Pill>}
              {r.isFork && <Pill>fork</Pill>}
              {r.isArchived && <Pill>archived</Pill>}
            </h1>
            {r.description && <p className="mt-1 max-w-2xl text-[14px] text-ink-2">{r.description}</p>}
            <p className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-muted">
              <a href={`https://github.com/${r.fullName}`} target="_blank" rel="noreferrer" className="text-accent hover:underline">{r.fullName} ↗</a>
              {r.source !== 'watched' && <Link to={`/releases?repo=${r.id}`} className="text-accent hover:underline">Release notes</Link>}
              <span>★ {num(r.stars)}</span><span>⑂ {num(r.forks)}</span>
              <span>Created {date(r.createdAt)}</span><span>Pushed {ago(r.pushedAt)}</span>
              <span>{bytes(r.sizeKb * 1024)}</span>{r.license && <span>{r.license}</span>}
              {r.excluded && <span>Excluded from aggregates: {r.excluded}</span>}
            </p>
            {r.topics.length > 0 && (
              <div className="mt-2 flex flex-wrap gap-1">{r.topics.map((t) => <Pill key={t}>{t}</Pill>)}</div>
            )}
          </div>
          <div className="text-right">
            <div className="text-5xl font-semibold" style={{ color: 'var(--text-primary)' }}>
              {r.healthScore}<span className="text-xl text-muted">/100</span>
            </div>
            <div className="mt-1 flex items-center justify-end gap-2">
              <span className="inline-block h-1.5 w-24 overflow-hidden rounded-full" style={{ background: 'var(--meter-track)' }}>
                <span className="block h-full rounded-full" style={{ width: `${r.healthScore}%`, background: scoreColor(r.healthScore) }} />
              </span>
              <StatusBadge status={r.status} />
            </div>
          </div>
        </div>
      </div>

      <Card title="Why this score" subtitle="Each check and the points it earned">
        <Breakdown r={r} />
      </Card>

      {insights && (
        <Card
          title="Data Science signals"
          subtitle="Computed in Postgres from historical snapshots and open PR distributions"
          actions={
            <button
              type="button"
              onClick={() =>
                askWith({
                  role: r.source === 'watched' ? 'contributor' : 'maintainer',
                  repoId: r.id,
                  repoName: r.fullName,
                  question:
                    r.source === 'watched'
                      ? `Explain the Data Science metrics for ${r.fullName} and whether it is a good repo to contribute to.`
                      : `Explain the Data Science metrics for ${r.fullName} and what I should prioritize fixing.`,
                })
              }
              className="rounded-lg border border-line bg-surface-2 px-2.5 py-1 text-[12px] font-medium text-accent hover:border-accent/50"
            >
              Explain ✦
            </button>
          }
        >
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4 text-[13px] tabular">
            <div className="rounded-lg border border-line bg-surface-2/50 p-3">
              <div className="text-[12px] text-muted">30d health projection</div>
              <div className="mt-1 text-lg font-semibold text-ink">{insights.projectedHealth30d}/100</div>
              <div className="text-[12px] text-muted">
                {insights.sampleCount >= 2
                  ? `${insights.healthSlopePerDay >= 0 ? '+' : ''}${insights.healthSlopePerDay.toFixed(2)} pts/day (${insights.sampleCount} scans)`
                  : 'Needs ≥2 scans for slope'}
              </div>
            </div>
            <div className="rounded-lg border border-line bg-surface-2/50 p-3">
              <div className="text-[12px] text-muted">Contributor diversity</div>
              <div className="mt-1 text-lg font-semibold text-ink">
                {insights.diversityScore.toFixed(2)} <span className="text-[12px] font-normal text-muted">· {insights.diversityLabel}</span>
              </div>
              <div className="text-[12px] text-muted">
                {insights.uniqueAuthors30d} authors (30d){insights.topAuthorPct > 0 ? ` · top ${insights.topAuthorPct}%` : ''}
              </div>
            </div>
            <div className="rounded-lg border border-line bg-surface-2/50 p-3">
              <div className="text-[12px] text-muted">PR responsiveness</div>
              <div className="mt-1 text-lg font-semibold text-ink capitalize">{insights.responsiveness}</div>
              <div className="text-[12px] text-muted">
                {insights.openPrs > 0
                  ? `Median ${insights.prMedianAgeDays.toFixed(1)}d · P90 ${insights.prP90AgeDays.toFixed(1)}d`
                  : 'No open PR backlog'}
              </div>
            </div>
            <div className="rounded-lg border border-line bg-surface-2/50 p-3">
              <div className="text-[12px] text-muted">Contributor readiness</div>
              <div className="mt-1 text-lg font-semibold text-ink">{insights.contributorReadiness}/100</div>
              <div className="text-[12px] text-muted">{insights.readinessVerdict}</div>
            </div>
          </div>
          {insights.anomalies.length > 0 && (
            <div className="mt-3 flex flex-wrap gap-2">
              {insights.anomalies.map((a) => (
                <span key={a.metric} className="rounded-md border border-amber-500/40 bg-amber-500/10 px-2.5 py-1 text-[12px] text-amber-500">
                  {a.summary}
                </span>
              ))}
            </div>
          )}
        </Card>
      )}

      {d.trend && (
        <Card
          title={<span className="inline-flex items-center gap-2">Trend & decay signals <TrendBadge direction={d.trend.direction} showLabel /></span>}
          subtitle="7-day and 30-day deltas vs historical snapshots"
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <DeltaBox label="7-day delta" d={d.trend.delta7d} />
            <DeltaBox label="30-day delta" d={d.trend.delta30d} />
          </div>
          {d.trend.alerts.length > 0 && (
            <ul className="mt-4 divide-y divide-line text-[13px]">
              {d.trend.alerts.map((a) => (
                <li key={a.id} className="flex items-center justify-between gap-2 py-1.5">
                  <span className="text-ink">{a.title}</span>
                  {a.url && <a href={a.url} target="_blank" rel="noreferrer" className="shrink-0 text-accent hover:underline">Open ↗</a>}
                </li>
              ))}
            </ul>
          )}
        </Card>
      )}

      <div className="grid gap-5 lg:grid-cols-2">
        <ChartCard
          title="Languages"
          subtitle="By bytes"
          chart={
            parts.length ? (
              <>
                <SegmentedBar tall parts={parts} color={color} label="Languages" />
                <Legend items={parts.map((p) => ({ label: p.language, color: p.language === OTHER ? OTHER_COLOR : color(p.language) }))} />
              </>
            ) : <Empty>No language data.</Empty>
          }
          table={<LanguageTable shares={d.languages} />}
        />
        <Card title="Detected tech" subtitle="From manifests and config files">
          {d.tech.length ? (
            <ul className="flex flex-wrap gap-1.5">
              {d.tech.map((t) => (
                <li key={t.name} title={`${t.category} · ${t.repos[0]}`} className="rounded-md border border-line bg-surface-2 px-2 py-0.5 text-[12px] text-ink-2">
                  {t.name} <span className="text-muted">· {t.category}</span>
                </li>
              ))}
            </ul>
          ) : <Empty>Nothing detected.</Empty>}
        </Card>
      </div>

      <ChartCard
        title="Weekly commits"
        subtitle="Default branch, last 52 weeks. “All authors” comes from GitHub’s weekly stats, which can lag a few days."
        chart={<WeeklyChart weeks={d.weekly} />}
        table={<Table head={['Week of', 'Mine', 'All authors']}>{[...d.weekly].reverse().map((w) => <tr key={w.week}><td>{shortDate(w.week)}</td><td>{w.mine}</td><td>{w.all}</td></tr>)}</Table>}
      />

      <div className="grid gap-5 lg:grid-cols-2">
        <Card title={`Open pull requests (${r.openPrs})`} subtitle={r.openPrs > prs.length ? `Showing the oldest ${prs.length}` : undefined}>
          <ItemList items={prs} />
        </Card>
        <Card title={`Open issues (${r.openIssues})`} subtitle={r.openIssues > issues.length ? `Showing the newest ${issues.length}` : undefined}>
          <ItemList items={issues} />
        </Card>
      </div>

      <div className="grid gap-5 lg:grid-cols-2">
        <Card title={`Branches (${d.branches.length})`} subtitle={stale.length ? `${stale.length} stale (no commits in 90+ days)` : 'None stale'}>
          <Table head={['Branch', 'Last commit', '']}>
            {d.branches.map((b) => (
              <tr key={b.name}>
                <td className="font-mono text-[12px]">{b.name}</td>
                <td>{ago(b.lastCommitAt)}</td>
                <td className="text-right">{b.isDefault ? <Pill>default</Pill> : b.stale ? <Pill>stale</Pill> : null}</td>
              </tr>
            ))}
          </Table>
        </Card>
        <Card title="Failed workflow runs" subtitle="Last 14 days">
          {d.failedRuns.length ? (
            <ul className="space-y-1.5 text-[13px]">
              {d.failedRuns.map((f) => (
                <li key={f.url} className="flex justify-between gap-3">
                  <a href={f.url} target="_blank" rel="noreferrer" className="truncate text-ink hover:text-accent hover:underline">{f.workflow}</a>
                  <span className="shrink-0 text-muted">{f.branch} · {ago(f.createdAt)}</span>
                </li>
              ))}
            </ul>
          ) : <Empty>{r.hasCi ? 'No failures.' : 'No CI workflows in this repo.'}</Empty>}
        </Card>
      </div>

      <div className="grid gap-5 lg:grid-cols-2">
        <Card title="Health over time"><HistoryChart points={d.history} dataKey="health" label="Health" domain={[0, 100]} /></Card>
        <Card title="Stars over time"><HistoryChart points={d.history} dataKey="stars" label="Stars" /></Card>
      </div>
    </div>
  )
}

function ItemList({ items }: { items: RepoDetail['items'] }) {
  if (!items.length) return <Empty>None open.</Empty>
  return (
    <ul className="divide-y divide-line text-[13px]">
      {items.map((i) => (
        <li key={i.number} className="flex items-baseline justify-between gap-3 py-1.5">
          <a href={i.url} target="_blank" rel="noreferrer" className="min-w-0 truncate text-ink hover:text-accent hover:underline">
            <span className="text-muted">#{i.number}</span> {i.title}
          </a>
          <span className="shrink-0 text-[12px] text-muted">
            {i.isDraft && 'draft · '}{i.author || 'ghost'} · {i.ageDays}d
            {i.ageDays > 30 && i.kind === 'pr' && <b className="ml-1 font-medium text-ink-2">· old</b>}
          </span>
        </li>
      ))}
    </ul>
  )
}

const signedDelta = (n: number) => (n > 0 ? `+${n}` : String(n))

function DeltaBox({ label, d }: { label: string; d?: DeltaSet }) {
  if (!d) {
    return (
      <div className="rounded-lg border border-line bg-surface-2 p-3 text-[13px]">
        <div className="font-medium text-ink">{label}</div>
        <p className="mt-1 text-muted">Not enough scan history yet.</p>
      </div>
    )
  }
  const rows: [string, number][] = [
    ['Health score', d.healthScore],
    ['Open PRs', d.openPrs],
    ['Stale branches', d.staleBranches],
    ['Failed runs', d.failedRuns],
    ['30d commits', d.commitCadence],
    ['Unique authors', d.uniqueAuthors],
  ]
  return (
    <div className="rounded-lg border border-line bg-surface-2 p-3 text-[13px]">
      <div className="font-medium text-ink">{label}</div>
      <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 tabular">
        {rows.map(([k, v]) => (
          <div key={k} className="flex justify-between">
            <dt className="text-muted">{k}</dt>
            <dd className="font-medium text-ink-2">{signedDelta(v)}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}
