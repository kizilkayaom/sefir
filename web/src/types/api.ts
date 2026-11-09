export type Page<T> = {
  items: T[]
  has_more: boolean
  offset: number
  next_offset: number
}

export type Session = {
  authenticated: boolean
  signup_available?: boolean
  account_id?: string
  google_available?: boolean
  password_available?: boolean
  owner?: string
  csrf_token?: string
}

export type AccountStatus = {
  google_available: boolean
  google_linked: boolean
  google_email?: string
  default_preset_id: string
}

export type AIProvider = {
  id: 'cohere' | 'anthropic' | 'openai' | 'openrouter'
  name: string
  model: string
}

export type AISettings = {
  default_provider: AIProvider['id'] | null
  providers: AIProvider[]
  can_manage?: boolean
  options?: AIProviderOption[]
}

export type AIProviderOption = AIProvider & {
  configured: boolean
}

export type AIModel = {
  id: string
  name: string
}

export type Contact = {
  id: string
  name: string | null
  email: string | null
  organization: string | null
  role: string | null
  context: string | null
  status: 'proposed' | 'confirmed'
  created_at: string
}

export type ContactInput = {
  name: string
  email: string
  organization: string
  role: string
  context: string
  status: 'proposed' | 'confirmed'
}

export type Activity = {
  outbox_id: string
  draft_id: string
  version: number
  subject: string
  recipient_email: string | null
  approved_by: string
  state: 'pending' | 'sending' | 'sent' | 'failed' | 'needs_attention'
  attempts: number
  gmail_message_id: string | null
  last_error: string | null
  updated_at: string
}

export type DraftStatus = {
  workflow: {
    status: string
    workflow_id?: string
    run_id?: string
    failure?: string | null
  } | null
  delivery: Activity | null
}

export type GmailStatus = {
  connected: boolean
  email?: string
  message?: string
}
