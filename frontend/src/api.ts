// API client and response types (mirroring backend/internal/stats and scanner).

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
