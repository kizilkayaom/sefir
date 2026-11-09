import { useState } from 'react'
import type { FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { AuthNotice } from '@/components/auth-notice'
import { AISettingsPanel } from '@/components/ai-settings'
import { GoogleSignIn } from '@/components/google-sign-in'
import { WritingStyleSettings } from '@/components/writing-style-settings'
import { useResource } from '@/hooks/use-resource'
import { api, errorMessage } from '@/lib/api'
import { repositoryURL, setupGuideURL } from '@/lib/project'
import type { AccountStatus, GmailStatus } from '@/types/api'

export function SettingsPage({ onDeleted }: { onDeleted: () => void }) {
  const gmail = useResource<GmailStatus>('/api/settings/gmail')
  const account = useResource<AccountStatus>('/api/settings/account')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirmation, setConfirmation] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [deletionError, setDeletionError] = useState('')
  const [styleMessage, setStyleMessage] = useState('')
  const result = new URLSearchParams(window.location.search).get('gmail')
  const notices: Record<string, string> = {
    connected: 'Gmail authorization saved.',
    cancelled: 'Gmail connection was cancelled.',
    failed: 'Gmail connection failed. Check the server configuration and try again.',
  }

  async function connect() {
    setBusy(true)
    setError('')
    try {
      const result = await api<{ url: string }>('/api/settings/gmail/connect', { method: 'POST' })
      window.location.assign(result.url)
    } catch (error) {
      setError(errorMessage(error))
      setBusy(false)
    }
  }

  async function deleteAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (deleting || confirmation !== 'DELETE') return
    setDeleting(true)
    setDeletionError('')
    try {
      await api('/api/settings/account', {
        method: 'DELETE', body: JSON.stringify({ confirmation }),
      })
      onDeleted()
    } catch (error) {
      setDeletionError(errorMessage(error))
      setDeleting(false)
    }
  }

  function styleSaved() {
    setStyleMessage('Default writing style saved.')
    account.reload()
  }

  return (
    <div className="max-w-2xl">
      <h1 className="text-3xl font-semibold">Settings</h1>
      <section className="mt-6 space-y-4 rounded-xl border bg-card p-6">
        <h2 className="text-xl font-semibold">Account sign-in</h2>
        <AuthNotice />
        {account.loading && <p role="status">Loading account…</p>}
        {account.error && <div role="alert" className="space-y-3"><p className="text-destructive">{account.error}</p><Button variant="outline" onClick={account.reload}>Retry account</Button></div>}
        {account.data?.google_linked && <p>Google sign-in linked as <strong>{account.data.google_email}</strong>.</p>}
        {account.data && !account.data.google_linked && <GoogleSignIn mode="link" available={account.data.google_available} />}
        <p className="text-sm text-muted-foreground">Google sign-in gives you access to Sefir. Connect Gmail separately below to send emails.</p>
      </section>
      <section className="mt-6 space-y-4 rounded-xl border bg-card p-6">
        <h2 className="text-xl font-semibold">Gmail connection</h2>
        {result && notices[result] && <p role="status">{notices[result]}</p>}
        {gmail.loading && <p role="status">Checking Gmail…</p>}
        {gmail.error && <p role="alert" className="text-destructive">{gmail.error}</p>}
        {gmail.data?.connected && <p>Connected as <strong>{gmail.data.email}</strong>.</p>}
        {gmail.data?.message && <p>{gmail.data.message}</p>}
        {error && <p role="alert" className="text-destructive">{error}</p>}
        <div className="flex flex-wrap gap-2">
          <Button disabled={busy} onClick={connect}>{busy ? 'Opening Google…' : gmail.data?.connected ? 'Reconnect Gmail' : 'Connect Gmail'}</Button>
          <Button variant="outline" disabled={gmail.loading || busy} onClick={gmail.reload}>Check connection</Button>
        </div>
      </section>
      <AISettingsPanel />
      <section className="mt-6 space-y-4 rounded-xl border bg-card p-6">
        <h2 className="text-xl font-semibold">Writing style</h2>
        {styleMessage && <p role="status" className="text-sm">{styleMessage}</p>}
        {account.data && <WritingStyleSettings key={account.data.default_preset_id} current={account.data.default_preset_id} onSaved={styleSaved} />}
      </section>
      <section className="mt-6 space-y-4 rounded-xl border bg-card p-6">
        <h2 className="text-xl font-semibold">Your workspace and data</h2>
        <p className="text-sm leading-6 text-muted-foreground">This instance stores your account, contacts, draft versions, delivery records, and Gmail connection. AI drafting sends your supplied résumé text and contact details to your selected provider. OpenRouter also forwards them to the provider serving your selected model. Approved emails are sent through Gmail.</p>
        <p className="text-sm leading-6 text-muted-foreground">Sefir is open source under Apache-2.0. You can run your own installation and inspect the code.</p>
        <div className="flex flex-wrap gap-x-5 gap-y-3 text-sm font-medium text-primary">
          <a href="#self-host" className="rounded underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">About self-hosting</a>
          <a href={setupGuideURL} target="_blank" rel="noreferrer" className="rounded underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">Setup guide</a>
          <a href={repositoryURL} target="_blank" rel="noreferrer" className="rounded underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">Source code</a>
        </div>
      </section>
      <section className="mt-6 space-y-4 rounded-xl border border-destructive/40 bg-card p-6">
        <h2 className="text-xl font-semibold">Delete account</h2>
        <p className="text-sm leading-6 text-muted-foreground">This permanently deletes your account, contacts, drafts, delivery records, and saved Gmail connection. You will be signed out immediately. Cleanup runs in the background and retries if a service is unavailable.</p>
        <p className="text-sm leading-6 text-muted-foreground">Emails already sent or being sent cannot be recalled. Copies held by Gmail, AI providers, or installation backups are outside this deletion. Shared installation settings are kept.</p>
        <form onSubmit={deleteAccount} className="space-y-3">
          <label htmlFor="delete-confirmation" className="block text-sm font-medium">Type DELETE to confirm</label>
          <input id="delete-confirmation" className="field" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} disabled={deleting} autoComplete="off" spellCheck={false} />
          {deletionError && <p role="alert" className="text-destructive">{deletionError}</p>}
          <Button type="submit" variant="destructive" disabled={deleting || confirmation !== 'DELETE'}>{deleting ? 'Requesting deletion…' : 'Permanently delete account'}</Button>
        </form>
      </section>
    </div>
  )
}
