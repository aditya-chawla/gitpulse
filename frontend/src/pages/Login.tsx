import { useSearchParams } from 'react-router'
import { loginURL } from '../api'

const messages: Record<string, string> = {
  not_allowed: 'This GitHub account isn’t on the allowlist for this dashboard.',
  invalid_state: 'The sign-in link expired or was opened in another browser. Please try again.',
  denied: 'GitHub sign-in was cancelled.',
  exchange_failed: 'GitHub didn’t accept the sign-in. Please try again.',
}

export default function LoginPage({ onDemo }: { onDemo?: () => void }) {
  const [params] = useSearchParams()
  const err = params.get('error')
  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <div className="w-full max-w-sm rounded-2xl border border-line bg-surface p-8 text-center">
        <img src="/favicon.svg" alt="" className="mx-auto h-10 w-10" />
        <h1 className="mt-4 text-xl font-semibold text-ink">gitpulse</h1>
        <p className="mt-1 text-[13px] text-muted">Repository intelligence, Postgres Data Science signals, and OSS contributor radar.</p>
        {err && (
          <p role="alert" className="mt-5 rounded-lg bg-surface-2 px-3 py-2 text-left text-[13px] text-ink-2">
            {messages[err] ?? 'Sign-in failed. Please try again.'}
          </p>
        )}
        <a href={loginURL()}
          className="mt-6 inline-flex w-full items-center justify-center gap-2 rounded-lg bg-[#24292f] px-4 py-2.5 text-[14px] font-medium text-white hover:bg-[#32383f]">
          <svg viewBox="0 0 16 16" width="18" height="18" fill="currentColor" aria-hidden>
            <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
          </svg>
          Sign in with GitHub
        </a>
        {onDemo && (
          <div className="mt-3">
            <button
              type="button"
              onClick={onDemo}
              className="w-full rounded-lg border border-line bg-surface-2 px-4 py-2 text-[13px] font-medium text-ink hover:border-accent/50 hover:text-accent"
            >
              View live demo (read-only)
            </button>
            <p className="mt-2 text-[11px] text-muted">
              Explore My Repos, OSS Explorer, DS insights &amp; Ask AI without signing in.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
