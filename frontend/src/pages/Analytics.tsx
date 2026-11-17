import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { isNoData, type Analytics, type NoData, type Overview, type TopRepo } from '../api'
import { foldLanguages, languageColors, OTHER, OTHER_COLOR, SLOTS } from '../colors'
import { BarList, Heatmap, HistoryChart, LanguageTable, LanguageTrendChart, SegmentedBar, WeeklyChart } from '../components/charts'
import { Card, ChartCard, ErrorBox, Legend, NoScanYet, Segmented, Spinner, StatTile, Table } from '../components/ui'
import { useApi } from '../data'
import { hourLabel, num, pct, shortDate, WEEKDAYS } from '../format'

export default function AnalyticsPage() {
  const ov = useApi<Overview | NoData>('/api/overview')
  const an = useApi<Analytics | NoData>('/api/analytics')

  if (ov.error) return <ErrorBox error={ov.error} onRetry={ov.reload} />
  if (an.error) return <ErrorBox error={an.error} onRetry={an.reload} />
  if (!ov.data || !an.data) return <Spinner />
  if (isNoData(ov.data) || isNoData(an.data)) return <NoScanYet />
  return <Dashboard ov={ov.data} an={an.data} />
}

function Dashboard({ ov, an }: { ov: Overview; an: Analytics }) {
  const order = useMemo(() => an.languages.map((l) => l.language), [an.languages])
  const color = useMemo(() => languageColors(order), [order])
  const folded = foldLanguages(an.languages, order)
  const c = an.commits

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold text-ink">Analytics</h1>
        <p className="text-[12px] text-muted">
          {ov.includedRepos} of {ov.totalRepos} repos included
          {ov.totalRepos > ov.includedRepos && <> · forks, archived and excluded repos are left out (<Link to="/settings" className="text-accent hover:underline">settings</Link>)</>}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6">
        <StatTile label="Repositories" value={num(ov.includedRepos)} sub={`${ov.privateRepos} private · ${ov.forkRepos} forks`} />
        <StatTile label="Stars" value={num(ov.stars)} sub={`${num(ov.forks)} forks`} />
        <StatTile label="Average health" value={ov.avgHealth} sub={<Link to="/health" className="hover:text-accent">out of 100</Link>} />
        <StatTile label="Commits, last 52 weeks" value={num(c.total)} sub={`${c.activeDays} active days`} />
        <StatTile label="Longest streak" value={`${c.longestStreak.days} days`}
          sub={c.longestStreak.start ? `${shortDate(c.longestStreak.start)} – ${shortDate(c.longestStreak.end!)}` : '—'} />
        <StatTile label="Current streak" value={`${c.currentStreak} days`} sub={c.currentStreak ? 'keep it going' : 'no commit today or yesterday'} />
      </div>

      <div className="grid gap-5 lg:grid-cols-2">
        <ChartCard
          title="Language mix"
          subtitle={`By bytes of code across ${an.includedRepos} repos`}
          chart={
            <BarList
              format={(v) => pct(v)}
              rows={folded.map((l) => ({
                key: l.language, label: l.language, value: l.percent,
                color: l.language === OTHER ? OTHER_COLOR : color(l.language),
              }))}
            />
          }
          table={<LanguageTable shares={an.languages} />}
        />
        <ChartCard
          title="Language trend by year"
          subtitle="Share of bytes among repos active each year (from creation to last push)"
          chart={<LanguageTrendChart trend={an.trend} languages={an.trendLanguages} color={color} />}
          table={
            <Table head={['Year', 'Repos', ...an.trendLanguages, OTHER]}>
              {an.trend.map((y) => (
                <tr key={y.year}>
                  <td>{y.year}</td><td>{y.repos}</td>
                  {[...an.trendLanguages, OTHER].map((l) => <td key={l}>{y.percents[l] ? pct(y.percents[l]) : '—'}</td>)}
                </tr>
              ))}
            </Table>
          }
        />
      </div>

      <PerRepoLanguages an={an} color={color} order={order} />

      <ChartCard
        title="Weekly commits"
        subtitle="Default branches, last 52 weeks. “All authors” comes from GitHub’s weekly stats, which can lag a few days."
        chart={<WeeklyChart weeks={an.weekly} />}
        table={
          <Table head={['Week of', 'My commits', 'All authors']}>
            {[...an.weekly].reverse().map((w) => <tr key={w.week}><td>{shortDate(w.week)}</td><td>{w.mine}</td><td>{w.all}</td></tr>)}
          </Table>
        }
      />

      <div className="grid gap-5 lg:grid-cols-[3fr_2fr]">
        <ChartCard
          title="When you commit"
          subtitle={c.total > 0 ? `Busiest: ${WEEKDAYS[c.busiestWeekday]}s around ${hourLabel(c.busiestHour)} · times in ${an.timezone}` : `Times in ${an.timezone}`}
          chart={<Heatmap grid={c.heatmap} />}
          table={
            <Table head={['Day', 'Commits', 'Busiest hour']}>
              {c.heatmap.map((row, d) => {
                const total = row.reduce((a, b) => a + b, 0)
                const h = row.indexOf(Math.max(...row))
                return <tr key={d}><td>{WEEKDAYS[d]}</td><td>{total}</td><td>{total ? hourLabel(h) : '—'}</td></tr>
              })}
            </Table>
          }
        />
        <TopRepos an={an} />
      </div>

      <TechStack an={an} />

      <div className="grid gap-5 lg:grid-cols-2">
        <Card title="Stars over time" subtitle="Included repos, one point per scan">
          <HistoryChart points={ov.history} dataKey="stars" label="Stars" />
        </Card>
        <Card title="Average health over time" subtitle="Included repos, one point per scan">
          <HistoryChart points={ov.history} dataKey="avgHealth" label="Avg health" domain={[0, 100]} format={(v) => String(Math.round(v))} />
        </Card>
      </div>
    </div>
  )
}

function PerRepoLanguages({ an, color, order }: { an: Analytics; color: (l: string) => string; order: string[] }) {
  const [all, setAll] = useState(false)
  const rows = all ? an.perRepo : an.perRepo.slice(0, 12)
  const legend = order.slice(0, SLOTS).map((l) => ({ label: l, color: color(l) }))
  if (order.length > SLOTS) legend.push({ label: OTHER, color: OTHER_COLOR })
  return (
    <ChartCard
      title="Languages per repository"
      subtitle="Largest repositories first"
      chart={
        <>
          <ul className="space-y-2.5">
            {rows.map((r) => (
              <li key={r.repoId} className="grid grid-cols-[1fr_auto] items-center gap-x-3 gap-y-1 text-[13px] sm:grid-cols-[minmax(7rem,13rem)_1fr_4.5rem]">
                <Link to={`/repos/${r.repoId}`} className="truncate text-ink-2 hover:text-accent hover:underline" title={r.name}>{r.name}</Link>
                <span className="order-last col-span-2 sm:order-none sm:col-span-1">
                  <SegmentedBar parts={foldLanguages(r.languages, order)} color={color} label={r.name} />
                </span>
                <span className="truncate text-[12px] text-muted">{r.languages[0]?.language}</span>
              </li>
            ))}
          </ul>
          {an.perRepo.length > 12 && (
            <button onClick={() => setAll((a) => !a)} className="mt-3 text-[13px] text-accent hover:underline">
              {all ? 'Show fewer' : `Show all ${an.perRepo.length}`}
            </button>
          )}
          <Legend items={legend} />
        </>
      }
      table={
        <Table head={['Repository', 'Languages']}>
          {an.perRepo.map((r) => (
            <tr key={r.repoId}><td>{r.name}</td><td>{r.languages.map((l) => `${l.language} ${pct(l.percent)}`).join(', ')}</td></tr>
          ))}
        </Table>
      }
    />
  )
}

function TopRepos({ an }: { an: Analytics }) {
  const [by, setBy] = useState<'stars' | 'forks' | 'commits'>('stars')
  const data: Record<typeof by, TopRepo[]> = { stars: an.topByStars, forks: an.topByForks, commits: an.topByCommits }
  const rows = data[by]
  return (
    <Card
      title="Top repositories"
      subtitle={by === 'commits' ? 'By your commits, last 52 weeks' : `By ${by}`}
      actions={<Segmented value={by} onChange={setBy} options={[{ value: 'stars', label: 'Stars' }, { value: 'forks', label: 'Forks' }, { value: 'commits', label: 'Commits' }]} />}
    >
      {rows.length ? (
        <BarList linkTo={(id) => `/repos/${id}`} rows={rows.map((r) => ({ key: r.id, id: r.id, label: r.name, value: r.value }))} />
      ) : (
        <p className="py-6 text-center text-[13px] text-muted">No {by} yet.</p>
      )}
    </Card>
  )
}

const categoryLabel: Record<string, string> = {
  framework: 'Frameworks', library: 'Libraries', database: 'Data', testing: 'Testing', build: 'Build', infra: 'Infrastructure', tooling: 'Tooling',
}

function TechStack({ an }: { an: Analytics }) {
  const groups = new Map<string, typeof an.tech>()
  for (const t of an.tech) groups.set(t.category, [...(groups.get(t.category) ?? []), t])
  const order = ['framework', 'library', 'database', 'testing', 'build', 'infra', 'tooling']
  return (
    <Card title="Detected tech stack" subtitle="From manifests (go.mod, package.json, requirements.txt, …) and config files; number of repos in brackets">
      {an.tech.length === 0 ? (
        <p className="py-6 text-center text-[13px] text-muted">Nothing detected yet.</p>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {order.filter((k) => groups.has(k)).map((k) => (
            <div key={k}>
              <h3 className="mb-2 text-[12px] font-medium text-muted">{categoryLabel[k] ?? k}</h3>
              <ul className="flex flex-wrap gap-1.5">
                {groups.get(k)!.map((t) => (
                  <li key={t.name} title={t.repos.join(', ')}
                    className="rounded-md border border-line bg-surface-2 px-2 py-0.5 text-[12px] text-ink-2">
                    {t.name} <span className="text-muted">({t.repos.length})</span>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}
