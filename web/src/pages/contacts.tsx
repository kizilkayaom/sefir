import { useState } from 'react'
import type { FormEvent } from 'react'

import { PageControls } from '@/components/page-controls'
import { Button } from '@/components/ui/button'
import { useResource } from '@/hooks/use-resource'
import { api, errorMessage } from '@/lib/api'
import type { Contact, ContactInput, Page } from '@/types/api'

const blank: ContactInput = { name: '', email: '', organization: '', role: '', context: '', status: 'proposed' }

export function ContactsPage({ onDraft }: { onDraft: (contact: Contact) => void }) {
  const [offset, setOffset] = useState(0)
  const contacts = useResource<Page<Contact>>(`/api/contacts?limit=25&offset=${offset}`)
  const [editing, setEditing] = useState<Contact | null>(null)
  const [form, setForm] = useState<ContactInput>(blank)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  function edit(contact: Contact) {
    setEditing(contact)
    setForm({
      name: contact.name ?? '', email: contact.email ?? '', organization: contact.organization ?? '',
      role: contact.role ?? '', context: contact.context ?? '', status: contact.status,
    })
    setError('')
    setNotice('')
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setError('')
    setNotice('')
    try {
      await api<Contact>(editing ? `/api/contacts/${editing.id}` : '/api/contacts', {
        method: editing ? 'PUT' : 'POST',
        body: JSON.stringify(form),
      })
      setEditing(null)
      setForm(blank)
      setNotice('Contact saved.')
      contacts.reload()
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <h1 className="text-3xl font-semibold">Contacts</h1>
      <p className="mt-2 text-muted-foreground">Confirm contact details before drafting outreach.</p>
      <div className="mt-6 grid items-start gap-6 lg:grid-cols-2">
        <section className="min-w-0">
          <PageControls offset={offset} hasMore={contacts.data?.has_more ?? false} loading={contacts.loading} onPage={setOffset} onRefresh={contacts.reload} />
          {contacts.loading && <p role="status">Loading contacts…</p>}
          {contacts.error && <p role="alert" className="text-destructive">{contacts.error}</p>}
          {contacts.data?.items.length === 0 && <p>No contacts yet. Add your first contact.</p>}
          <ul className="space-y-3">
            {contacts.data?.items.map((contact) => (
              <li key={contact.id} className="space-y-2 rounded-xl border bg-card p-5">
                <h2 className="break-words font-semibold">{contact.name || 'Unnamed contact'}</h2>
                <p className="break-words text-sm">{contact.email || 'Missing email'}</p>
                <p className="text-sm text-muted-foreground">{[contact.role, contact.organization].filter(Boolean).join(' · ')}</p>
                <p className="text-sm capitalize">{contact.status}</p>
                <div className="flex flex-wrap gap-2">
                  <Button variant="outline" disabled={busy} onClick={() => edit(contact)}>Edit {contact.name || 'contact'}</Button>
                  <Button disabled={contact.status !== 'confirmed'} onClick={() => onDraft(contact)}>Draft email</Button>
                </div>
              </li>
            ))}
          </ul>
        </section>
        <form onSubmit={save} className="space-y-4 rounded-xl border bg-card p-6">
          <h2 className="text-xl font-semibold">{editing ? 'Edit contact' : 'Add contact'}</h2>
          <fieldset disabled={busy} className="space-y-4">
            {(['name', 'email', 'organization', 'role'] as const).map((field) => (
              <label key={field} className="block text-sm font-medium capitalize">
                {field.charAt(0).toUpperCase() + field.slice(1)}
                <input className="field mt-1" type={field === 'email' ? 'email' : 'text'} required={field === 'name'} maxLength={field === 'name' ? 200 : field === 'email' ? 254 : 300} value={form[field]} onChange={(event) => setForm({ ...form, [field]: event.target.value })} />
              </label>
            ))}
            <label className="block text-sm font-medium">Context
              <textarea className="field mt-1 min-h-28" maxLength={10000} value={form.context} onChange={(event) => setForm({ ...form, context: event.target.value })} />
            </label>
            <label className="flex items-start gap-3 text-sm">
              <input type="checkbox" className="mt-1" checked={form.status === 'confirmed'} onChange={(event) => setForm({ ...form, status: event.target.checked ? 'confirmed' : 'proposed' })} />
              <span>I have checked these contact details.</span>
            </label>
          </fieldset>
          {editing && <p className="text-sm text-muted-foreground">Changes apply to new drafts. Saved drafts keep their original recipient.</p>}
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
          {notice && <p role="status" className="text-sm">{notice}</p>}
          <div className="flex gap-2">
            <Button type="submit" disabled={busy}>{busy ? 'Saving…' : 'Save contact'}</Button>
            {editing && <Button variant="outline" type="button" disabled={busy} onClick={() => { setEditing(null); setForm(blank); setError('') }}>Cancel edit</Button>}
          </div>
        </form>
      </div>
    </>
  )
}
