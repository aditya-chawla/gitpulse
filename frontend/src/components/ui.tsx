import { useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import type { Status } from '../api'
import { scoreColor } from '../colors'

export function Card({ title, subtitle, actions, children, className = '' }: {
  title?: ReactNode
  subtitle?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
}) {
  return (
    <section className={`min-w-0 rounded-xl border border-line bg-surface p-4 sm:p-5 ${className}`}>
      {(title || actions) && (
        <header className="mb-4 flex flex-wrap items-start justify-between gap-2">
          <div>
            {title && <h2 className="text-[15px] font-semibold text-ink">{title}</h2>}
            {subtitle && <p className="mt-0.5 text-[13px] text-muted">{subtitle}</p>}
          </div>
          {actions}
        </header>
      )}
      {children}
    </section>
  )
}

/** A chart with a "Table" toggle so values are never colour-only. */
export function ChartCard({ title, subtitle, chart, table, extra, className }: {
  title: ReactNode
  subtitle?: ReactNode
  chart: ReactNode
  table: ReactNode
  extra?: ReactNode
  className?: string
}) {
  const [view, setView] = useState<'chart' | 'table'>('chart')
  return (
    <Card
      title={title}
      subtitle={subtitle}
      className={className}
      actions={
        <div className="flex items-center gap-2">
          {extra}
          <Segmented
            value={view}
            onChange={setView}
            options={[
              { value: 'chart', label: 'Chart' },
              { value: 'table', label: 'Table' },
            ]}
          />
        </div>
      }
    >
      {view === 'chart' ? chart : <div className="max-h-96 overflow-auto">{table}</div>}
    </Card>
  )
}

export function Segmented<T extends string>({ value, onChange, options }: {
  value: T
  onChange: (v: T) => void
  options: { value: T; label: string }[]
}) {
  return (
    <div role="group" className="inline-flex rounded-lg border border-line bg-surface-2 p-0.5 text-[12px]">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className={`rounded-md px-2.5 py-1 font-medium transition-colors ${
            value === o.value ? 'bg-surface text-ink shadow-sm' : 'text-muted hover:text-ink'
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function StatTile({ label, value, sub }: { label: string; value: ReactNode; sub?: ReactNode }) {
  return (
    <div className="rounded-xl border border-line bg-surface p-4">
      <div className="text-[13px] text-muted">{label}</div>
      <div className="mt-1 text-2xl font-semibold text-ink">{value}</div>
      {sub && <div className="mt-1 text-[12px] text-ink-2">{sub}</div>}
    </div>
  )
}

const statusStyle: Record<Status, { color: string; icon: ReactNode; label: string }> = {
  active: {
    color: 'var(--status-good)',
    label: 'Active',
    icon: <circle cx="6" cy="6" r="5" fill="currentColor" />,
  },
  slowing: {
    color: 'var(--status-warning)',
    label: 'Slowing',
    icon: (
      <>
        <circle cx="6" cy="6" r="4.25" fill="none" stroke="currentColor" strokeWidth="1.5" />
        <path d="M6 1.75a4.25 4.25 0 0 1 0 8.5z" fill="currentColor" />
      </>
    ),
  },
  dormant: {
    color: 'var(--status-neutral)',
    label: 'Dormant',
    icon: <circle cx="6" cy="6" r="4.25" fill="none" stroke="currentColor" strokeWidth="1.5" />,
  },
}

/** Status is always icon + label, never colour alone. */
export function StatusBadge({ status }: { status: Status }) {
  const s = statusStyle[status]
  return (
    <span className="inline-flex items-center gap-1.5 text-[13px] text-ink-2">
      <svg width="12" height="12" viewBox="0 0 12 12" style={{ color: s.color }} aria-hidden>
        {s.icon}
      </svg>
      {s.label}
    </span>
  )
}

export function ScoreMeter({ score, max = 100, width = 'w-20' }: { score: number; max?: number; width?: string }) {
  const pct = max === 0 ? 0 : Math.max(0, Math.min(100, (score / max) * 100))
  return (
    <span className={`inline-block h-2 ${width} overflow-hidden rounded-full`} style={{ background: 'var(--meter-track)' }}
      role="meter" aria-valuemin={0} aria-valuemax={max} aria-valuenow={score}>
      <span className="block h-full rounded-full" style={{ width: `${pct}%`, background: scoreColor((score / max) * 100) }} />
    </span>
  )
}

export function Legend({ items }: { items: { label: string; color: string }[] }) {
  return (
    <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1.5 text-[12px] text-ink-2">
      {items.map((i) => (
        <li key={i.label} className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2.5 w-2.5 rounded-sm" style={{ background: i.color }} aria-hidden />
          {i.label}
        </li>
      ))}
    </ul>
  )
}

/** Recharts custom tooltip shell. */
export function TipBox({ title, rows }: { title?: ReactNode; rows: { label: ReactNode; value: ReactNode; color?: string }[] }) {
  return (
    <div className="min-w-36 rounded-lg border border-line bg-surface px-3 py-2 text-[12px] shadow-lg">
      {title && <div className="mb-1 font-medium text-ink">{title}</div>}
      {rows.map((r, i) => (
        <div key={i} className="flex items-center justify-between gap-4">
          <span className="inline-flex items-center gap-1.5 text-ink-2">
            {r.color && <span className="inline-block h-2 w-2 rounded-sm" style={{ background: r.color }} />}
            {r.label}
          </span>
          <span className="tabular font-medium text-ink">{r.value}</span>
        </div>
      ))}
    </div>
  )
}

export function Table({ head, children }: { head: ReactNode[]; children: ReactNode }) {
  return (
    <table className="w-full text-left text-[13px]">
      <thead className="sticky top-0 bg-surface text-[12px] text-muted">
        <tr>{head.map((h, i) => <th key={i} className="border-b border-line px-2 py-1.5 font-medium">{h}</th>)}</tr>
      </thead>
      <tbody className="tabular text-ink-2 [&_td]:border-b [&_td]:border-line [&_td]:px-2 [&_td]:py-1.5">{children}</tbody>
    </table>
  )
}

export function Spinner({ label = 'Loading' }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 py-10 text-[13px] text-muted" role="status">
      <span className="h-4 w-4 animate-spin rounded-full border-2 border-line border-t-accent" />
      {label}…
    </div>
  )
}

export function ErrorBox({ error, onRetry }: { error: Error; onRetry?: () => void }) {
  return (
    <div className="rounded-xl border border-line bg-surface p-4 text-[13px]">
      <p className="font-medium text-ink">Couldn’t load this data</p>
      <p className="mt-1 text-muted">{error.message}</p>
      {onRetry && <button onClick={onRetry} className="mt-3 text-accent hover:underline">Try again</button>}
    </div>
  )
}

export function NoScanYet() {
  return (
    <Card>
      <div className="py-8 text-center">
        <p className="text-[15px] font-semibold text-ink">No scan yet</p>
        <p className="mx-auto mt-1 max-w-md text-[13px] text-muted">
          Run your first scan with the <b>Scan now</b> button. It reads your repositories from GitHub and
          stores a snapshot. Every page reads from those snapshots.
        </p>
      </div>
    </Card>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="py-6 text-center text-[13px] text-muted">{children}</p>
}

export function RepoLink({ id, name }: { id: number; name: string }) {
  return <Link to={`/repos/${id}`} className="font-medium text-ink hover:text-accent hover:underline">{name}</Link>
}

export function Pill({ children }: { children: ReactNode }) {
  return <span className="rounded-full border border-line px-1.5 py-px text-[11px] text-muted">{children}</span>
}
