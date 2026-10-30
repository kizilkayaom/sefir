import { useState } from 'react'
import type { FormEvent } from 'react'

import { EmailEditor } from '@/components/email-editor'
import { Button } from '@/components/ui/button'
import { useUnsavedChanges } from '@/hooks/use-unsaved-changes'
import { api, errorMessage } from '@/lib/api'
import { emailFits, textToEmailHTML } from '@/lib/email-content'
import type { EmailContent } from '@/lib/email-content'
import type { Draft } from '@/types/draft'

export function DraftEditor({ draft, onSaved, onCancel, onDirtyChange }: {
  draft: Draft
  onSaved: (draft: Draft) => void
  onCancel: () => void
  onDirtyChange: (dirty: boolean) => void
}) {
  const initialHTML = draft.body_html || textToEmailHTML(draft.body)
  const [subject, setSubject] = useState(draft.subject)
  const [recipient, setRecipient] = useState(draft.recipient_email ?? '')
  const [content, setContent] = useState<EmailContent>({ html: initialHTML, text: draft.body })
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useUnsavedChanges(dirty)

  function changed() {
    setDirty(true)
    onDirtyChange(true)
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || !recipient.trim() || !subject.trim() || !emailFits(content)) return
    setBusy(true)
    setError('')
    try {
      const saved = await api<Draft>(`/api/drafts/${draft.draft_id}/versions/${draft.version}/edit`, {
        method: 'POST',
        body: JSON.stringify({ recipient_email: recipient.trim(), subject, body: content.text, body_html: content.html, content_hash: draft.content_hash }),
      })
      setDirty(false)
      onDirtyChange(false)
      onSaved(saved)
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} className="space-y-5 rounded-xl border bg-card p-6">
      <div>
        <h2 className="text-xl font-semibold">Edit draft</h2>
        <p className="mt-1 text-sm text-muted-foreground">Saving creates a new version to review and approve. Earlier versions and deliveries stay unchanged.</p>
      </div>
      <div>
        <label htmlFor="draft-recipient" className="block text-sm font-medium">To</label>
        <input id="draft-recipient" type="email" autoComplete="email" spellCheck={false} className="field mt-2" required maxLength={254} disabled={busy} value={recipient} onChange={(event) => {
          setRecipient(event.target.value)
          changed()
        }} aria-describedby="draft-recipient-help" />
        <p id="draft-recipient-help" className="mt-2 text-xs text-muted-foreground">Use one email address. This updates the draft; your saved contact stays the same.</p>
      </div>
      <label className="block text-sm font-medium">Subject
        <input className="field mt-2" required maxLength={998} disabled={busy} value={subject} onChange={(event) => {
          setSubject(event.target.value)
          changed()
        }} />
      </label>
      <EmailEditor initialHTML={initialHTML} disabled={busy} onChange={(value) => {
        setContent(value)
        changed()
      }} />
      {!emailFits(content) && <p className="text-sm text-muted-foreground">Add email text, keeping it within 100,000 bytes of text and 200,000 bytes of formatting.</p>}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={busy || !dirty || !recipient.trim() || !subject.trim() || !emailFits(content)}>{busy ? 'Saving…' : 'Save new version'}</Button>
        <Button type="button" variant="outline" disabled={busy} onClick={() => {
          if (dirty && !window.confirm('Discard your unsaved email changes?')) return
          onDirtyChange(false)
          onCancel()
        }}>Cancel editing</Button>
      </div>
    </form>
  )
}
