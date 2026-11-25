// API client and response types (mirroring backend/internal/stats and scanner).

import { demoApi, isDemoMode } from './demo'

// Empty means same origin (Vite dev proxy locally, Render rewrite in production).
// Set VITE_API_BASE_URL only for the cross-origin fallback setup.
export const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '')

export class AuthError extends Error {}
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  if (isDemoMode()) return demoApi<T>(path, init)
  const res = await fetch(API_BASE + path, {
    credentials: 'include',
    ...init,
    headers: { Accept: 'application/json', ...(init?.body ? { 'Content-Type': 'application/json' } : {}), ...init?.headers },
  })
  if (res.status === 401) throw new AuthError('not signed in')
  if (res.status === 204) return undefined as T
  const body = await res.json().catch(() => null)
  if (!res.ok) throw new ApiError(res.status, body?.error ?? `HTTP ${res.status}`)
  return body as T
}

export const loginURL = () => API_BASE + '/api/auth/login'

export type NoData = { noData: true }
export const isNoData = (v: unknown): v is NoData =>
  typeof v === 'object' && v !== null && (v as NoData).noData === true

export interface Me {
  id: number
  login: string
  name: string
  avatarUrl: string
}

export interface Scan {
  id: number
  userId: number
  trigger: 'cron' | 'ui'
  status: 'running' | 'succeeded' | 'failed'
  phase: string
  reposTotal: number
  reposDone: number
  apiCalls: number
  rateRemaining: number | null
  error?: string
  startedAt: string
  finishedAt: string | null
}

export interface ScanRef {
  id: number
  startedAt: string
  finishedAt: string | null
}

export type Status = 'active' | 'slowing' | 'dormant'
export type Direction = 'improving' | 'steady' | 'slipping'

export interface DeltaSet {
  healthScore: number
  openPrs: number
  staleBranches: number
  failedRuns: number
  commitCadence: number
  uniqueAuthors: number
}

export interface ActionItem {
  id: number
  repoId: number
  repoName: string
  rule: string
  severity: number
  title: string
  detail: Record<string, unknown>
  url: string
  active: boolean
  dismissed: boolean
  createdAt: string
  updatedAt: string
}

export interface TrendSnapshot {
  repoId: number
  scanId: number
  scannedAt: string
  healthScore: number
  openPrs: number
  openIssues: number
  staleBranches: number
  failedRuns: number
  commitCount7d: number
  commitCount30d: number
  uniqueAuthors30d: number
  topAuthorLogin?: string
  topAuthorPct: number
  stars: number
  forks: number
}

export interface RepoTrendDetail {
  id: number
  name: string
  fullName: string
  direction: Direction
  delta7d?: DeltaSet
  delta30d?: DeltaSet
  alerts: ActionItem[]
  history: TrendSnapshot[]
}

export interface WatchedRepo {
  id: number
  userId: number
  owner: string
  name: string
  fullName: string
  githubId?: number
  addedAt: string
  lastError?: string
}

export interface Anomaly {
  metric: string
  zScore: number
  current: number
  mean: number
  stdDev: number
  summary: string
}

export interface RepoInsights {
  repoId: number
  fullName: string
  source: 'owned' | 'watched'
  healthScore: number
  sampleCount: number
  healthSlopePerDay: number
  projectedHealth30d: number
  openPrs: number
  prMedianAgeDays: number
  prP90AgeDays: number
  prStalePct: number
  responsiveness: 'none' | 'fast' | 'moderate' | 'slow'
  commitCount7d: number
  commitCount30d: number
  uniqueAuthors30d: number
  topAuthorLogin?: string
  topAuthorPct: number
  diversityScore: number
  diversityLabel: 'inactive' | 'concentrated' | 'moderate' | 'healthy'
  contributorReadiness: number
  readinessVerdict: string
  anomalies: Anomaly[]
}

export type AdvisorRole = 'maintainer' | 'contributor'
export type AdvisorProvider = 'deterministic' | 'gemini' | 'openai' | 'anthropic'

export interface AskRequest {
  role: AdvisorRole
  repoId?: number
  question: string
  provider?: AdvisorProvider
  model?: string
  apiKey?: string
}

export interface AskResponse {
  answer: string
  mode: string
  model?: string
  role: AdvisorRole
  links: { label: string; url: string }[]
  warning?: string
}

export interface HealthCheck {
  key: string
  label: string
  points: number
  max: number
  detail: string
}

export interface RepoRow {
  id: number
  name: string
  fullName: string
  source?: 'owned' | 'watched'
  description: string
  visibility: 'public' | 'private' | 'internal'
  isFork: boolean
  isArchived: boolean
  stars: number
  forks: number
  sizeKb: number
  defaultBranch: string
  topics: string[]
  license: string
  createdAt: string
  pushedAt: string | null
  hasReadme: boolean
  hasLicense: boolean
  hasGitignore: boolean
  hasCi: boolean
  hasTests: boolean
  openPrs: number
  openIssues: number
  healthScore: number
  healthBreakdown: HealthCheck[]
  status: Status
  primaryLanguage: string
  excluded: string
  direction?: Direction
  trendDelta7d?: DeltaSet
  trendDelta30d?: DeltaSet
  commitCount7d?: number
  commitCount30d?: number
  uniqueAuthors30d?: number
  topAuthorLogin?: string
  topAuthorPct?: number
  staleBranches?: number
  failedRuns?: number
}

export interface HistoryPoint {
  scanId: number
  date: string
  repos: number
  stars: number
  forks: number
  avgHealth: number
}

export interface Overview {
  scan: ScanRef
  prevScan: ScanRef | null
  totalRepos: number
  includedRepos: number
  forkRepos: number
  archivedRepos: number
  privateRepos: number
  stars: number
  forks: number
  openPrs: number
  openIssues: number
  avgHealth: number
  statusCounts: Record<Status, number>
  trendSummary?: Record<Direction, number>
  history: HistoryPoint[]
}

export interface LanguageShare {
  language: string
  bytes: number
  percent: number
  repos: number
}

export interface YearShare {
  year: number
  repos: number
  percents: Record<string, number>
}

export interface WeekPoint {
  week: string
  all: number
  mine: number
}

export interface CommitStats {
  total: number
  byWeekday: number[]
  byHour: number[]
  heatmap: number[][]
  longestStreak: { days: number; start?: string; end?: string }
  currentStreak: number
  activeDays: number
  busiestWeekday: number
  busiestHour: number
}

export interface TopRepo {
  id: number
  name: string
  value: number
}

export interface TechUse {
  name: string
  category: string
  repos: string[]
}

export interface Analytics {
  scan: ScanRef
  timezone: string
  includedRepos: number
  languages: LanguageShare[]
  perRepo: { repoId: number; name: string; languages: LanguageShare[] }[]
  trend: YearShare[]
  trendLanguages: string[]
  weekly: WeekPoint[]
  commits: CommitStats
  topByStars: TopRepo[]
  topByForks: TopRepo[]
  topByCommits: TopRepo[]
  tech: TechUse[]
}

export interface ItemInfo {
  kind: 'pr' | 'issue'
  number: number
  title: string
  author: string
  url: string
  isDraft: boolean
  reviewRequested: boolean
  assignedToMe: boolean
  createdAt: string
  ageDays: number
  repo?: string
}

export interface RunInfo {
  repo?: string
  workflow: string
  branch: string
  url: string
  createdAt: string
}

export interface RepoDetail {
  scan: ScanRef
  repo: RepoRow
  languages: LanguageShare[]
  branches: { name: string; lastCommitAt: string | null; isDefault: boolean; stale: boolean }[]
  items: ItemInfo[]
  tech: TechUse[]
  failedRuns: RunInfo[]
  history: { date: string; health: number; stars: number; forks: number }[]
  weekly: WeekPoint[]
  trend?: RepoTrendDetail
}

export interface Digest {
  scan: ScanRef
  since: string
  sincePrevScan: boolean
  waitingOnMe: ItemInfo[]
  newIssues: ItemInfo[]
  failedRuns: RunInfo[]
  newStars: { repo: string; actor: string; at: string }[]
  newForks: { repo: string; actor: string; at: string }[]
  deltas: { repo: string; stars: number; forks: number }[]
  whatToFixNext?: ActionItem[]
}

export interface Settings {
  excludedRepoIds: number[]
  excludedRepoGlobs: string[]
  includeForks: boolean
  includeArchived: boolean
  timezone: string
}

export interface AppStatus {
  installed: boolean
  appName?: string
  installUrl?: string
  configureUrl?: string
  installationId?: number
  repositorySelection?: 'all' | 'selected'
  suspended?: boolean
  /** Read permissions scans need but the installation lacks, e.g. "contents:read". */
  missingPermissions?: string[]
}

export type PRMode = 'off' | 'dashboard' | 'comment'

export interface RepoSetting {
  repoId: number
  fullName: string
  source?: 'owned' | 'watched'
  visibility: string
  isArchived: boolean
  isFork: boolean
  prMode: PRMode
  releasesEnabled: boolean
}

export interface ReposResponse {
  scan: ScanRef
  repos: RepoRow[]
}

export interface FeatureUsage {
  feature: string
  calls: number
  cacheHits: number
  skipped: number
  failed: number
  costUsd: number
}

export interface LLMUsage {
  enabled: boolean
  disabledReason?: string
  provider?: string
  model?: string
  month: string
  budgetUsd: number
  spentUsd: number
  capped: boolean
  byFeature: FeatureUsage[]
}

export interface PRFlag {
  kind: string
  title: string
  files: string[]
  extra: number
}

export interface PRReviewResult {
  headSha: string
  filesChanged: number
  filesListed: number
  additions: number
  deletions: number
  flags: PRFlag[]
  omitted: { path: string; reason: string }[]
  suggestedLabels: string[]
  labelReasons: Record<string, string>
  ai?: { summary: string; notable: string[] | null; suggestedTitle: string; suggestedLabels: string[] | null }
}

export interface PRReview {
  id: number
  repoId: number
  repo: string
  number: number
  headSha: string
  title: string
  author: string
  url: string
  source: 'webhook' | 'sync' | 'manual'
  status: 'pending' | 'running' | 'done' | 'skipped' | 'failed'
  mode?: string
  aiStatus?: string
  skipReason?: string
  error?: string
  commentUrl?: string
  result?: PRReviewResult | null
  createdAt: string
  updatedAt: string
}

export interface ReleaseItem {
  number: number
  title: string
  url: string
  author: string
  labels: string[] | null
  breaking: boolean
  summary?: string
  why: string
}

export interface ReleasePreview {
  repoId: number
  repo: string
  defaultBranch: string
  baseTag: string
  baseSource: 'release' | 'tag' | ''
  baseDate?: string
  suggestedVersion: string
  bump: 'major' | 'minor' | 'patch' | 'initial'
  groups: { key: string; title: string; items: ReleaseItem[] }[]
  count: number
  truncated: boolean
  intro?: string
  markdown: string
  compareUrl?: string
  aiStatus: string
  releasesEnabled: boolean
}

export interface ReleaseDraft {
  id: number
  repo: string
  tag: string
  baseTag?: string
  url: string
  source: 'ui' | 'mcp'
  createdAt: string
}

export interface ProfileSettings {
  enabled: boolean
  feedUrl: string
}

export interface ProfileRun {
  outcome: 'updated' | 'unchanged' | 'no_markers' | 'disabled' | 'failed'
  commitUrl?: string
  error?: string
  at: string
}

export interface ProfilePreview {
  repo: string
  enabled: boolean
  feedUrl: string
  readmePath?: string
  markerError?: string
  current: string
  proposed: string
  changed: boolean
  feedError?: string
  articles: number
  lastRun?: ProfileRun
}
