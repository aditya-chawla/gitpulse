// Categorical colour assignment. Colour follows the entity, never its rank on
// a given chart: languages get slots from the *overall* language order once,
// and every chart reuses that mapping. Past 8, everything folds into Other.

/** Meter fill carries severity: accent → warning → critical. */
export function scoreColor(score: number) {
  if (score >= 70) return 'var(--accent)'
  if (score >= 40) return 'var(--status-warning)'
  return 'var(--status-critical)'
}

export const SLOTS = 8
export const OTHER = 'Other'

export const seriesVar = (slot: number) => `var(--series-${slot + 1})`
export const OTHER_COLOR = 'var(--series-other)'

export function languageColors(overallOrder: string[]): (language: string) => string {
  const map = new Map<string, string>()
  overallOrder.slice(0, SLOTS).forEach((l, i) => map.set(l, seriesVar(i)))
  return (language) => map.get(language) ?? OTHER_COLOR
}

/** Keeps the first `SLOTS` languages (by the overall order) and folds the rest. */
export function foldLanguages<T extends { language: string; bytes: number; percent: number }>(
  shares: T[],
  overallOrder: string[],
): { language: string; bytes: number; percent: number }[] {
  const keep = new Set(overallOrder.slice(0, SLOTS))
  const out: { language: string; bytes: number; percent: number }[] = []
  let other = { language: OTHER, bytes: 0, percent: 0 }
  for (const s of shares) {
    if (keep.has(s.language)) out.push({ language: s.language, bytes: s.bytes, percent: s.percent })
    else other = { language: OTHER, bytes: other.bytes + s.bytes, percent: other.percent + s.percent }
  }
  if (other.bytes > 0) out.push(other)
  return out
}
