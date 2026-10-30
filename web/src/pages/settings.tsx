import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { AuthNotice } from '@/components/auth-notice'
import { AISettingsPanel } from '@/components/ai-settings'
import { GoogleSignIn } from '@/components/google-sign-in'
import { useResource } from '@/hooks/use-resource'
import { api, errorMessage } from '@/lib/api'
import { repositoryURL, setupGuideURL } from '@/lib/project'
import type { AccountStatus, GmailStatus } from '@/types/api'

export function SettingsPage() {
  const gmail = useResource<GmailStatus>('/api/settings/gmail')
  const account = useResource<AccountStatus>('/api/settings/account')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
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
        <h2 className="text-xl font-semibold">Your workspace and data</h2>
        <p className="text-sm leading-6 text-muted-foreground">This instance stores your account, contacts, draft versions, delivery records, and Gmail connection. AI drafting sends your supplied résumé text and contact details to your selected provider. OpenRouter also forwards them to the provider serving your selected model. Approved emails are sent through Gmail.</p>
        <p className="text-sm leading-6 text-muted-foreground">Sefir is open source under Apache-2.0. You can run your own installation and inspect the code.</p>
        <div className="flex flex-wrap gap-x-5 gap-y-3 text-sm font-medium text-primary">
          <a href="#self-host" className="rounded underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">About self-hosting</a>
          <a href={setupGuideURL} target="_blank" rel="noreferrer" className="rounded underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">Setup guide</a>
          <a href={repositoryURL} target="_blank" rel="noreferrer" className="rounded underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">Source code</a>
        </div>
      </section>
    </div>
  )
}
