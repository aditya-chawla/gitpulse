import { useState } from 'react'
import { api, ApiError, type PRReview, type PRReviewResult } from '../api'
import { AiOffNote, Card, Empty, ErrorBox, Pill, Spinner } from '../components/ui'
import { useApi, useData } from '../data'
import { ago } from '../format'

const statusStyle: Record<PRReview['status'], { label: string; cls: string }> = {
  pending: { label: 'Queued', cls: 'text-muted' },
  running: { label: 'Reviewing', cls: 'text-accent' },
  done: { label: 'Reviewed', cls: 'text-good-text' },
  skipped: { label: 'Skipped', cls: 'text-muted' },
  failed: { label: 'Failed', cls: 'text-critical' },
}
const modeLabel: Record<string, string> = { dashboard: 'dashboard only', comment: 'commented', off: 'off' }

function parse(r: PRReview): PRReviewResult | undefined {
  return r.result ?? undefined
}

export default function PRsPage() {
  const q = useApi<{ reviews: PRReview[]; aiEnabled: boolean }>('/api/prs?limit=100')
  const { invalidate } = useData()
  const [syncing, setSyncing] = useState(false)
  const [msg, setMsg] = useState<string>()
  const [open, setOpen] = useState<number>()

  const sync = async () => {
    setSyncing(true)
    setMsg(undefined)
    try {
      const res = await api<{ users: { openPrs: number; queued: number; reposChecked: number }[] }>('/api/prs/sync', { method: 'POST' })
      const u = res.users[0]
      setMsg(u ? `Checked ${u.reposChecked} repos: ${u.openPrs} open PRs, ${u.queued} queued for review.` : 'Nothing to check.')
      window.setTimeout(invalidate, 4000) // the worker reviews in the background
    } catch (e) {
      setMsg(e instanceof ApiError ? e.message : 'Sync failed')
    } finally {
      setSyncing(false)
    }
  }

  if (q.error) return <ErrorBox error={q.error} onRetry={q.reload} />
  if (!q.data) return <Spinner />
  const { reviews, aiEnabled } = q.data

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-ink">Pull requests</h1>
          <p className="text-[13px] text-muted">Each new commit on an open PR in your repos is reviewed once. Modes are set per repo in Settings.</p>
        </div>
        <div className="flex items-center gap-3">
          {msg && <span role="status" className="text-[13px] text-ink-2">{msg}</span>}
          <button onClick={sync} disabled={syncing}
            className="rounded-lg border border-line bg-surface px-3 py-1.5 text-[13px] font-medium text-ink hover:bg-surface-2 disabled:opacity-50">
            {syncing ? 'Checking…' : 'Check for new PRs'}
          </button>
        </div>
      </div>
      {!aiEnabled && <AiOffNote />}

      <Card>
        {reviews.length === 0 ? (
          <Empty>No PR reviews yet. Open a pull request in a repo whose PR assistant is on, or press “Check for new PRs”.</Empty>
        ) : (
          <ul className="divide-y divide-line">
            {reviews.map((r) => {
              const res = parse(r)
              const st = statusStyle[r.status]
              const expanded = open === r.id
              return (
                <li key={r.id} className="py-2.5">
                  <button onClick={() => setOpen(expanded ? undefined : r.id)} aria-expanded={expanded}
                    className="flex w-full flex-wrap items-baseline gap-x-3 gap-y-1 text-left">
                    <span className="min-w-0 flex-1 text-[14px] text-ink">
                      <span className="text-muted">{r.repo.split('/')[1]} #{r.number}</span> {r.title}
                    </span>
                    <span className="flex flex-wrap items-center gap-2 text-[12px]">
                      {res && res.flags.length > 0 && <Pill>{res.flags.length} notable</Pill>}
                      {res?.suggestedLabels.map((l) => <Pill key={l}>{l}</Pill>)}
                      <span className={`font-medium ${st.cls}`}>{st.label}</span>
                      {r.mode && r.status === 'done' && <span className="text-muted">· {modeLabel[r.mode] ?? r.mode}</span>}
                      <span className="text-muted">· <code className="font-mono">{r.headSha.slice(0, 7)}</code> · {ago(r.updatedAt)}</span>
                    </span>
                  </button>
                  {expanded && <Detail r={r} res={res} aiEnabled={aiEnabled} />}
                </li>
              )
            })}
          </ul>
        )}
      </Card>
    </div>
  )
}

function Detail({ r, res, aiEnabled }: { r: PRReview; res?: PRReviewResult; aiEnabled: boolean }) {
  return (
    <div className="mt-3 space-y-3 rounded-lg bg-surface-2 p-3 text-[13px] text-ink-2">
      <p className="flex flex-wrap gap-x-4 gap-y-1">
        <a href={r.url} className="text-accent hover:underline">Open PR on GitHub</a>
        {r.commentUrl && <a href={r.commentUrl} className="text-accent hover:underline">gitpulse comment</a>}
        <span className="text-muted">by {r.author || 'unknown'} · via {r.source}</span>
      </p>
      {r.status === 'skipped' && <p>Skipped: {r.skipReason}</p>}
      {r.status === 'failed' && <p className="text-critical">Failed: {r.error}</p>}
      {res && (
        <>
          <p className="text-ink">{res.filesChanged} files changed · +{res.additions} −{res.deletions}</p>
          {res.ai?.summary ? (
            <div>
              <h3 className="font-medium text-ink">Summary</h3>
              <p className="whitespace-pre-line">{res.ai.summary}</p>
            </div>
          ) : aiEnabled ? (
            // With AI off the page-level note already says so.
            <AiOffNote reason={r.aiStatus === 'over_budget' ? 'budget' : 'other'} />
          ) : null}
          <div>
            <h3 className="font-medium text-ink">Notable changes</h3>
            {res.flags.length === 0 && !res.ai?.notable?.length ? <p className="text-muted">Nothing flagged.</p> : (
              <ul className="ml-4 list-disc space-y-0.5">
                {res.flags.map((f) => (
                  <li key={f.kind}><span className="text-ink">{f.title}:</span>{' '}
                    <span className="font-mono text-[12px] break-all">{f.files.join(', ')}</span>{f.extra > 0 && ` and ${f.extra} more`}</li>
                ))}
                {res.ai?.notable?.map((n, i) => <li key={i}>{n}</li>)}
              </ul>
            )}
          </div>
          {res.ai?.suggestedTitle && <p><span className="font-medium text-ink">Suggested title:</span> <code className="font-mono">{res.ai.suggestedTitle}</code></p>}
          <div>
            <h3 className="font-medium text-ink">Suggested labels</h3>
            {res.suggestedLabels.length === 0 && !res.ai?.suggestedLabels?.length ? (
              <p className="text-muted">None of the repo’s labels fit.</p>
            ) : (
              <ul className="ml-4 list-disc">
                {res.suggestedLabels.map((l) => <li key={l}><code className="font-mono">{l}</code> <span className="text-muted">({res.labelReasons[l]})</span></li>)}
                {res.ai?.suggestedLabels?.filter((l) => !res.suggestedLabels.includes(l)).map((l) => <li key={l}><code className="font-mono">{l}</code> <span className="text-muted">(AI)</span></li>)}
              </ul>
            )}
          </div>
          {res.omitted.length > 0 && (
            <details>
              <summary className="cursor-pointer text-ink">Left out of the review ({res.omitted.length})</summary>
              <ul className="ml-4 mt-1 list-disc">
                {res.omitted.map((o) => <li key={o.path}><span className="font-mono text-[12px] break-all">{o.path}</span>: {o.reason}</li>)}
              </ul>
            </details>
          )}
        </>
      )}
    </div>
  )
}
