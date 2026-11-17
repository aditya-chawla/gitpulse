import { useMemo, useState } from 'react'
import { api, ApiError, isNoData, type NoData, type ReposResponse, type Settings } from '../api'
import { Card, ErrorBox, Pill, Spinner } from '../components/ui'
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
    </div>
  )
}
