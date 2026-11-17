export const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
export const WEEKDAYS_SHORT = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

const compact = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 })
const full = new Intl.NumberFormat()

/** 1,284 / 12.9K — full below 10,000. */
export const num = (n: number) => (Math.abs(n) < 10000 ? full.format(n) : compact.format(n))

export function bytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ['KB', 'MB', 'GB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`
}

export const pct = (p: number) => `${p < 10 ? p.toFixed(1) : Math.round(p)}%`

export function ago(iso: string | null | undefined): string {
  if (!iso) return 'never'
  const days = Math.floor((Date.now() - new Date(iso).getTime()) / 86_400_000)
  if (days < 1) return 'today'
  if (days === 1) return 'yesterday'
  if (days < 30) return `${days} days ago`
  if (days < 365) return `${Math.floor(days / 30)} mo ago`
  const y = Math.floor(days / 365)
  return `${y} yr${y > 1 ? 's' : ''} ago`
}

export const date = (iso: string) =>
  new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })

export const dateTime = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })

export const shortDate = (iso: string) =>
  new Date(iso + (iso.length === 10 ? 'T00:00:00Z' : '')).toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' })

export function hourLabel(h: number): string {
  const suffix = h < 12 ? 'am' : 'pm'
  const v = h % 12 === 0 ? 12 : h % 12
  return `${v}${suffix}`
}
