export type Draft = {
  draft_id: string
  version: number
  contact_id: string
  recipient_email: string | null
  subject: string
  body: string
  body_html?: string
  content_hash: string
  created_at: string
}
