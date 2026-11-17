import type { ReactNode } from 'react'
import { isNoData, type Digest, type NoData } from '../api'
import { Card, Empty, ErrorBox, NoScanYet, Spinner } from '../components/ui'
import { useApi } from '../data'
import { ago, dateTime } from '../format'

export default function DigestPage() {
  const q = useApi<Digest | NoData>('/api/digest')
  if (q.error) return <ErrorBox error={q.error} onRetry={q.reload} />
  if (!q.data) return <Spinner />
  if (isNoData(q.data)) return <NoScanYet />
  const d = q.data

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold text-ink">Digest</h1>
        <p className="text-[13px] text-muted">
          {d.sincePrevScan ? 'Changes since the previous scan' : 'Last 7 days (only one scan so far)'} · since {dateTime(d.since)}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-5">
        <Count label="Waiting on you" n={d.waitingOnMe.length} />
        <Count label="New issues" n={d.newIssues.length} />
        <Count label="Failed runs" n={d.failedRuns.length} />
        <Count label="New stars" n={d.newStars.length} />
        <Count label="New forks" n={d.newForks.length} />
      </div>

      <Section title="Pull requests waiting on you" hint="Open PRs on your repos by others, or where your review was requested">
        {d.waitingOnMe.length ? (
          <ul className="divide-y divide-line text-[13px]">
            {d.waitingOnMe.map((p) => (
              <Row key={p.url} href={p.url}
                title={<><span className="text-muted">{p.repo} #{p.number}</span> {p.title}</>}
                meta={<>{p.reviewRequested && <b className="mr-1 font-medium text-ink-2">review requested ·</b>}{p.author} · open {p.ageDays}d</>} />
            ))}
          </ul>
        ) : <Empty>Nothing is waiting on you.</Empty>}
      </Section>

      <div className="grid gap-5 lg:grid-cols-2">
        <Section title="New issues">
          {d.newIssues.length ? (
            <ul className="divide-y divide-line text-[13px]">
              {d.newIssues.map((i) => (
                <Row key={i.url} href={i.url} title={<><span className="text-muted">{i.repo} #{i.number}</span> {i.title}</>} meta={<>{i.author} · {ago(i.createdAt)}</>} />
              ))}
            </ul>
          ) : <Empty>No new issues.</Empty>}
        </Section>
        <Section title="Failed workflow runs">
          {d.failedRuns.length ? (
            <ul className="divide-y divide-line text-[13px]">
              {d.failedRuns.map((f) => (
                <Row key={f.url} href={f.url} title={<><span className="text-muted">{f.repo}</span> {f.workflow}</>} meta={<>{f.branch} · {ago(f.createdAt)}</>} />
              ))}
            </ul>
          ) : <Empty>No failed runs.</Empty>}
        </Section>
      </div>

      <div className="grid gap-5 lg:grid-cols-2">
        <Section title="New stars and forks">
          {d.newStars.length + d.newForks.length ? (
            <ul className="divide-y divide-line text-[13px]">
              {[...d.newStars.map((s) => ({ ...s, kind: '★ starred' })), ...d.newForks.map((s) => ({ ...s, kind: '⑂ forked' }))]
                .sort((a, b) => b.at.localeCompare(a.at))
                .map((e) => (
                  <Row key={e.kind + e.repo + e.actor} href={`https://github.com/${e.actor}`}
                    title={<><b className="font-medium">{e.actor}</b> <span className="text-muted">{e.kind}</span> {e.repo.split('/')[1]}</>}
                    meta={ago(e.at)} />
                ))}
            </ul>
          ) : <Empty>No new stars or forks.</Empty>}
        </Section>
        <Section title="Star and fork changes" hint="Net change per repo since the previous scan">
          {!d.sincePrevScan ? <Empty>Available after the next scan.</Empty> : d.deltas.length ? (
            <ul className="divide-y divide-line text-[13px] tabular">
              {d.deltas.map((c) => (
                <li key={c.repo} className="flex justify-between py-1.5">
                  <span className="text-ink">{c.repo}</span>
                  <span className="text-ink-2">{signed(c.stars)} ★ · {signed(c.forks)} ⑂</span>
                </li>
              ))}
            </ul>
          ) : <Empty>No changes.</Empty>}
        </Section>
      </div>
    </div>
  )
}

const signed = (n: number) => (n > 0 ? `+${n}` : String(n))

function Count({ label, n }: { label: string; n: number }) {
  return (
    <div className="rounded-xl border border-line bg-surface p-4">
      <div className="text-[13px] text-muted">{label}</div>
      <div className="mt-1 text-2xl font-semibold text-ink">{n}</div>
    </div>
  )
}

function Section({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return <Card title={title} subtitle={hint}>{children}</Card>
}

function Row({ href, title, meta }: { href: string; title: ReactNode; meta: ReactNode }) {
  return (
    <li className="flex items-baseline justify-between gap-3 py-1.5">
      <a href={href} target="_blank" rel="noreferrer" className="min-w-0 truncate text-ink hover:text-accent hover:underline">{title}</a>
      <span className="shrink-0 text-[12px] text-muted">{meta}</span>
    </li>
  )
}
