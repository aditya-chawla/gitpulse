import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { AdvisorCtx, type AdvisorMessage, type AdvisorPrompt } from '../advisor'
import { api, ApiError, type AdvisorProvider as ProviderType, type AdvisorRole, type AskResponse } from '../api'

const STORAGE_PROVIDER = 'gitpulse-byok-provider'
const STORAGE_KEY = 'gitpulse-byok-key'
const STORAGE_MODEL = 'gitpulse-byok-model'

export function AdvisorProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const [repoId, setRepoId] = useState<number | undefined>()
  const [repoName, setRepoName] = useState<string | undefined>()
  const [messages, setMessages] = useState<AdvisorMessage[]>([])

  const [provider, setProvider] = useState<ProviderType>(() => {
    try {
      return (localStorage.getItem(STORAGE_PROVIDER) as ProviderType) || 'deterministic'
    } catch {
      return 'deterministic'
    }
  })
  const [apiKey, setApiKey] = useState(() => {
    try {
      return localStorage.getItem(STORAGE_KEY) || ''
    } catch {
      return ''
    }
  })
  const [model, setModel] = useState(() => {
    try {
      return localStorage.getItem(STORAGE_MODEL) || ''
    } catch {
      return ''
    }
  })

  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_PROVIDER, provider)
      localStorage.setItem(STORAGE_KEY, apiKey)
      localStorage.setItem(STORAGE_MODEL, model)
    } catch {
      /* storage unavailable */
    }
  }, [provider, apiKey, model])

  const setRepoContext = useCallback((id?: number, name?: string) => {
    setRepoId(id)
    setRepoName(name)
  }, [])

  const clearMessages = useCallback(() => setMessages([]), [])

  const sendQuestion = useCallback(
    async (questionText: string, role: AdvisorRole, overrideRepoId?: number) => {
      const q = questionText.trim()
      if (!q) return
      const id = Date.now() + Math.random()
      const targetRepoId = overrideRepoId !== undefined ? overrideRepoId : repoId
      setMessages((m) => [...m, { id, question: q, loading: true }])
      try {
        const res = await api<AskResponse>('/api/agent/ask', {
          method: 'POST',
          body: JSON.stringify({
            role,
            repoId: targetRepoId || 0,
            question: q,
            provider,
            model: model.trim(),
            apiKey: provider === 'deterministic' ? '' : apiKey.trim(),
          }),
        })
        setMessages((m) => m.map((item) => (item.id === id ? { ...item, loading: false, response: res } : item)))
      } catch (e) {
        const msg = e instanceof ApiError ? e.message : 'Request failed'
        setMessages((m) => m.map((item) => (item.id === id ? { ...item, loading: false, error: msg } : item)))
      }
    },
    [repoId, provider, model, apiKey],
  )

  const askWith = useCallback(
    (prompt: AdvisorPrompt) => {
      if (prompt.repoId !== undefined) {
        setRepoId(prompt.repoId)
        setRepoName(prompt.repoName)
      }
      setOpen(true)
      void sendQuestion(prompt.question, prompt.role ?? 'maintainer', prompt.repoId)
    },
    [sendQuestion],
  )

  return (
    <AdvisorCtx.Provider
      value={{
        open,
        setOpen,
        repoId,
        repoName,
        provider,
        setProvider,
        apiKey,
        setApiKey,
        model,
        setModel,
        messages,
        clearMessages,
        setRepoContext,
        sendQuestion,
        askWith,
      }}
    >
      {children}
    </AdvisorCtx.Provider>
  )
}
