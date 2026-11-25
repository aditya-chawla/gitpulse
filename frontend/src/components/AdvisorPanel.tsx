import { useState, type FormEvent } from 'react'
import { useAdvisor } from '../advisor'
import type { AdvisorProvider as ProviderType, AdvisorRole } from '../api'

const maintainerPrompts = [
  'What should I prioritize fixing this week?',
  'Explain any statistical anomalies or slipping health scores',
  'Which open PRs or CI failures need attention?',
]

const contributorPrompts = [
  'Which watched repo is best to contribute to right now?',
  'Explain the contributor diversity and PR responsiveness scores',
  'Are there any red flags or backlog spikes across watched repos?',
]

const defaultModels: Record<ProviderType, string> = {
  deterministic: '',
  gemini: 'gemini-3.8-flash',
  openai: 'gpt-4o-mini',
  anthropic: 'claude-haiku-4-5-20251001',
}

export default function AdvisorPanel({ role }: { role: AdvisorRole }) {
  const {
    open, setOpen, repoId, repoName, setRepoContext,
    provider, setProvider, apiKey, setApiKey, model, setModel,
    messages, clearMessages, sendQuestion,
  } = useAdvisor()

  const [showConfig, setShowConfig] = useState(false)
  const [showMCP, setShowMCP] = useState(false)
  const [copiedMCP, setCopiedMCP] = useState(false)
  const [input, setInput] = useState('')

  if (!open) return null

  const prompts = role === 'contributor' ? contributorPrompts : maintainerPrompts
  const mcpUrl = `${window.location.origin}/mcp`
  const mcpConfig = JSON.stringify(
    {
      mcpServers: {
        gitpulse: {
          url: mcpUrl,
          headers: { Authorization: 'Bearer <YOUR_MCP_TOKEN>' },
        },
      },
    },
    null,
    2,
  )

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (!input.trim()) return
    const q = input
    setInput('')
    void sendQuestion(q, role)
  }

  return (
    <aside
      aria-label="GitPulse AI Advisor"
      className="fixed inset-y-0 right-0 z-50 flex w-full max-w-md flex-col border-l border-line bg-surface shadow-2xl lg:sticky lg:top-[57px] lg:z-30 lg:h-[calc(100vh-57px)] lg:w-96 lg:shrink-0 lg:shadow-none"
    >
      <div className="flex items-center justify-between border-b border-line px-4 py-3">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-[14px] font-semibold text-ink">✦ Ask GitPulse</span>
            <span
              className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                role === 'contributor'
                  ? 'bg-sky-500/15 text-sky-500'
                  : 'bg-accent/15 text-accent'
              }`}
            >
              {role === 'contributor' ? 'Contributor Advisor' : 'Maintainer Copilot'}
            </span>
          </div>
          <div className="mt-1 flex items-center gap-2 text-[11px] text-muted">
            <span>
              Scope: <b className="text-ink-2">{repoName ? repoName : role === 'contributor' ? 'Watched repos' : 'My repos'}</b>
            </span>
            {repoId && (
              <button
                type="button"
                onClick={() => setRepoContext(undefined, undefined)}
                className="text-accent hover:underline"
              >
                All
              </button>
            )}
          </div>
        </div>
        <button
          type="button"
          onClick={() => setOpen(false)}
          aria-label="Close AI Advisor"
          className="rounded-md px-2 py-1 text-[14px] text-muted hover:bg-surface-2 hover:text-ink"
        >
          ✕
        </button>
      </div>

      <div className="flex items-center gap-2 border-b border-line bg-surface-2/40 px-4 py-2 text-[12px]">
        <button
          type="button"
          onClick={() => {
            setShowConfig((v) => !v)
            setShowMCP(false)
          }}
          className={`rounded-md px-2 py-1 font-medium ${showConfig ? 'bg-surface text-ink shadow-xs' : 'text-muted hover:text-ink'}`}
        >
          Provider: {provider === 'deterministic' ? 'Free (Rules)' : provider}
        </button>
        <button
          type="button"
          onClick={() => {
            setShowMCP((v) => !v)
            setShowConfig(false)
          }}
          className={`rounded-md px-2 py-1 font-medium ${showMCP ? 'bg-surface text-ink shadow-xs' : 'text-muted hover:text-ink'}`}
        >
          Connect MCP
        </button>
        {messages.length > 0 && (
          <button
            type="button"
            onClick={clearMessages}
            className="ml-auto text-muted hover:text-ink"
          >
            Clear
          </button>
        )}
      </div>

      {showConfig && (
        <div className="space-y-2.5 border-b border-line bg-surface-2/60 px-4 py-3 text-[12px]">
          <p className="text-muted">
            Bring your own key (stored only in your browser) or use the free deterministic mode.
          </p>
          <select
            value={provider}
            onChange={(e) => setProvider(e.target.value as ProviderType)}
            aria-label="AI Provider"
            className="w-full rounded-lg border border-line bg-surface px-2.5 py-1.5 text-ink"
          >
            <option value="deterministic">Free Deterministic (Postgres DS metrics, $0)</option>
            <option value="gemini">Google Gemini (BYOK — free tier friendly)</option>
            <option value="openai">OpenAI GPT (BYOK)</option>
            <option value="anthropic">Anthropic Claude (BYOK)</option>
          </select>
          {provider !== 'deterministic' && (
            <>
              <input
                type="password"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={`Paste ${provider} API key`}
                className="w-full rounded-lg border border-line bg-surface px-2.5 py-1.5 text-ink placeholder:text-muted"
                aria-label="API Key"
              />
              <input
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder={`Model (default: ${defaultModels[provider]})`}
                className="w-full rounded-lg border border-line bg-surface px-2.5 py-1.5 text-ink placeholder:text-muted"
                aria-label="Model override"
              />
            </>
          )}
        </div>
      )}

      {showMCP && (
        <div className="space-y-2 border-b border-line bg-surface-2/60 px-4 py-3 text-[12px]">
          <p className="text-muted">
            Connect your own Claude Desktop, Cursor, or Gemini CLI directly to GitPulse&rsquo;s MCP server (including{' '}
            <code className="font-mono text-ink-2">get_repo_insights</code> and{' '}
            <code className="font-mono text-ink-2">compare_watched_repos</code>):
          </p>
          <pre className="overflow-x-auto rounded-lg border border-line bg-surface p-2 font-mono text-[11px] text-ink-2">
            {mcpConfig}
          </pre>
          <button
            type="button"
            onClick={() => {
              void navigator.clipboard.writeText(mcpConfig)
              setCopiedMCP(true)
              window.setTimeout(() => setCopiedMCP(false), 2000)
            }}
            className="rounded-md border border-line bg-surface px-2.5 py-1 font-medium text-ink hover:bg-surface-2"
          >
            {copiedMCP ? 'Copied!' : 'Copy MCP config'}
          </button>
        </div>
      )}

      <div className="flex-1 space-y-4 overflow-y-auto p-4 text-[13px]">
        {messages.length === 0 ? (
          <div className="space-y-3">
            <p className="text-muted">
              {role === 'contributor'
                ? 'Ask about watched open-source repositories — commit momentum, contributor diversity, PR turnaround, and where to contribute.'
                : 'Ask about your repositories — health score drops, Z-score anomalies, failing CI, and what to fix next.'}
            </p>
            <div className="space-y-2">
              <div className="text-[11px] font-medium uppercase tracking-wide text-muted">Suggested questions</div>
              {prompts.map((p) => (
                <button
                  key={p}
                  type="button"
                  onClick={() => void sendQuestion(p, role)}
                  className="block w-full rounded-lg border border-line bg-surface-2/50 px-3 py-2 text-left text-[12px] text-ink-2 hover:border-accent/50 hover:text-ink"
                >
                  {p}
                </button>
              ))}
            </div>
          </div>
        ) : (
          messages.map((m) => (
            <div key={m.id} className="space-y-2">
              <div className="ml-6 rounded-xl bg-accent/15 px-3 py-2 text-ink">{m.question}</div>
              {m.loading ? (
                <div className="mr-4 rounded-xl border border-line bg-surface-2/40 px-3 py-2 text-muted">
                  Analyzing Postgres snapshots…
                </div>
              ) : m.error ? (
                <div className="mr-4 rounded-xl border border-critical/40 bg-critical/10 px-3 py-2 text-critical">
                  {m.error}
                </div>
              ) : m.response ? (
                <div className="mr-2 space-y-2 rounded-xl border border-line bg-surface-2/40 p-3">
                  {m.response.warning && (
                    <p className="rounded-md bg-amber-500/10 px-2 py-1 text-[11px] text-amber-500">
                      {m.response.warning}
                    </p>
                  )}
                  <div className="whitespace-pre-line leading-relaxed text-ink-2">{m.response.answer}</div>
                  {m.response.links.length > 0 && (
                    <div className="flex flex-wrap gap-1.5 pt-1">
                      {m.response.links.map((l) => (
                        <a
                          key={l.url}
                          href={l.url}
                          target="_blank"
                          rel="noreferrer"
                          className="rounded-md border border-line bg-surface px-2 py-0.5 text-[11px] text-accent hover:underline"
                        >
                          {l.label} ↗
                        </a>
                      ))}
                    </div>
                  )}
                  <div className="text-[10px] uppercase tracking-wide text-muted">
                    {m.response.mode}
                    {m.response.model ? ` · ${m.response.model}` : ''}
                  </div>
                </div>
              ) : null}
            </div>
          ))
        )}
      </div>

      <form onSubmit={handleSubmit} className="border-t border-line p-3">
        <div className="flex gap-2">
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={role === 'contributor' ? 'Ask about watched repos…' : 'Ask about your repos…'}
            className="min-w-0 flex-1 rounded-lg border border-line bg-surface-2 px-3 py-1.5 text-[13px] text-ink placeholder:text-muted"
            aria-label="Ask GitPulse"
          />
          <button
            type="submit"
            disabled={!input.trim()}
            className="rounded-lg bg-accent px-3 py-1.5 text-[13px] font-medium text-white hover:opacity-90 disabled:opacity-50"
          >
            Ask
          </button>
        </div>
      </form>
    </aside>
  )
}
