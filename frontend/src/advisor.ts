import { createContext, useContext } from 'react'
import type { AdvisorProvider as ProviderType, AdvisorRole, AskResponse } from './api'

export interface AdvisorPrompt {
  role?: AdvisorRole
  repoId?: number
  repoName?: string
  question: string
}

export interface AdvisorMessage {
  id: number
  question: string
  response?: AskResponse
  error?: string
  loading?: boolean
}

export interface AdvisorContextValue {
  open: boolean
  setOpen: (v: boolean) => void
  repoId?: number
  repoName?: string
  provider: ProviderType
  setProvider: (p: ProviderType) => void
  apiKey: string
  setApiKey: (k: string) => void
  model: string
  setModel: (m: string) => void
  messages: AdvisorMessage[]
  clearMessages: () => void
  setRepoContext: (repoId?: number, repoName?: string) => void
  sendQuestion: (question: string, role: AdvisorRole, overrideRepoId?: number) => Promise<void>
  askWith: (prompt: AdvisorPrompt) => void
}

export const AdvisorCtx = createContext<AdvisorContextValue | null>(null)

export function useAdvisor(): AdvisorContextValue {
  const ctx = useContext(AdvisorCtx)
  if (!ctx) throw new Error('useAdvisor must be used within AdvisorProvider')
  return ctx
}
