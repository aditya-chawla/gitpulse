import { useState } from 'react'
import { Link } from 'react-router'
import { api, type ActionItem } from '../api'
import { useApi, useData } from '../data'
import { Card } from './ui'

const severityLabel: Record<number, { text: string; cls: string }> = {
  5: { text: 'Critical', cls: 'border-critical/40 bg-critical/10 text-critical' },
  4: { text: 'High', cls: 'border-amber-500/40 bg-amber-500/10 text-amber-500' },
  3: { text: 'Medium', cls: 'border-sky-500/40 bg-sky-500/10 text-sky-500' },
  2: { text: 'Low', cls: 'border-line bg-surface-2 text-muted' },
  1: { text: 'Info', cls: 'border-line bg-surface-2 text-muted' },
}

export default function ActionQueue({ limit = 5 }: { limit?: number }) {
  const { invalidate } = useData()
  const q = useApi<{ items: ActionItem[] }>(`/api/actions?limit=${limit}`)
  const [busyId, setBusyId] = useState<number | null>(null)
  const [hidden, setHidden] = useState<number[]>([])

  if (q.error || !q.data) return null
  const items = q.data.items.filter((it) => !hidden.includes(it.id))
  if (items.length === 0) return null

  const dismiss = async (id: number) => {
    setBusyId(id)
    try {
      await api(`/api/actions/${id}/dismiss`, { method: 'POST' })
      setHidden((h) => [...h, id])
      invalidate()
    } finally {
      setBusyId(null)
    }
  }

  return (
    <Card
      title="What to fix next"
      subtitle={`Top ${items.length} severity-weighted maintenance signals across your tracked repositories`}
    >
      <ul className="divide-y divide-line">
        {items.map((it) => {
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
              <div className="flex shrink-0 items-center gap-2 text-[12px]">
                {it.url && (
                  <a
                    href={it.url}
                    target="_blank"
                    rel="noreferrer"
                    className="rounded-lg border border-line bg-surface-2 px-2.5 py-1 text-ink-2 hover:text-accent"
                  >
                    Open ↗
                  </a>
                )}
                <button
                  type="button"
                  disabled={busyId === it.id}
                  onClick={() => dismiss(it.id)}
                  className="rounded-lg border border-line px-2.5 py-1 text-muted hover:text-ink disabled:opacity-50"
                >
                  Dismiss
                </button>
              </div>
            </li>
          )
        })}
      </ul>
    </Card>
  )
}
