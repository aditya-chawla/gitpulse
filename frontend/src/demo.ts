import {
  ApiError,
  type ActionItem,
  type Analytics,
  type AppStatus,
  type AskRequest,
  type AskResponse,
  type Digest,
  type LLMUsage,
  type Me,
  type Overview,
  type PRReview,
  type ProfilePreview,
  type ProfileSettings,
  type RepoDetail,
  type RepoInsights,
  type RepoRow,
  type RepoSetting,
  type Scan,
  type Settings,
  type WatchedRepo,
} from './api'

const DEMO_STORAGE_KEY = 'gitpulse-demo'

export function isDemoMode(): boolean {
  try {
    const params = new URLSearchParams(window.location.search)
    if (params.get('demo') === '1') {
      sessionStorage.setItem(DEMO_STORAGE_KEY, '1')
      return true
    }
    return sessionStorage.getItem(DEMO_STORAGE_KEY) === '1'
  } catch {
    return false
  }
}

export function enterDemoMode(): void {
  try {
    sessionStorage.setItem(DEMO_STORAGE_KEY, '1')
  } catch {
    /* ignore */
  }
}

export function exitDemoMode(): void {
  try {
    sessionStorage.removeItem(DEMO_STORAGE_KEY)
  } catch {
    /* ignore */
  }
}

const now = new Date()
const daysAgo = (d: number) => new Date(now.getTime() - d * 86400_000).toISOString()

const demoUser: Me = {
  id: 1,
  login: 'demo-maintainer',
  name: 'Demo User',
  avatarUrl: 'https://github.com/ghost.png',
}

const demoScan: Scan = {
  id: 42,
  userId: 1,
  trigger: 'ui',
  status: 'succeeded',
  phase: 'done',
  reposTotal: 6,
  reposDone: 6,
  apiCalls: 34,
  rateRemaining: 4966,
  startedAt: daysAgo(0.1),
  finishedAt: daysAgo(0.09),
}

const demoRepos: RepoRow[] = [
  {
    id: 101,
    name: 'gitpulse',
    fullName: 'demo-maintainer/gitpulse',
    source: 'owned',
    description: 'Repository intelligence, Postgres Data Science insights, and maintenance advisor.',
    visibility: 'public',
    isFork: false,
    isArchived: false,
    stars: 142,
    forks: 19,
    sizeKb: 1840,
    defaultBranch: 'main',
    topics: ['go', 'react', 'postgres', 'mcp'],
    license: 'MIT',
    createdAt: daysAgo(180),
    pushedAt: daysAgo(0.4),
    hasReadme: true,
    hasLicense: true,
    hasGitignore: true,
    hasCi: true,
    hasTests: true,
    openPrs: 3,
    openIssues: 7,
    healthScore: 91,
    healthBreakdown: [
      { key: 'readme', label: 'README', points: 15, max: 15, detail: 'README present' },
      { key: 'license', label: 'License', points: 10, max: 10, detail: 'MIT' },
      { key: 'ci', label: 'CI', points: 15, max: 15, detail: 'GitHub Actions' },
      { key: 'tests', label: 'Tests', points: 15, max: 15, detail: 'Go + Vitest' },
      { key: 'activity', label: 'Activity', points: 20, max: 20, detail: 'Active this week' },
    ],
    status: 'active',
    primaryLanguage: 'Go',
    excluded: '',
    direction: 'improving',
    trendDelta7d: { healthScore: 4, openPrs: -1, staleBranches: 0, failedRuns: 0, commitCadence: 3, uniqueAuthors: 1 },
    commitCount7d: 14,
    commitCount30d: 48,
    uniqueAuthors30d: 3,
    topAuthorLogin: 'demo-maintainer',
    topAuthorPct: 68,
    staleBranches: 0,
    failedRuns: 0,
  },
  {
    id: 102,
    name: 'py-etl-kit',
    fullName: 'demo-maintainer/py-etl-kit',
    source: 'owned',
    description: 'Lightweight DuckDB & Postgres incremental pipeline helpers.',
    visibility: 'public',
    isFork: false,
    isArchived: false,
    stars: 58,
    forks: 9,
    sizeKb: 620,
    defaultBranch: 'main',
    topics: ['python', 'etl', 'postgres'],
    license: 'Apache-2.0',
    createdAt: daysAgo(320),
    pushedAt: daysAgo(9),
    hasReadme: true,
    hasLicense: true,
    hasGitignore: true,
    hasCi: true,
    hasTests: true,
    openPrs: 6,
    openIssues: 11,
    healthScore: 68,
    healthBreakdown: [
      { key: 'readme', label: 'README', points: 15, max: 15, detail: 'README present' },
      { key: 'license', label: 'License', points: 10, max: 10, detail: 'Apache-2.0' },
      { key: 'ci', label: 'CI', points: 8, max: 15, detail: '1 failing workflow on main' },
      { key: 'tests', label: 'Tests', points: 15, max: 15, detail: 'pytest' },
      { key: 'prs', label: 'PR backlog', points: 5, max: 15, detail: '4 PRs >14d untouched' },
    ],
    status: 'slowing',
    primaryLanguage: 'Python',
    excluded: '',
    direction: 'slipping',
    trendDelta7d: { healthScore: -7, openPrs: 3, staleBranches: 1, failedRuns: 2, commitCadence: -4, uniqueAuthors: 0 },
    commitCount7d: 2,
    commitCount30d: 11,
    uniqueAuthors30d: 2,
    topAuthorLogin: 'demo-maintainer',
    topAuthorPct: 82,
    staleBranches: 3,
    failedRuns: 2,
  },
  {
    id: 103,
    name: 'ts-ui-tokens',
    fullName: 'demo-maintainer/ts-ui-tokens',
    source: 'owned',
    description: 'Zero-runtime Tailwind v4 design tokens.',
    visibility: 'public',
    isFork: false,
    isArchived: false,
    stars: 31,
    forks: 4,
    sizeKb: 210,
    defaultBranch: 'main',
    topics: ['typescript', 'tailwind'],
    license: 'MIT',
    createdAt: daysAgo(95),
    pushedAt: daysAgo(2),
    hasReadme: true,
    hasLicense: true,
    hasGitignore: true,
    hasCi: true,
    hasTests: true,
    openPrs: 1,
    openIssues: 2,
    healthScore: 86,
    healthBreakdown: [
      { key: 'readme', label: 'README', points: 15, max: 15, detail: 'README present' },
      { key: 'license', label: 'License', points: 10, max: 10, detail: 'MIT' },
      { key: 'ci', label: 'CI', points: 15, max: 15, detail: 'GitHub Actions' },
    ],
    status: 'active',
    primaryLanguage: 'TypeScript',
    excluded: '',
    direction: 'steady',
    trendDelta7d: { healthScore: 0, openPrs: 0, staleBranches: 0, failedRuns: 0, commitCadence: 1, uniqueAuthors: 0 },
    commitCount7d: 5,
    commitCount30d: 18,
    uniqueAuthors30d: 2,
    topAuthorLogin: 'demo-maintainer',
    topAuthorPct: 75,
    staleBranches: 0,
    failedRuns: 0,
  },
  {
    id: 201,
    name: 'vite',
    fullName: 'vitejs/vite',
    source: 'watched',
    description: 'Next generation frontend tooling. It’s fast!',
    visibility: 'public',
    isFork: false,
    isArchived: false,
    stars: 72400,
    forks: 6680,
    sizeKb: 48200,
    defaultBranch: 'main',
    topics: ['frontend', 'vite', 'typescript', 'bundler'],
    license: 'MIT',
    createdAt: daysAgo(1600),
    pushedAt: daysAgo(0.1),
    hasReadme: true,
    hasLicense: true,
    hasGitignore: true,
    hasCi: true,
    hasTests: true,
    openPrs: 42,
    openIssues: 390,
    healthScore: 92,
    healthBreakdown: [
      { key: 'readme', label: 'README', points: 15, max: 15, detail: 'README present' },
      { key: 'ci', label: 'CI', points: 15, max: 15, detail: 'CI passing' },
      { key: 'tests', label: 'Tests', points: 15, max: 15, detail: 'Vitest + Playwright' },
    ],
    status: 'active',
    primaryLanguage: 'TypeScript',
    excluded: '',
    direction: 'improving',
    trendDelta7d: { healthScore: 2, openPrs: -4, staleBranches: 0, failedRuns: 0, commitCadence: 6, uniqueAuthors: 3 },
    commitCount7d: 34,
    commitCount30d: 128,
    uniqueAuthors30d: 26,
    topAuthorLogin: 'patak-dev',
    topAuthorPct: 18,
    staleBranches: 1,
    failedRuns: 0,
  },
  {
    id: 202,
    name: 'moto',
    fullName: 'getmoto/moto',
    source: 'watched',
    description: 'A library that allows you to easily mock out tests based on AWS infrastructure.',
    visibility: 'public',
    isFork: false,
    isArchived: false,
    stars: 8420,
    forks: 2150,
    sizeKb: 64100,
    defaultBranch: 'master',
    topics: ['aws', 'python', 'mocking', 'testing'],
    license: 'Apache-2.0',
    createdAt: daysAgo(3200),
    pushedAt: daysAgo(0.5),
    hasReadme: true,
    hasLicense: true,
    hasGitignore: true,
    hasCi: true,
    hasTests: true,
    openPrs: 77,
    openIssues: 510,
    healthScore: 71,
    healthBreakdown: [
      { key: 'readme', label: 'README', points: 15, max: 15, detail: 'README present' },
      { key: 'ci', label: 'CI', points: 15, max: 15, detail: 'GitHub Actions' },
      { key: 'prs', label: 'PR backlog', points: 2, max: 15, detail: '77 open PRs >14d untouched' },
    ],
    status: 'active',
    primaryLanguage: 'Python',
    excluded: '',
    direction: 'slipping',
    trendDelta7d: { healthScore: -4, openPrs: 14, staleBranches: 2, failedRuns: 0, commitCadence: 1, uniqueAuthors: 0 },
    commitCount7d: 21,
    commitCount30d: 89,
    uniqueAuthors30d: 19,
    topAuthorLogin: 'bblommers',
    topAuthorPct: 34,
    staleBranches: 7,
    failedRuns: 0,
  },
  {
    id: 203,
    name: 'tinygrad-notes',
    fullName: 'example-org/tiny-cli',
    source: 'watched',
    description: 'Minimalist CLI utility with single-maintainer history.',
    visibility: 'public',
    isFork: false,
    isArchived: false,
    stars: 610,
    forks: 44,
    sizeKb: 920,
    defaultBranch: 'main',
    topics: ['go', 'cli'],
    license: 'MIT',
    createdAt: daysAgo(700),
    pushedAt: daysAgo(45),
    hasReadme: true,
    hasLicense: true,
    hasGitignore: true,
    hasCi: false,
    hasTests: true,
    openPrs: 9,
    openIssues: 24,
    healthScore: 49,
    healthBreakdown: [
      { key: 'readme', label: 'README', points: 15, max: 15, detail: 'README present' },
      { key: 'ci', label: 'CI', points: 0, max: 15, detail: 'No CI detected' },
    ],
    status: 'slowing',
    primaryLanguage: 'Go',
    excluded: '',
    direction: 'slipping',
    trendDelta7d: { healthScore: -5, openPrs: 1, staleBranches: 0, failedRuns: 0, commitCadence: -3, uniqueAuthors: -1 },
    commitCount7d: 0,
    commitCount30d: 3,
    uniqueAuthors30d: 1,
    topAuthorLogin: 'solo-author',
    topAuthorPct: 100,
    staleBranches: 4,
    failedRuns: 0,
  },
]

const demoInsights: RepoInsights[] = [
  {
    repoId: 101,
    fullName: 'demo-maintainer/gitpulse',
    source: 'owned',
    healthScore: 91,
    sampleCount: 12,
    healthSlopePerDay: 0.22,
    projectedHealth30d: 98,
    openPrs: 3,
    prMedianAgeDays: 2.1,
    prP90AgeDays: 4.8,
    prStalePct: 0,
    responsiveness: 'fast',
    commitCount7d: 14,
    commitCount30d: 48,
    uniqueAuthors30d: 3,
    topAuthorLogin: 'demo-maintainer',
    topAuthorPct: 68,
    diversityScore: 0.38,
    diversityLabel: 'moderate',
    contributorReadiness: 83,
    readinessVerdict: 'Welcoming',
    anomalies: [],
  },
  {
    repoId: 102,
    fullName: 'demo-maintainer/py-etl-kit',
    source: 'owned',
    healthScore: 68,
    sampleCount: 12,
    healthSlopePerDay: -0.45,
    projectedHealth30d: 55,
    openPrs: 6,
    prMedianAgeDays: 16.4,
    prP90AgeDays: 29.0,
    prStalePct: 67,
    responsiveness: 'slow',
    commitCount7d: 2,
    commitCount30d: 11,
    uniqueAuthors30d: 2,
    topAuthorLogin: 'demo-maintainer',
    topAuthorPct: 82,
    diversityScore: 0.23,
    diversityLabel: 'concentrated',
    contributorReadiness: 51,
    readinessVerdict: 'Caution',
    anomalies: [
      { metric: 'failed_runs', zScore: 2.1, current: 2, mean: 0.3, stdDev: 0.8, summary: 'Failed CI runs is +2.1σ above 30d baseline (2 vs avg 0.3)' },
    ],
  },
  {
    repoId: 103,
    fullName: 'demo-maintainer/ts-ui-tokens',
    source: 'owned',
    healthScore: 86,
    sampleCount: 10,
    healthSlopePerDay: 0.05,
    projectedHealth30d: 88,
    openPrs: 1,
    prMedianAgeDays: 1.5,
    prP90AgeDays: 1.5,
    prStalePct: 0,
    responsiveness: 'fast',
    commitCount7d: 5,
    commitCount30d: 18,
    uniqueAuthors30d: 2,
    topAuthorLogin: 'demo-maintainer',
    topAuthorPct: 75,
    diversityScore: 0.27,
    diversityLabel: 'concentrated',
    contributorReadiness: 74,
    readinessVerdict: 'Steady',
    anomalies: [],
  },
  {
    repoId: 201,
    fullName: 'vitejs/vite',
    source: 'watched',
    healthScore: 92,
    sampleCount: 14,
    healthSlopePerDay: 0.14,
    projectedHealth30d: 96,
    openPrs: 42,
    prMedianAgeDays: 3.4,
    prP90AgeDays: 11.2,
    prStalePct: 12,
    responsiveness: 'fast',
    commitCount7d: 34,
    commitCount30d: 128,
    uniqueAuthors30d: 26,
    topAuthorLogin: 'patak-dev',
    topAuthorPct: 18,
    diversityScore: 0.89,
    diversityLabel: 'healthy',
    contributorReadiness: 95,
    readinessVerdict: 'Welcoming',
    anomalies: [],
  },
  {
    repoId: 202,
    fullName: 'getmoto/moto',
    source: 'watched',
    healthScore: 71,
    sampleCount: 14,
    healthSlopePerDay: -0.28,
    projectedHealth30d: 63,
    openPrs: 77,
    prMedianAgeDays: 21.5,
    prP90AgeDays: 58.0,
    prStalePct: 64,
    responsiveness: 'slow',
    commitCount7d: 21,
    commitCount30d: 89,
    uniqueAuthors30d: 19,
    topAuthorLogin: 'bblommers',
    topAuthorPct: 34,
    diversityScore: 0.80,
    diversityLabel: 'healthy',
    contributorReadiness: 74,
    readinessVerdict: 'Active but backlogged',
    anomalies: [
      { metric: 'open_prs', zScore: 2.4, current: 77, mean: 48.0, stdDev: 12.1, summary: 'Open PRs is +2.4σ above 30d baseline (77 vs avg 48.0)' },
    ],
  },
  {
    repoId: 203,
    fullName: 'example-org/tiny-cli',
    source: 'watched',
    healthScore: 49,
    sampleCount: 8,
    healthSlopePerDay: -0.35,
    projectedHealth30d: 39,
    openPrs: 9,
    prMedianAgeDays: 38.0,
    prP90AgeDays: 82.0,
    prStalePct: 89,
    responsiveness: 'slow',
    commitCount7d: 0,
    commitCount30d: 3,
    uniqueAuthors30d: 1,
    topAuthorLogin: 'solo-author',
    topAuthorPct: 100,
    diversityScore: 0,
    diversityLabel: 'concentrated',
    contributorReadiness: 27,
    readinessVerdict: 'Caution',
    anomalies: [],
  },
]

const demoActions: (ActionItem & { source: 'owned' | 'watched' })[] = [
  {
    id: 1,
    repoId: 102,
    repoName: 'demo-maintainer/py-etl-kit',
    source: 'owned',
    rule: 'ci_rot',
    severity: 5,
    title: 'CI workflow "pytest" failed across consecutive scans on main',
    detail: {},
    url: 'https://github.com/demo-maintainer/py-etl-kit/actions',
    active: true,
    dismissed: false,
    createdAt: daysAgo(2),
    updatedAt: daysAgo(0.1),
  },
  {
    id: 2,
    repoId: 102,
    repoName: 'demo-maintainer/py-etl-kit',
    source: 'owned',
    rule: 'pr_bottleneck',
    severity: 4,
    title: '4 open PRs untouched >14 days on py-etl-kit',
    detail: {},
    url: 'https://github.com/demo-maintainer/py-etl-kit/pulls',
    active: true,
    dismissed: false,
    createdAt: daysAgo(4),
    updatedAt: daysAgo(0.1),
  },
  {
    id: 3,
    repoId: 102,
    repoName: 'demo-maintainer/py-etl-kit',
    source: 'owned',
    rule: 'stale_branches',
    severity: 3,
    title: '3 stale branches (>90 days) on py-etl-kit',
    detail: {},
    url: 'https://github.com/demo-maintainer/py-etl-kit/branches',
    active: true,
    dismissed: false,
    createdAt: daysAgo(5),
    updatedAt: daysAgo(0.1),
  },
  {
    id: 4,
    repoId: 202,
    repoName: 'getmoto/moto',
    source: 'watched',
    rule: 'pr_bottleneck',
    severity: 4,
    title: '77 open PRs untouched >14 days on getmoto/moto',
    detail: {},
    url: 'https://github.com/getmoto/moto/pulls',
    active: true,
    dismissed: false,
    createdAt: daysAgo(3),
    updatedAt: daysAgo(0.1),
  },
  {
    id: 5,
    repoId: 203,
    repoName: 'example-org/tiny-cli',
    source: 'watched',
    rule: 'bus_factor',
    severity: 3,
    title: 'Single author accounts for 100% of 30d commits',
    detail: {},
    url: 'https://github.com/example-org/tiny-cli',
    active: true,
    dismissed: false,
    createdAt: daysAgo(7),
    updatedAt: daysAgo(0.1),
  },
]

function buildWeeks(): { week: string; mine: number; all: number }[] {
  const out: { week: string; mine: number; all: number }[] = []
  for (let i = 11; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 7 * 86400_000)
    out.push({
      week: d.toISOString().slice(0, 10),
      mine: 6 + ((i * 3) % 11),
      all: 10 + ((i * 5) % 15),
    })
  }
  return out
}

function buildHeatmap(): number[][] {
  const g: number[][] = Array.from({ length: 7 }, () => Array.from({ length: 24 }, () => 0))
  for (let d = 0; d < 7; d++) {
    for (let h = 9; h <= 21; h++) {
      g[d][h] = ((d + h) % 4) + (h >= 14 && h <= 18 ? 3 : 0)
    }
  }
  return g
}

export async function demoApi<T>(path: string, init?: RequestInit): Promise<T> {
  const method = (init?.method ?? 'GET').toUpperCase()
  const [pathname, queryStr] = path.split('?')
  const params = new URLSearchParams(queryStr ?? '')

  if (pathname === '/api/auth/logout' && method === 'POST') {
    exitDemoMode()
    return undefined as T
  }

  if (pathname === '/api/agent/ask' && method === 'POST') {
    const req = JSON.parse(String(init?.body ?? '{}')) as AskRequest
    return buildDemoAskResponse(req) as T
  }

  if (method !== 'GET') {
    throw new ApiError(403, 'Disabled in read-only demo mode')
  }

  switch (pathname) {
    case '/api/me':
      return demoUser as T
    case '/api/app':
      return { installed: true, suspended: false, missingPermissions: [] } satisfies AppStatus as T
    case '/api/scans/latest':
      return demoScan as T
    case '/api/overview': {
      const ov: Overview = {
        scan: { id: demoScan.id, startedAt: demoScan.startedAt, finishedAt: demoScan.finishedAt },
        prevScan: { id: 41, startedAt: daysAgo(1), finishedAt: daysAgo(1) },
        totalRepos: 3,
        includedRepos: 3,
        forkRepos: 0,
        archivedRepos: 0,
        privateRepos: 0,
        stars: 231,
        forks: 32,
        openPrs: 10,
        openIssues: 20,
        avgHealth: 81.7,
        statusCounts: { active: 2, slowing: 1, dormant: 0 },
        trendSummary: { improving: 1, steady: 1, slipping: 1 },
        history: [5, 4, 3, 2, 1, 0].map((d, idx) => ({
          scanId: 37 + idx,
          date: daysAgo(d * 5),
          repos: 3,
          stars: 215 + idx * 3,
          forks: 28 + idx,
          avgHealth: 78 + idx * 0.7,
        })),
      }
      return ov as T
    }
    case '/api/analytics': {
      const an: Analytics = {
        scan: { id: demoScan.id, startedAt: demoScan.startedAt, finishedAt: demoScan.finishedAt },
        timezone: 'UTC',
        includedRepos: 3,
        languages: [
          { language: 'Go', bytes: 185000, percent: 52.1, repos: 1 },
          { language: 'TypeScript', bytes: 98000, percent: 27.6, repos: 2 },
          { language: 'Python', bytes: 54000, percent: 15.2, repos: 1 },
          { language: 'SQL', bytes: 18000, percent: 5.1, repos: 2 },
        ],
        perRepo: [
          { repoId: 101, name: 'gitpulse', languages: [{ language: 'Go', bytes: 185000, percent: 72, repos: 1 }, { language: 'TypeScript', bytes: 72000, percent: 28, repos: 1 }] },
          { repoId: 102, name: 'py-etl-kit', languages: [{ language: 'Python', bytes: 54000, percent: 75, repos: 1 }, { language: 'SQL', bytes: 18000, percent: 25, repos: 1 }] },
          { repoId: 103, name: 'ts-ui-tokens', languages: [{ language: 'TypeScript', bytes: 26000, percent: 100, repos: 1 }] },
        ],
        trend: [
          { year: 2025, repos: 2, percents: { Go: 45, Python: 35, TypeScript: 20 } },
          { year: 2026, repos: 3, percents: { Go: 52, TypeScript: 28, Python: 15 } },
        ],
        trendLanguages: ['Go', 'TypeScript', 'Python', 'SQL'],
        weekly: buildWeeks(),
        commits: {
          total: 248,
          activeDays: 94,
          longestStreak: { days: 19, start: daysAgo(45), end: daysAgo(26) },
          currentStreak: 6,
          busiestWeekday: 3,
          busiestHour: 16,
          byWeekday: [22, 38, 41, 49, 44, 31, 23],
          byHour: Array.from({ length: 24 }, (_, h) => (h >= 9 && h <= 21 ? 18 : 2)),
          heatmap: buildHeatmap(),
        },
        topByStars: demoRepos.filter((r) => r.source === 'owned').map((r) => ({ id: r.id, name: r.name, value: r.stars })),
        topByForks: demoRepos.filter((r) => r.source === 'owned').map((r) => ({ id: r.id, name: r.name, value: r.forks })),
        topByCommits: demoRepos.filter((r) => r.source === 'owned').map((r) => ({ id: r.id, name: r.name, value: r.commitCount30d ?? 10 })),
        tech: [
          { name: 'PostgreSQL', category: 'database', repos: ['gitpulse', 'py-etl-kit'] },
          { name: 'React', category: 'framework', repos: ['gitpulse'] },
          { name: 'Tailwind CSS', category: 'styling', repos: ['gitpulse', 'ts-ui-tokens'] },
        ],
      }
      return an as T
    }
    case '/api/repos': {
      const src = params.get('source')
      const repos = src ? demoRepos.filter((r) => r.source === src) : demoRepos
      return { scan: { id: demoScan.id, startedAt: demoScan.startedAt, finishedAt: demoScan.finishedAt }, repos } as T
    }
    case '/api/insights': {
      const src = params.get('source')
      const repos = src ? demoInsights.filter((r) => r.source === src) : demoInsights
      return { repos } as T
    }
    case '/api/actions': {
      const src = params.get('source') || 'owned'
      const items = demoActions.filter((a) => a.source === src)
      return { items } as T
    }
    case '/api/watchlist': {
      const repos: WatchedRepo[] = demoRepos
        .filter((r) => r.source === 'watched')
        .map((r, i) => ({
          id: i + 1,
          userId: 1,
          owner: r.fullName.split('/')[0],
          name: r.name,
          fullName: r.fullName,
          githubId: r.id,
          addedAt: daysAgo(14),
        }))
      return { repos } as T
    }
    case '/api/digest': {
      const d: Digest = {
        scan: { id: demoScan.id, startedAt: demoScan.startedAt, finishedAt: demoScan.finishedAt },
        since: daysAgo(1),
        sincePrevScan: true,
        waitingOnMe: [
          {
            kind: 'pr',
            number: 18,
            title: 'feat: add DuckDB arrow streaming helper',
            author: 'contributor-42',
            url: 'https://github.com/demo-maintainer/py-etl-kit/pull/18',
            isDraft: false,
            reviewRequested: true,
            assignedToMe: false,
            createdAt: daysAgo(16),
            ageDays: 16,
            repo: 'py-etl-kit',
          },
        ],
        newIssues: [
          {
            kind: 'issue',
            number: 44,
            title: 'Support custom Neon SSL mode hint in README',
            author: 'alice',
            url: 'https://github.com/demo-maintainer/gitpulse/issues/44',
            isDraft: false,
            reviewRequested: false,
            assignedToMe: false,
            createdAt: daysAgo(0.5),
            ageDays: 0,
            repo: 'gitpulse',
          },
        ],
        failedRuns: [
          {
            repo: 'py-etl-kit',
            workflow: 'pytest',
            branch: 'main',
            url: 'https://github.com/demo-maintainer/py-etl-kit/actions',
            createdAt: daysAgo(0.3),
          },
        ],
        newStars: [],
        newForks: [],
        deltas: [{ repo: 'gitpulse', stars: 4, forks: 1 }],
        whatToFixNext: demoActions.filter((a) => a.source === 'owned'),
      }
      return d as T
    }
    case '/api/prs': {
      const reviews: PRReview[] = [
        {
          id: 1,
          repoId: 101,
          repo: 'demo-maintainer/gitpulse',
          number: 24,
          headSha: 'a1b2c3d4',
          title: 'feat: Postgres 17 REGR_SLOPE insights',
          author: 'demo-maintainer',
          url: 'https://github.com/demo-maintainer/gitpulse/pull/24',
          source: 'sync',
          status: 'done',
          mode: 'dashboard',
          result: {
            headSha: 'a1b2c3d4',
            filesChanged: 6,
            filesListed: 6,
            additions: 310,
            deletions: 24,
            flags: [{ kind: 'sql', title: 'New SQL statistical queries', files: ['backend/internal/stats/insights.go'], extra: 0 }],
            omitted: [],
            suggestedLabels: ['enhancement'],
            labelReasons: { enhancement: 'feat: prefix' },
          },
          createdAt: daysAgo(1),
          updatedAt: daysAgo(0.2),
        },
      ]
      return { reviews, aiEnabled: false } as T
    }
    case '/api/releases/drafts':
      return { drafts: [] } as T
    case '/api/settings': {
      const st: Settings = {
        excludedRepoIds: [],
        excludedRepoGlobs: [],
        includeForks: false,
        includeArchived: false,
        timezone: 'UTC',
      }
      return st as T
    }
    case '/api/repo-settings': {
      const repos: RepoSetting[] = demoRepos
        .filter((r) => r.source === 'owned')
        .map((r) => ({
          repoId: r.id,
          name: r.name,
          fullName: r.fullName,
          visibility: r.visibility,
          isArchived: r.isArchived,
          isFork: r.isFork,
          source: 'owned',
          prMode: 'dashboard',
          releasesEnabled: true,
        }))
      return { repos } as T
    }
    case '/api/llm/usage': {
      const u: LLMUsage = {
        enabled: false,
        disabledReason: 'Demo mode uses free deterministic Postgres insights (BYOK supported in live app)',
        month: now.toISOString().slice(0, 7),
        budgetUsd: 0,
        spentUsd: 0,
        capped: false,
        byFeature: [],
      }
      return u as T
    }
    case '/api/profile-readme/settings': {
      const p: ProfileSettings = { enabled: false, feedUrl: '' }
      return p as T
    }
    case '/api/profile-readme/preview': {
      const p: ProfilePreview = {
        repo: 'demo-maintainer/demo-maintainer',
        enabled: false,
        feedUrl: '',
        current: '',
        proposed: '<!-- gitpulse:start -->\nDemo preview\n<!-- gitpulse:end -->',
        changed: false,
        articles: 0,
      }
      return p as T
    }
    default: {
      if (pathname.startsWith('/api/repos/')) {
        const id = Number(pathname.split('/').pop())
        const repo = demoRepos.find((r) => r.id === id)
        if (!repo) throw new ApiError(404, 'not found')
        const detail: RepoDetail = {
          scan: { id: demoScan.id, startedAt: demoScan.startedAt, finishedAt: demoScan.finishedAt },
          repo,
          languages: [{ language: repo.primaryLanguage || 'Go', bytes: 120000, percent: 100, repos: 1 }],
          branches: [
            { name: repo.defaultBranch, lastCommitAt: repo.pushedAt, isDefault: true, stale: false },
            ...(repo.staleBranches ? [{ name: 'wip/legacy-migration', lastCommitAt: daysAgo(120), isDefault: false, stale: true }] : []),
          ],
          items: demoActions
            .filter((a) => a.repoId === repo.id)
            .map((a, idx) => ({
              kind: 'pr',
              number: 10 + idx,
              title: a.title,
              author: 'contributor',
              url: a.url,
              isDraft: false,
              reviewRequested: repo.source === 'owned',
              assignedToMe: false,
              createdAt: daysAgo(18),
              ageDays: 18,
            })),
          tech: [{ name: repo.primaryLanguage || 'Go', category: 'language', repos: ['manifest'] }],
          failedRuns: repo.failedRuns
            ? [{ workflow: 'pytest', branch: repo.defaultBranch, url: `https://github.com/${repo.fullName}/actions`, createdAt: daysAgo(0.3) }]
            : [],
          history: [5, 4, 3, 2, 1, 0].map((d, i) => ({
            date: daysAgo(d * 5),
            health: Math.max(20, Math.min(100, repo.healthScore + (repo.direction === 'slipping' ? 6 - i : i - 4))),
            stars: Math.max(0, repo.stars - (5 - i) * 2),
            forks: repo.forks,
          })),
          weekly: buildWeeks(),
          trend: {
            id: repo.id,
            name: repo.name,
            fullName: repo.fullName,
            direction: repo.direction ?? 'steady',
            delta7d: repo.trendDelta7d,
            delta30d: repo.trendDelta7d,
            alerts: demoActions.filter((a) => a.repoId === repo.id),
            history: [],
          },
        }
        return detail as T
      }
      if (pathname.startsWith('/api/insights/')) {
        const id = Number(pathname.split('/').pop())
        const ins = demoInsights.find((r) => r.repoId === id)
        if (!ins) throw new ApiError(404, 'not found')
        return ins as T
      }
      throw new ApiError(404, 'not found')
    }
  }
}

function buildDemoAskResponse(req: AskRequest): AskResponse {
  const role = req.role === 'contributor' ? 'contributor' : 'maintainer'
  const ins = req.repoId ? demoInsights.find((r) => r.repoId === req.repoId) : undefined

  if (ins) {
    const anomalyNote = ins.anomalies.length
      ? `\n\n**Statistical Anomaly:**\n- ${ins.anomalies.map((a) => a.summary).join('\n- ')}`
      : ''
    const rec =
      role === 'contributor'
        ? ins.readinessVerdict === 'Welcoming'
          ? 'Strong candidate for contributing — active commits, high contributor diversity, and fast PR turnaround.'
          : ins.responsiveness === 'slow'
            ? 'Active codebase, but PR reviews are backlogged. Start with a small, well-scoped issue first.'
            : 'Single-maintainer heavy or low activity; open an issue before investing in a large PR.'
        : ins.anomalies.length > 0
          ? 'Fix the failing CI workflow on main first to unblock aging PRs.'
          : 'Metrics look healthy — keep PR median age low and CI green.'

    return {
      mode: 'deterministic',
      role,
      answer:
        `**${ins.fullName}** (${role === 'contributor' ? `Contributor Readiness ${ins.contributorReadiness}/100 · ${ins.readinessVerdict}` : `Health ${ins.healthScore}/100 · 30d proj ${ins.projectedHealth30d}/100`})\n\n` +
        `- **Commits:** ${ins.commitCount7d} (7d) · ${ins.commitCount30d} (30d)\n` +
        `- **Diversity:** ${ins.diversityScore.toFixed(2)} (${ins.diversityLabel}, ${ins.uniqueAuthors30d} authors)\n` +
        `- **PR Responsiveness:** ${ins.responsiveness} (median ${ins.prMedianAgeDays.toFixed(1)}d, ${ins.prStalePct.toFixed(0)}% stale)` +
        anomalyNote +
        `\n\n**Recommendation:** ${rec}`,
      links: [{ label: ins.fullName, url: `https://github.com/${ins.fullName}` }],
      warning: req.provider && req.provider !== 'deterministic' ? 'Demo mode uses free deterministic analysis instead of external API calls.' : undefined,
    }
  }

  if (role === 'contributor') {
    return {
      mode: 'deterministic',
      role,
      answer:
        `**Watched Repositories — Contributor Evaluation (Demo)**\n\n` +
        `1. **vitejs/vite** — Readiness **95/100 (Welcoming)** · Diversity 0.89 · Median PR age 3.4d.\n` +
        `2. **getmoto/moto** — Readiness **74/100 (Active but backlogged)** · +2.4σ PR spike (77 open PRs, 21.5d median).\n` +
        `3. **example-org/tiny-cli** — Readiness **27/100 (Caution)** · 100% single-author concentration.\n\n` +
        `**Next step:** Start with **vitejs/vite** for fastest review turnaround, or pick a scoped AWS service issue in **getmoto/moto**.`,
      links: [
        { label: 'vitejs/vite', url: 'https://github.com/vitejs/vite' },
        { label: 'getmoto/moto', url: 'https://github.com/getmoto/moto' },
      ],
    }
  }

  return {
    mode: 'deterministic',
    role,
    answer:
      `**My Repositories — Maintenance Briefing (Demo)**\n\n` +
      `- **demo-maintainer/py-etl-kit** is slipping (-0.45 pts/day, 30d projection 55/100) with a **+2.1σ CI failure spike**.\n` +
      `- **demo-maintainer/gitpulse** (91/100) and **ts-ui-tokens** (86/100) are healthy.\n\n` +
      `**Highest-leverage fix:** Repair the failing \`pytest\` workflow on \`py-etl-kit/main\` before reviewing its 4 aging PRs.`,
    links: [
      { label: 'py-etl-kit actions', url: 'https://github.com/demo-maintainer/py-etl-kit/actions' },
      { label: 'py-etl-kit PRs', url: 'https://github.com/demo-maintainer/py-etl-kit/pulls' },
    ],
  }
}
