import { useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { api, ApiError, type ReleaseDraft, type ReleasePreview, type RepoSetting } from '../api'
import { AiOffNote, Card, Empty, ErrorBox, Pill, Spinner } from '../components/ui'
import { useApi } from '../data'
import { ago, date } from '../format'

export default function ReleasesPage() {
  const repos = useApi<{ repos: RepoSetting[] }>('/api/repo-settings')
  const drafts = useApi<{ drafts: ReleaseDraft[] }>('/api/releases/drafts')
  const [params, setParams] = useSearchParams()
  const repoId = Number(params.get('repo')) || undefined

  if (repos.error) return <ErrorBox error={repos.error} onRetry={repos.reload} />
  if (!repos.data) return <Spinner />
  const list = repos.data.repos.filter((r) => !r.isArchived && !r.isFork)

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-xl font-semibold text-ink">Release notes</h1>
        <p className="text-[13px] text-muted">Notes for everything merged since the last release. gitpulse only ever creates <b className="font-medium text-ink-2">draft</b> releases; publishing stays with you on GitHub.</p>
      </div>
      <Card>
        <label className="flex flex-wrap items-center gap-2 text-[14px] text-ink-2">
          Repository
          <select value={repoId ?? ''} onChange={(e) => setParams(e.target.value ? { repo: e.target.value } : {})} aria-label="Repository"
            className="min-w-0 max-w-full rounded-lg border border-line bg-surface-2 px-2 py-1.5 text-ink">
            <option value="">Choose a repository…</option>
            {list.map((r) => <option key={r.repoId} value={r.repoId}>{r.fullName}{r.releasesEnabled ? '' : ' (drafts off)'}</option>)}
          </select>
        </label>
      </Card>
      {repoId && <PreviewPanel key={repoId} repoId={repoId} onCreated={drafts.reload} />}
      <Card title="Drafts created by gitpulse">
        {!drafts.data ? <Spinner /> : drafts.data.drafts.length === 0 ? <Empty>None yet.</Empty> : (
          <ul className="divide-y divide-line text-[13px]">
            {drafts.data.drafts.map((d) => (
              <li key={d.id} className="flex flex-wrap justify-between gap-2 py-1.5">
                <span><span className="text-muted">{d.repo}</span> <b className="font-medium text-ink">{d.tag}</b>{d.baseTag && <span className="text-muted"> (since {d.baseTag})</span>}</span>
                <span className="text-muted"><a href={d.url} className="text-accent hover:underline">Open draft</a> · {ago(d.createdAt)}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  )
}

function PreviewPanel({ repoId, onCreated }: { repoId: number; onCreated: () => void }) {
  const [p, setP] = useState<ReleasePreview>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<Error>()
  const [version, setVersion] = useState('')
  const [body, setBody] = useState('')
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<{ url: string; tag: string }>()
  const [createErr, setCreateErr] = useState<string>()

  const load = async () => {
    setLoading(true)
    setError(undefined)
    setCreated(undefined)
    try {
      const res = await api<ReleasePreview>(`/api/repos/${repoId}/release-notes/preview`, { method: 'POST' })
      setP(res)
      setVersion(res.suggestedVersion)
      setBody(res.markdown)
    } catch (e) {
      setError(e as Error)
    } finally {
      setLoading(false)
    }
  }

  const create = async () => {
    if (!p) return
    setCreating(true)
    setCreateErr(undefined)
    try {
      const res = await api<{ url: string; tag: string }>(`/api/repos/${repoId}/releases`, {
        method: 'POST', body: JSON.stringify({ tag: version.trim(), name: version.trim(), body, target: p.defaultBranch, baseTag: p.baseTag }),
      })
      setCreated(res)
      onCreated()
    } catch (e) {
      setCreateErr(e instanceof ApiError ? e.message : 'Could not create the draft')
    } finally {
      setCreating(false)
    }
  }

  if (!p) {
    return (
      <Card>
        {error && <p className="mb-2 text-[13px] text-critical">{error.message}</p>}
        <button onClick={load} disabled={loading} className="rounded-lg bg-accent px-4 py-2 text-[14px] font-medium text-white disabled:opacity-50">
          {loading ? 'Collecting merged PRs…' : 'Preview release notes'}
        </button>
      </Card>
    )
  }

  const base = p.baseTag
    ? <>since {p.baseSource === 'release' ? 'release' : 'tag'} <b className="font-medium text-ink">{p.baseTag}</b>{p.baseDate && <> ({date(p.baseDate)})</>}</>
    : <>no earlier release or semver tag, so everything merged</>
  return (
    <div className="space-y-5">
      <Card title={`${p.repo}: ${p.count} merged PR${p.count === 1 ? '' : 's'}`} subtitle={<>Into <code className="font-mono">{p.defaultBranch}</code>, {base}{p.truncated && '. Showing the latest 200'}.</>}
        actions={<button onClick={load} disabled={loading} className="text-[13px] text-accent hover:underline">{loading ? 'Refreshing…' : 'Refresh'}</button>}>
        {p.aiStatus === 'disabled' && <AiOffNote className="mb-3" />}
        {p.aiStatus === 'over_budget' && <AiOffNote reason="budget" className="mb-3" />}
        {p.count === 0 ? <Empty>Nothing merged since {p.baseTag || 'the start'}: no release needed.</Empty> : (
          <div className="grid gap-4 md:grid-cols-2">
            {p.groups.map((g) => (
              <div key={g.key}>
                <h3 className="mb-1 text-[14px] font-semibold text-ink">{g.title} <span className="font-normal text-muted">({g.items.length})</span></h3>
                <ul className="space-y-1 text-[13px] text-ink-2">
                  {g.items.map((it) => (
                    <li key={it.number}>
                      {it.breaking && <Pill>breaking</Pill>}{' '}
                      <a href={it.url} className="text-ink hover:text-accent hover:underline">{it.summary || it.title}</a>{' '}
                      <span className="text-muted">#{it.number} · {it.author} · {it.why}</span>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        )}
      </Card>

      {p.count > 0 && (
        <Card title="Draft release" subtitle={`Suggested ${p.bump === 'initial' ? 'first version' : `${p.bump} bump`}: ${p.suggestedVersion}. Edit anything before creating the draft.`}>
          <div className="space-y-3">
            <label className="flex flex-wrap items-center gap-2 text-[14px] text-ink-2">
              Version / tag
              <input value={version} onChange={(e) => setVersion(e.target.value)} spellCheck={false} aria-label="Version"
                className="w-40 rounded-lg border border-line bg-surface-2 px-2 py-1.5 font-mono text-[13px] text-ink" />
            </label>
            <textarea value={body} onChange={(e) => setBody(e.target.value)} rows={14} spellCheck={false} aria-label="Release notes"
              className="w-full rounded-lg border border-line bg-surface-2 px-3 py-2 font-mono text-[12px] text-ink" />
            {created ? (
              <p role="status" className="text-[14px] text-good-text">
                Draft {created.tag} created. <a href={created.url} className="font-medium text-accent hover:underline">Review and publish it on GitHub</a>.
              </p>
            ) : p.releasesEnabled ? (
              <div className="flex flex-wrap items-center gap-3">
                <button onClick={create} disabled={creating || !version.trim()} className="rounded-lg bg-accent px-4 py-2 text-[14px] font-medium text-white disabled:opacity-50">
                  {creating ? 'Creating…' : 'Create draft release'}
                </button>
                <span className="text-[12px] text-muted">Creates a draft only. Nothing is published or tagged until you publish it on GitHub.</span>
              </div>
            ) : (
              <p className="text-[13px] text-ink-2">Draft releases are off for this repository. Turn on <b className="font-medium">Draft releases</b> in <Link to="/settings" className="text-accent hover:underline">Settings → Repository automation</Link> to create one. You can still copy the notes above.</p>
            )}
            {createErr && <p role="alert" className="text-[13px] text-critical">{createErr}</p>}
          </div>
        </Card>
      )}
    </div>
  )
}
