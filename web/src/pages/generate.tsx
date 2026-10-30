import { useState } from 'react'
import type { FormEvent } from 'react'

import { PageControls } from '@/components/page-controls'
import { EmailEditor } from '@/components/email-editor'
import { Button } from '@/components/ui/button'
import { useResource } from '@/hooks/use-resource'
import { useUnsavedChanges } from '@/hooks/use-unsaved-changes'
import { api, errorMessage } from '@/lib/api'
import { emailFits } from '@/lib/email-content'
import type { EmailContent } from '@/lib/email-content'
import type { AISettings, Contact, Page } from '@/types/api'
import type { Draft } from '@/types/draft'

export function GeneratePage({ contact, onCreated }: { contact: Contact | null, onCreated: (draft: Draft) => void }) {
  const [selected, setSelected] = useState<Contact | null>(contact)
  const [offset, setOffset] = useState(0)
  const contacts = useResource<Page<Contact>>(`/api/contacts?limit=25&offset=${offset}`)
  const ai = useResource<AISettings>('/api/settings/ai')
  const [provider, setProvider] = useState('')
  const providerID = provider || ai.data?.default_provider || ''
  const selectedProvider = ai.data?.providers.find((item) => item.id === providerID)
  const [cv, setCV] = useState('')
  const [mode, setMode] = useState<'generate' | 'write'>('generate')
  const [subject, setSubject] = useState('')
  const [content, setContent] = useState<EmailContent>({ html: '', text: '' })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useUnsavedChanges(!!subject || !!content.text.trim())
  const choices = (contacts.data?.items ?? []).filter((item) => item.status === 'confirmed')
  if (selected && !choices.some((item) => item.id === selected.id)) choices.unshift(selected)

  async function generate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected || busy || (mode === 'generate' && !selectedProvider)) return
    setBusy(true)
    setError('')
    try {
      const draft = await api<Draft>(mode === 'generate' ? '/api/drafts' : '/api/drafts/manual', {
        method: 'POST',
        body: JSON.stringify(mode === 'generate'
          ? { contact_id: selected.id, cv_text: cv, provider: selectedProvider?.id }
          : { contact_id: selected.id, subject, body: content.text, body_html: content.html }),
      })
      setCV('')
      onCreated(draft)
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="max-w-3xl">
      <h1 className="text-3xl font-semibold">New draft</h1>
      <p className="mt-2 text-muted-foreground">Write an email yourself or generate a starting draft to edit and review.</p>
      <form onSubmit={generate} className="mt-6 space-y-5 rounded-xl border bg-card p-6">
        <div role="group" aria-label="Draft creation method" className="flex flex-wrap gap-2">
          <Button type="button" variant={mode === 'generate' ? 'secondary' : 'ghost'} aria-pressed={mode === 'generate'} disabled={busy} onClick={() => { setMode('generate'); setError('') }}>Generate with AI</Button>
          <Button type="button" variant={mode === 'write' ? 'secondary' : 'ghost'} aria-pressed={mode === 'write'} disabled={busy} onClick={() => { setMode('write'); setError('') }}>Write yourself</Button>
        </div>
        <div>
          <label htmlFor="draft-contact" className="block text-sm font-medium">Contact</label>
          <select id="draft-contact" className="field mt-2" required disabled={busy} value={selected?.id ?? ''} onChange={(event) => setSelected(choices.find((item) => item.id === event.target.value) ?? null)}>
            <option value="">Select a confirmed contact</option>
            {choices.map((item) => <option key={item.id} value={item.id}>{item.name} — {item.email || 'missing email'}</option>)}
          </select>
        </div>
        <PageControls offset={offset} hasMore={contacts.data?.has_more ?? false} loading={contacts.loading || busy} onPage={setOffset} onRefresh={contacts.reload} />
        {contacts.loading && <p role="status">Loading contacts…</p>}
        {contacts.error && <p role="alert" className="text-destructive">{contacts.error}</p>}
        {contacts.data && choices.length === 0 && <p className="text-sm">No confirmed contacts on this page. Confirm a contact in Contacts or browse another page.</p>}
        {mode === 'generate' ? (
          <>
            {ai.loading && <p role="status">Loading AI providers…</p>}
            {ai.error && <div className="space-y-2"><p role="alert" className="text-destructive">{ai.error}</p><Button type="button" variant="outline" disabled={busy} onClick={ai.reload}>Retry AI providers</Button></div>}
            {ai.data && ai.data.providers.length === 0 && <p role="status" className="text-sm text-muted-foreground">AI generation is not configured on this installation. You can still choose Write yourself.</p>}
            {ai.data && ai.data.providers.length > 0 && <div>
              <label htmlFor="draft-provider" className="block text-sm font-medium">AI provider</label>
              <select id="draft-provider" className="field mt-2" required disabled={busy} value={selectedProvider?.id ?? ''} aria-describedby="provider-data-notice" onChange={(event) => { setProvider(event.target.value); setError('') }}>
                <option value="" disabled>Select an AI provider</option>
                {ai.data.providers.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
              </select>
              {selectedProvider && <p className="mt-2 break-words text-sm text-muted-foreground">Model: {selectedProvider.model}</p>}
              <p id="provider-data-notice" className="mt-2 text-sm leading-6 text-muted-foreground">{selectedProvider ? `Generating this draft sends your résumé text and selected contact's details to ${selectedProvider.name}.` : 'Choose which provider receives your résumé text and contact details.'}</p>
              {selectedProvider?.id === 'openrouter' && <p className="mt-2 text-sm leading-6 text-muted-foreground">OpenRouter forwards this information to the provider serving your selected model.</p>}
            </div>}
            <label className="block text-sm font-medium">Résumé text
              <textarea className="field mt-2 min-h-64" required maxLength={100000} disabled={busy} value={cv} onChange={(event) => setCV(event.target.value)} placeholder="Paste your résumé as plain text." />
            </label>
          </>
        ) : (
          <>
            <label className="block text-sm font-medium">Subject
              <input className="field mt-2" required maxLength={998} disabled={busy} value={subject} onChange={(event) => setSubject(event.target.value)} />
            </label>
            <EmailEditor initialHTML={content.html} disabled={busy} onChange={setContent} />
            {!emailFits(content) && content.text && <p className="text-sm text-destructive">Keep the email within 100,000 bytes of text and 200,000 bytes of formatting.</p>}
          </>
        )}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        {busy && <p role="status" className="text-sm">{mode === 'generate' ? 'Generating your draft. This may take a minute.' : 'Saving your draft…'}</p>}
        <Button type="submit" disabled={busy || !selected || (mode === 'generate' ? !cv.trim() || !selectedProvider : !subject.trim() || !emailFits(content))}>
          {mode === 'generate' ? (busy ? 'Generating…' : 'Generate draft') : (busy ? 'Saving…' : 'Save draft')}
        </Button>
      </form>
    </div>
  )
}
