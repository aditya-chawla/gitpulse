import { useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import {
  Bar, BarChart, CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from 'recharts'
import type { LanguageShare, WeekPoint, YearShare } from '../api'
import { OTHER, OTHER_COLOR } from '../colors'
import { bytes, hourLabel, num, pct, shortDate, WEEKDAYS, WEEKDAYS_SHORT } from '../format'
import { Legend, TipBox } from './ui'

/** Hover card that follows a mark; used by the HTML-drawn charts. */
function HoverTip({ children, tip, className = '' }: { children: ReactNode; tip: ReactNode; className?: string }) {
  return (
    <span className={`group/tip relative block ${className}`}>
      {children}
      <span className="pointer-events-none absolute bottom-full left-1/2 z-20 mb-2 hidden -translate-x-1/2 whitespace-nowrap group-hover/tip:block group-focus-within/tip:block">
        {tip}
      </span>
    </span>
  )
}

/** Horizontal bars, value at the tip. Single series: no legend. */
export function BarList({ rows, color = 'var(--series-1)', format = num, linkTo }: {
  rows: { key: string | number; label: string; value: number; color?: string; tip?: ReactNode; id?: number }[]
  color?: string
  format?: (n: number) => string
  linkTo?: (id: number) => string
}) {
  const max = Math.max(1, ...rows.map((r) => r.value))
  return (
    <ul className="space-y-2">
      {rows.map((r) => (
        <li key={r.key} className="grid grid-cols-[minmax(6rem,11rem)_1fr] items-center gap-3 text-[13px]">
          <span className="truncate text-ink-2" title={r.label}>
            {linkTo && r.id != null ? (
              <Link to={linkTo(r.id)} className="hover:text-accent hover:underline">{r.label}</Link>
            ) : r.label}
          </span>
          <HoverTip tip={r.tip ?? <TipBox rows={[{ label: r.label, value: format(r.value), color: r.color ?? color }]} />}>
            <span className="flex items-center gap-2" tabIndex={0}>
              <span
                className="block h-3 rounded-r-[4px]"
                style={{ width: `${Math.max(0.5, (r.value / max) * 85)}%`, background: r.color ?? color }}
              />
              <span className="tabular shrink-0 text-[12px] text-ink-2">{format(r.value)}</span>
            </span>
          </HoverTip>
        </li>
      ))}
    </ul>
  )
}

/** One whole split into parts. thin: 8px, no text; tall: 28px with inside labels. */
export function SegmentedBar({ parts, color, tall = false, label }: {
  parts: { language: string; bytes: number; percent: number }[]
  color: (language: string) => string
  tall?: boolean
  label: string
}) {
  const shown = parts.filter((p) => p.bytes > 0)
  return (
    <div
      className={`flex w-full gap-[2px] ${tall ? 'h-7' : 'h-2'}`}
      role="img"
      aria-label={`${label}: ${shown.map((p) => `${p.language} ${pct(p.percent)}`).join(', ')}`}
    >
      {shown.map((p) => {
        const bg = p.language === OTHER ? OTHER_COLOR : color(p.language)
        const darkText = bg === 'var(--series-4)' // near-black on yellow
        return (
          <span key={p.language} style={{ flex: `${p.percent} 1 0`, minWidth: 3 }} className="h-full">
            <HoverTip className="h-full" tip={<TipBox title={p.language} rows={[{ label: 'Share', value: pct(p.percent) }, { label: 'Size', value: bytes(p.bytes) }]} />}>
              <span
                tabIndex={0}
                className={`flex h-full items-center ${tall ? 'rounded-[4px] px-1.5' : 'rounded-full'}`}
                style={{ background: bg }}
              >
                {tall && p.percent >= 9 && (
                  <span className={`text-[11px] font-medium ${darkText ? 'text-[#0b0b0b]' : 'text-white'}`}>{pct(p.percent)}</span>
                )}
              </span>
            </HoverTip>
          </span>
        )
      })}
    </div>
  )
}

function useRechartsTip() {
  return {
    cursor: { fill: 'var(--grid)', fillOpacity: 0.5 },
    wrapperStyle: { outline: 'none', zIndex: 30 },
    isAnimationActive: false,
  } as const
}

/** 100% stacked columns: language share per year. */
export function LanguageTrendChart({ trend, languages, color }: {
  trend: YearShare[]
  languages: string[]
  color: (l: string) => string
}) {
  const tip = useRechartsTip()
  const keys = useMemo(() => {
    const k = [...languages]
    if (trend.some((y) => (y.percents[OTHER] ?? 0) > 0)) k.push(OTHER)
    return k
  }, [trend, languages])
  const data = trend.map((y) => ({ year: String(y.year), repos: y.repos, ...y.percents }))
  return (
    <>
      <div className="h-64">
        <ResponsiveContainer>
          <BarChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: -12 }} barCategoryGap="30%">
            <CartesianGrid vertical={false} />
            <XAxis dataKey="year" tickLine={false} axisLine={{ stroke: 'var(--axis)' }} />
            <YAxis domain={[0, 100]} ticks={[0, 25, 50, 75, 100]} tickFormatter={(v) => `${v}%`} tickLine={false} axisLine={false} />
            <Tooltip
              {...tip}
              content={({ active, payload, label }) =>
                active && payload?.length ? (
                  <TipBox
                    title={`${label} · ${payload[0].payload.repos} repos`}
                    rows={[...payload].reverse().filter((p) => Number(p.value) > 0).map((p) => ({
                      label: String(p.dataKey), value: pct(Number(p.value)), color: p.color,
                    }))}
                  />
                ) : null
              }
            />
            {keys.map((k, i) => (
              <Bar key={k} dataKey={k} stackId="a" fill={k === OTHER ? OTHER_COLOR : color(k)} maxBarSize={24}
                stroke="var(--surface-1)" strokeWidth={2} isAnimationActive={false}
                radius={i === keys.length - 1 ? [4, 4, 0, 0] : 0} />
            ))}
          </BarChart>
        </ResponsiveContainer>
      </div>
      <Legend items={keys.map((k) => ({ label: k, color: k === OTHER ? OTHER_COLOR : color(k) }))} />
    </>
  )
}

/** Weekly commits: mine vs all authors (one axis, two lines). */
export function WeeklyChart({ weeks }: { weeks: WeekPoint[] }) {
  const tip = useRechartsTip()
  const identical = weeks.every((w) => w.all === w.mine)
  const series = identical
    ? [{ key: 'mine', label: 'Commits', color: 'var(--series-1)' }]
    : [
        { key: 'mine', label: 'My commits', color: 'var(--series-1)' },
        { key: 'all', label: 'All authors', color: 'var(--series-2)' },
      ]
  return (
    <>
      <div className="h-56">
        <ResponsiveContainer>
          <LineChart data={weeks} margin={{ top: 8, right: 8, bottom: 0, left: -20 }}>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="week" tickFormatter={shortDate} tickLine={false} axisLine={{ stroke: 'var(--axis)' }} minTickGap={40} />
            <YAxis allowDecimals={false} tickLine={false} axisLine={false} />
            <Tooltip
              {...tip}
              cursor={{ stroke: 'var(--axis)', strokeWidth: 1 }}
              content={({ active, payload, label }) =>
                active && payload?.length ? (
                  <TipBox title={`Week of ${shortDate(String(label))}`}
                    rows={series.map((s) => ({ label: s.label, value: num(Number(payload[0].payload[s.key])), color: s.color }))} />
                ) : null
              }
            />
            {series.map((s) => (
              <Line key={s.key} type="monotone" dataKey={s.key} stroke={s.color} strokeWidth={2} dot={false}
                activeDot={{ r: 4, stroke: 'var(--surface-1)', strokeWidth: 2 }} isAnimationActive={false}
                strokeLinecap="round" strokeLinejoin="round" />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
      {series.length > 1 && <Legend items={series.map((s) => ({ label: s.label, color: s.color }))} />}
      {identical && <p className="mt-2 text-[12px] text-muted">Every default-branch commit in these weeks is yours.</p>}
    </>
  )
}

const SEQ = ['var(--seq-0)', 'var(--seq-1)', 'var(--seq-2)', 'var(--seq-3)', 'var(--seq-4)', 'var(--seq-5)']

/** Day × hour heatmap on a single-hue sequential ramp. */
export function Heatmap({ grid }: { grid: number[][] }) {
  const max = Math.max(0, ...grid.flat())
  const step = (v: number) => (v === 0 || max === 0 ? 0 : Math.min(5, 1 + Math.floor(((v - 1) / Math.max(1, max - 1)) * 4.999)))
  const [hover, setHover] = useState<{ d: number; h: number } | null>(null)
  return (
    <div>
      <div className="overflow-x-auto">
        <div className="grid min-w-[560px] grid-cols-[2.5rem_repeat(24,minmax(0,1fr))] gap-[2px] text-[11px] text-muted">
          <span />
          {Array.from({ length: 24 }, (_, h) => (
            <span key={h} className="text-center">{h % 3 === 0 ? hourLabel(h) : ''}</span>
          ))}
          {grid.map((row, d) => (
            <div key={d} className="contents">
              <span className="pr-1 leading-5">{WEEKDAYS_SHORT[d]}</span>
              {row.map((v, h) => (
                <span
                  key={h}
                  tabIndex={0}
                  onMouseEnter={() => setHover({ d, h })}
                  onFocus={() => setHover({ d, h })}
                  onMouseLeave={() => setHover(null)}
                  onBlur={() => setHover(null)}
                  aria-label={`${WEEKDAYS[d]} ${hourLabel(h)}: ${v} commits`}
                  className={`h-5 rounded-[3px] ${hover?.d === d && hover.h === h ? 'ring-2 ring-[var(--text-primary)]' : ''}`}
                  style={{ background: SEQ[step(v)] }}
                />
              ))}
            </div>
          ))}
        </div>
      </div>
      <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-[12px] text-muted">
        <span className="tabular min-h-5 text-ink-2">
          {hover ? `${WEEKDAYS[hover.d]}, ${hourLabel(hover.h)}–${hourLabel((hover.h + 1) % 24)}: ${grid[hover.d][hover.h]} commits` : 'Hover a cell for its count'}
        </span>
        <span className="inline-flex items-center gap-1">
          Fewer {SEQ.map((c) => <span key={c} className="inline-block h-3 w-3 rounded-[2px]" style={{ background: c }} />)} More
        </span>
      </div>
    </div>
  )
}

/** Single-series history line. */
export function HistoryChart<T extends { date: string }>({ points, dataKey, label, format = num, domain }: {
  points: T[]
  dataKey: keyof T & string
  label: string
  format?: (n: number) => string
  domain?: [number, number]
}) {
  const tip = useRechartsTip()
  if (points.length < 2) {
    return <p className="py-6 text-center text-[13px] text-muted">The trend appears after two or more scans. Scans run daily.</p>
  }
  return (
    <div className="h-48">
      <ResponsiveContainer>
        <LineChart data={points} margin={{ top: 8, right: 8, bottom: 0, left: -16 }}>
          <CartesianGrid vertical={false} />
          <XAxis dataKey="date" tickFormatter={(d) => shortDate(String(d))} tickLine={false} axisLine={{ stroke: 'var(--axis)' }} minTickGap={40} />
          <YAxis domain={domain ?? ['auto', 'auto']} tickLine={false} axisLine={false} tickFormatter={(v) => format(Number(v))} />
          <Tooltip {...tip} cursor={{ stroke: 'var(--axis)', strokeWidth: 1 }}
            content={({ active, payload }) => active && payload?.length ? (
              <TipBox title={shortDate(String(payload[0].payload.date))} rows={[{ label, value: format(Number(payload[0].value)), color: 'var(--series-1)' }]} />
            ) : null} />
          <Line type="monotone" dataKey={dataKey} stroke="var(--series-1)" strokeWidth={2} dot={false}
            activeDot={{ r: 4, stroke: 'var(--surface-1)', strokeWidth: 2 }} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}

export function LanguageTable({ shares }: { shares: LanguageShare[] }) {
  return (
    <table className="w-full text-[13px]">
      <thead className="text-[12px] text-muted"><tr><th className="py-1 text-left font-medium">Language</th><th className="text-right font-medium">Share</th><th className="text-right font-medium">Bytes</th><th className="text-right font-medium">Repos</th></tr></thead>
      <tbody className="tabular text-ink-2">
        {shares.map((s) => (
          <tr key={s.language} className="border-t border-line">
            <td className="py-1">{s.language}</td><td className="text-right">{pct(s.percent)}</td><td className="text-right">{bytes(s.bytes)}</td><td className="text-right">{s.repos}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
