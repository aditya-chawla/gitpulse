import { useMemo, useState } from 'react'
import {
  api, ApiError, isNoData, type AppStatus, type LLMUsage, type NoData, type PRMode, type RepoSetting, type ReposResponse,
  type Settings,
} from '../api'
import { Card, ErrorBox, Pill, ScoreMeter, Spinner } from '../components/ui'
import { useApi, useData } from '../data'

// Mirrors Go's path.Match for the preview (*, ?, [...]), case-insensitive.
function globToRegex(glob: string): RegExp | null {
  let re = ''
  for (let i = 0; i < glob.length; i++) {
    const c = glob[i]
    if (c === '*') re += '[^/]*'
    else if (c === '?') re += '[^/]'
    else if (c === '[') {
      const end = glob.indexOf(']', i)
      if (end < 0) return null
      re += '[' + glob.slice(i + 1, end).replace(/^\^/, '^').replace(/\\/g, '\\\\') + ']'
      i = end
    } else re += c.replace(/[.+^${}()|\\]/g, '\\$&')
  }
  try {
    return new RegExp(`^${re}$`, 'i')
  } catch {
    return null
  }
}

const zones: string[] = (() => {
  try {
    return Intl.supportedValuesOf('timeZone')
  } catch {
    return ['UTC']
  }
})()

export default function SettingsPage() {
  const s = useApi<Settings>('/api/settings')
  const r = useApi<ReposResponse | NoData>('/api/repos')
  if (s.error) return <ErrorBox error={s.error} onRetry={s.reload} />
  if (!s.data || !r.data) return <Spinner />
  return <Form initial={s.data} repos={isNoData(r.data) ? [] : r.data.repos} />
}

function Form({ initial, repos }: { initial: Settings; repos: ReposResponse['repos'] }) {
  const { invalidate } = useData()
  const [st, setSt] = useState<Settings>(initial)
  const [globText, setGlobText] = useState(initial.excludedRepoGlobs.join('\n'))
  const [filter, setFilter] = useState('')
  const [saving, setSaving] = useState(false)
  // The message belongs to the exact form state it was produced for; any edit hides it.
  const [msg, setMsg] = useState<{ ok: boolean; text: string; for: string }>()
  const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone
  const formKey = JSON.stringify([st, globText])

  const globs = useMemo(() => globText.split('\n').map((g) => g.trim()).filter(Boolean), [globText])
  const invalid = globs.filter((g) => !globToRegex(g))
  const matched = useMemo(() => {
    const res = globs.map(globToRegex).filter((x): x is RegExp => !!x)
    return repos.filter((r) => res.some((re) => re.test(r.name))).map((r) => r.name)
  }, [globs, repos])

  const toggleRepo = (id: number) =>
    setSt((s) => ({ ...s, excludedRepoIds: s.excludedRepoIds.includes(id) ? s.excludedRepoIds.filter((x) => x !== id) : [...s.excludedRepoIds, id] }))

  const save = async () => {
    setSaving(true)
    try {
      const saved = await api<Settings>('/api/settings', { method: 'PUT', body: JSON.stringify({ ...st, excludedRepoGlobs: globs }) })
      const text = saved.excludedRepoGlobs.join('\n')
      setSt(saved)
      setGlobText(text)
      invalidate()
      setMsg({ ok: true, text: 'Saved. Charts now use these settings.', for: JSON.stringify([saved, text]) })
    } catch (e) {
      setMsg({ ok: false, text: e instanceof ApiError ? e.message : 'Save failed', for: formKey })
    } finally {
      setSaving(false)
    }
  }

  const shown = repos.filter((r) => r.name.toLowerCase().includes(filter.toLowerCase()))

  return (
    <div className="space-y-5">
      <h1 className="text-xl font-semibold text-ink">Settings</h1>

      <AppCard />
      <AutomationCard />
      <AiUsageCard />

      {/* The sticky save bar is scoped to this section: the cards above save immediately. */}
      <section className="space-y-5" aria-labelledby="dashboard-data">
        <h2 id="dashboard-data" className="pt-2 text-[15px] font-semibold text-ink">Dashboard data</h2>
        <Card title="What counts in aggregates" subtitle="Applies to analytics, averages and the digest. Excluded repos are still scanned and shown on the health page.">
          <div className="space-y-2 text-[14px] text-ink-2">
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={st.includeForks} onChange={(e) => setSt({ ...st, includeForks: e.target.checked })} />
              Include forks
            </label>
            <label className="flex items-center gap-2">
              <input type="checkbox" checked={st.includeArchived} onChange={(e) => setSt({ ...st, includeArchived: e.target.checked })} />
              Include archived repositories
            </label>
          </div>
        </Card>

        <Card title="Exclude by name pattern" subtitle="One pattern per line, matched against the repo name, case-insensitive. * matches anything, ? one character.">
          <textarea value={globText} onChange={(e) => setGlobText(e.target.value)} rows={4} spellCheck={false}
            placeholder={'*-playground\ndotfiles'} aria-label="Exclusion patterns"
            className="w-full rounded-lg border border-line bg-surface-2 px-3 py-2 font-mono text-[13px] text-ink placeholder:text-muted" />
          {invalid.length > 0 && <p className="mt-1 text-[13px] text-critical">Invalid pattern: {invalid.join(', ')}</p>}
          <p className="mt-2 text-[13px] text-muted">
            {globs.length === 0 ? 'No patterns.' : matched.length ? <>Matches {matched.length}: {matched.join(', ')}</> : 'Matches no repositories.'}
          </p>
        </Card>

        <Card title="Exclude specific repositories" subtitle={`${st.excludedRepoIds.length} excluded`}>
          {repos.length === 0 ? (
            <p className="text-[13px] text-muted">Run a scan first to see your repositories here.</p>
          ) : (
            <>
              <input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" aria-label="Filter repositories"
                className="mb-3 w-full max-w-xs rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-[13px] text-ink placeholder:text-muted" />
              <ul className="grid max-h-80 gap-x-6 gap-y-1 overflow-auto sm:grid-cols-2 lg:grid-cols-3">
                {shown.map((r) => (
                  <li key={r.id}>
                    <label className="flex items-center gap-2 text-[13px] text-ink-2">
                      <input type="checkbox" checked={st.excludedRepoIds.includes(r.id)} onChange={() => toggleRepo(r.id)} />
                      <span className="truncate">{r.name}</span>
                      {r.isFork && <Pill>fork</Pill>}
                      {r.isArchived && <Pill>archived</Pill>}
                    </label>
                  </li>
                ))}
              </ul>
            </>
          )}
        </Card>

        <Card title="Timezone" subtitle="Used for the day/hour activity chart and streaks">
          <div className="flex flex-wrap items-center gap-2 text-[13px]">
            <select value={st.timezone} onChange={(e) => setSt({ ...st, timezone: e.target.value })} aria-label="Timezone"
              className="rounded-lg border border-line bg-surface-2 px-2 py-1.5 text-ink">
              {(zones.includes(st.timezone) ? zones : [st.timezone, ...zones]).map((z) => <option key={z}>{z}</option>)}
            </select>
            {browserTz && browserTz !== st.timezone && (
              <button onClick={() => setSt({ ...st, timezone: browserTz })} className="text-accent hover:underline">Use this browser’s ({browserTz})</button>
            )}
          </div>
        </Card>

        <div className="sticky bottom-0 flex items-center gap-3 border-t border-line bg-page/95 py-3 backdrop-blur">
          <button onClick={save} disabled={saving || invalid.length > 0}
            className="rounded-lg bg-accent px-4 py-2 text-[14px] font-medium text-white disabled:opacity-50">
            {saving ? 'Saving…' : 'Save settings'}
          </button>
          {msg && msg.for === formKey && <span role="status" className={`text-[13px] ${msg.ok ? 'text-good-text' : 'text-critical'}`}>{msg.text}</span>}
        </div>
      </section>
    </div>
  )
}

function AppCard() {
  const { data, error, reload } = useApi<AppStatus>('/api/app')
  if (error) return <ErrorBox error={error} onRetry={reload} />
  if (!data) return null
  return (
    <Card title="GitHub App" subtitle="gitpulse reads and writes GitHub through its app installation, never your personal token.">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-[14px] text-ink-2">
        {data.installed ? (
          <>
            <span>
              {data.suspended ? 'Installed but suspended' : 'Installed'} on{' '}
              {data.repositorySelection === 'all' ? 'all repositories' : 'selected repositories'}.
            </span>
            {data.configureUrl && <a href={data.configureUrl} className="text-accent hover:underline">Change on GitHub</a>}
          </>
        ) : (
          <>
            <span>Not installed. Scans cannot run until it is.</span>
            {data.installUrl && <a href={data.installUrl} className="text-accent hover:underline">Install on GitHub</a>}
          </>
        )}
      </div>
      {!!data.missingPermissions?.length && (
        <p className="mt-2 text-[13px] text-critical">Missing permissions: {data.missingPermissions.join(', ')}</p>
      )}
      {data.installed && data.repositorySelection === 'selected' && (
        <p className="mt-2 text-[13px] text-muted">Private repositories not selected there are left out of analytics and health.</p>
      )}
    </Card>
  )
}

const modeLabels: Record<PRMode, string> = { off: 'Off', dashboard: 'Dashboard only', comment: 'Comment on PR' }

function AutomationCard() {
  const q = useApi<{ repos: RepoSetting[] }>('/api/repo-settings')
  const [rows, setRows] = useState<Record<number, RepoSetting>>({})
  const [busy, setBusy] = useState<number>()
  const [msg, setMsg] = useState<{ ok: boolean; text: string }>()
  const [filter, setFilter] = useState('')
  const [writesOnly, setWritesOnly] = useState(false)

  if (q.error) return <ErrorBox error={q.error} onRetry={q.reload} />
  if (!q.data) return null
  const list = q.data.repos.map((r) => rows[r.repoId] ?? r)

  const update = async (r: RepoSetting, patch: Partial<RepoSetting>) => {
    const next = { ...r, ...patch }
    setBusy(r.repoId)
    setMsg(undefined)
    try {
      const saved = await api<RepoSetting>(`/api/repo-settings/${r.repoId}`, {
        method: 'PUT', body: JSON.stringify({ prMode: next.prMode, releasesEnabled: next.releasesEnabled }),
      })
      setRows((m) => ({ ...m, [r.repoId]: saved }))
      setMsg({ ok: true, text: `Saved ${saved.fullName}.` })
    } catch (e) {
      setMsg({ ok: false, text: e instanceof ApiError ? e.message : 'Save failed' })
    } finally {
      setBusy(undefined)
    }
  }

  const writing = list.filter((r) => r.prMode === 'comment' || r.releasesEnabled)
  const shown = list.filter((r) =>
    r.fullName.toLowerCase().includes(filter.toLowerCase()) && (!writesOnly || r.prMode === 'comment' || r.releasesEnabled))

  return (
    <Card title="Repository automation"
      subtitle="Per-repo switches for features that act on GitHub. Writes are off everywhere until you turn them on here. Changes save immediately.">
      {list.length === 0 ? (
        <p className="text-[13px] text-muted">Run a scan first to see your repositories here.</p>
      ) : (
        <>
          <div className="mb-3 flex flex-wrap items-center gap-3 text-[13px]">
            <input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter" aria-label="Filter repositories"
              className="w-full max-w-xs rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-ink placeholder:text-muted" />
            <label className="flex items-center gap-2 text-ink-2">
              <input type="checkbox" checked={writesOnly} onChange={(e) => setWritesOnly(e.target.checked)} />
              Only repos with writes on ({writing.length})
            </label>
            {msg && <span role="status" className={msg.ok ? 'text-good-text' : 'text-critical'}>{msg.text}</span>}
          </div>
          <div className="max-h-96 overflow-auto">
            <table className="w-full text-left text-[13px]">
              <thead className="sticky top-0 bg-surface text-muted">
                <tr>
                  <th className="py-2 pr-3 font-medium">Repository</th>
                  <th className="py-2 pr-3 font-medium">PR assistant</th>
                  <th className="py-2 font-medium">Draft releases</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((r) => (
                  <tr key={r.repoId} className="border-t border-line">
                    <td className="py-1.5 pr-3 text-ink-2">
                      <span className="mr-2 break-all">{r.fullName}</span>
                      {r.visibility === 'private' && <Pill>private</Pill>}
                      {r.isArchived && <Pill>archived</Pill>}
                    </td>
                    <td className="py-1.5 pr-3">
                      <select value={r.prMode} disabled={busy === r.repoId} aria-label={`PR assistant for ${r.fullName}`}
                        onChange={(e) => update(r, { prMode: e.target.value as PRMode })}
                        className="rounded-md border border-line bg-surface-2 px-2 py-1 text-ink">
                        {(Object.keys(modeLabels) as PRMode[]).map((m) => <option key={m} value={m}>{modeLabels[m]}</option>)}
                      </select>
                    </td>
                    <td className="py-1.5">
                      <label className="flex items-center gap-2 text-ink-2">
                        <input type="checkbox" checked={r.releasesEnabled} disabled={busy === r.repoId}
                          onChange={(e) => update(r, { releasesEnabled: e.target.checked })} />
                        {r.releasesEnabled ? 'Allowed' : 'Off'}
                      </label>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-2 text-[12px] text-muted">
            Dashboard only: PRs are summarised in gitpulse and nothing is posted. Comment on PR: gitpulse also keeps one comment
            up to date on each PR. Draft releases: allows creating draft (never published) releases.
          </p>
        </>
      )}
    </Card>
  )
}

const usd = (n: number) => `$${n < 0.01 && n > 0 ? n.toFixed(4) : n.toFixed(2)}`
const featureNames: Record<string, string> = { pr_review: 'PR assistant', release_notes: 'Release notes' }

function AiUsageCard() {
  const { data, error, reload } = useApi<LLMUsage>('/api/llm/usage')
  if (error) return <ErrorBox error={error} onRetry={reload} />
  if (!data) return null
  const month = new Date(`${data.month}-01T00:00:00Z`).toLocaleDateString(undefined, { month: 'long', year: 'numeric', timeZone: 'UTC' })
  return (
    <Card title="AI usage" subtitle={`${month} (UTC). Every AI feature also works without AI, using rule-based output.`}>
      {!data.enabled ? (
        <div className="space-y-1 text-[14px] text-ink-2">
          <p><b className="font-medium text-ink">AI disabled</b> ({data.disabledReason ?? 'no API key'}). Spent this month: {usd(data.spentUsd)}.</p>
          <p className="text-[13px] text-muted">Set <code className="font-mono">ANTHROPIC_API_KEY</code> on the server to turn on AI summaries. The monthly budget is {usd(data.budgetUsd)}.</p>
        </div>
      ) : (
        <div className="space-y-3 text-[14px] text-ink-2">
          <div className="flex flex-wrap items-center gap-3">
            <span className="text-ink"><b className="font-semibold">{usd(data.spentUsd)}</b> of {usd(data.budgetUsd)}</span>
            <ScoreMeter score={Math.min(data.spentUsd, data.budgetUsd)} max={data.budgetUsd || 1} width="w-40" />
            <span className="text-[13px] text-muted">{data.provider} · {data.model}</span>
          </div>
          {data.capped && (
            <p role="status" className="text-[13px] text-critical">Budget reached: AI features are paused until next month and fall back to rule-based output.</p>
          )}
          {data.byFeature.length > 0 ? (
            <table className="w-full max-w-lg text-left text-[13px] tabular">
              <thead className="text-muted">
                <tr><th className="py-1 font-medium">Feature</th><th className="py-1 font-medium">Calls</th><th className="py-1 font-medium">Cached</th><th className="py-1 font-medium">Skipped</th><th className="py-1 text-right font-medium">Cost</th></tr>
              </thead>
              <tbody>
                {data.byFeature.map((f) => (
                  <tr key={f.feature} className="border-t border-line">
                    <td className="py-1">{featureNames[f.feature] ?? f.feature}</td>
                    <td className="py-1">{f.calls}{f.failed > 0 && <span className="text-muted"> ({f.failed} failed)</span>}</td>
                    <td className="py-1">{f.cacheHits}</td>
                    <td className="py-1">{f.skipped}</td>
                    <td className="py-1 text-right">{usd(f.costUsd)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : <p className="text-[13px] text-muted">No AI calls yet this month.</p>}
        </div>
      )}
    </Card>
  )
}
