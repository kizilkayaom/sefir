import { lazy, Suspense, useEffect, useState } from 'react'

import { Login } from '@/components/login'
import { AccountMenu } from '@/components/account-menu'
import { ThemeControl } from '@/components/theme-control'
import { Button } from '@/components/ui/button'
import { useResource } from '@/hooks/use-resource'
import { api, errorMessage, setCSRFToken } from '@/lib/api'
import { cn } from '@/lib/utils'
import { ActivityPage } from '@/pages/activity'
import { ContactsPage } from '@/pages/contacts'
import { DraftsPage } from '@/pages/drafts'
import { GeneratePage } from '@/pages/generate'
import { LandingPage } from '@/pages/landing'
import type { Contact, Session } from '@/types/api'
import type { Draft } from '@/types/draft'

const SettingsPage = lazy(() => import('@/pages/settings').then((module) => ({ default: module.SettingsPage })))

const tabs = [
  { id: 'drafts', label: 'Drafts' },
  { id: 'contacts', label: 'Contacts' },
  { id: 'new', label: 'New draft' },
  { id: 'activity', label: 'Activity' },
  { id: 'settings', label: 'Settings' },
]

function currentPage() {
  const hash = window.location.hash.slice(1)
  return tabs.some((tab) => tab.id === hash) || ['home', 'how-it-works', 'self-host', 'signin', 'signup'].includes(hash) ? hash : ''
}

function App() {
  const initialSession = useResource<Session>('/api/session')
  const [sessionOverride, setSession] = useState<Session | null>(null)
  const session = sessionOverride ?? initialSession.data
  const [page, setPage] = useState(currentPage)
  const tab = tabs.some((tab) => tab.id === page) ? page : 'drafts'
  const googleAvailable = session?.google_available ?? initialSession.data?.google_available
  const signupAvailable = session?.signup_available ?? initialSession.data?.signup_available ?? false
  const passwordAvailable = !session?.authenticated || session.password_available !== false
  const [contact, setContact] = useState<Contact | null>(null)
  const [createdDraft, setCreatedDraft] = useState<Draft | null>(null)
  const [error, setError] = useState('')
  const [loggingOut, setLoggingOut] = useState(false)
  const [deletionRequested, setDeletionRequested] = useState(false)

  useEffect(() => {
    if (initialSession.data) {
      setCSRFToken(initialSession.data.csrf_token ?? '')
    }
  }, [initialSession.data])

  useEffect(() => {
    if (!session || !['home', 'how-it-works', 'self-host'].includes(page)) return
    const frame = requestAnimationFrame(() => document.getElementById(page)?.scrollIntoView())
    return () => cancelAnimationFrame(frame)
  }, [page, session])

  useEffect(() => {
    const expired = () => {
      setSession({ authenticated: false })
      setContact(null)
      setCreatedDraft(null)
    }
    const navigate = () => setPage(currentPage())
    window.addEventListener('sefir-session-expired', expired)
    window.addEventListener('hashchange', navigate)
    return () => {
      window.removeEventListener('sefir-session-expired', expired)
      window.removeEventListener('hashchange', navigate)
    }
  }, [])

  async function logout() {
    setLoggingOut(true)
    setError('')
    try {
      await api('/api/logout', { method: 'POST' })
      setCSRFToken('')
      setSession({ authenticated: false })
      setContact(null)
      setCreatedDraft(null)
      window.location.hash = 'home'
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setLoggingOut(false)
    }
  }

  if (!session) {
    return (
      <main className="mx-auto max-w-md px-6 py-20">
        {initialSession.error ? (
          <div role="alert" className="space-y-4">
            <p>{initialSession.error}</p>
            <Button onClick={initialSession.reload}>Retry connection</Button>
          </div>
        ) : <p role="status">Loading Sefir…</p>}
      </main>
    )
  }
  if (deletionRequested) return (
    <main className="mx-auto max-w-lg space-y-4 px-6 py-20">
      <h1 className="text-3xl font-semibold">Account deletion requested</h1>
      <p role="status">You have been signed out. Your workspace is no longer accessible. Deletion continues in the background, including after server restarts.</p>
      <Button onClick={() => { setDeletionRequested(false); window.location.hash = 'home' }}>Back to home</Button>
    </main>
  )
  if (page === 'home' || page === 'how-it-works' || page === 'self-host' || (!session.authenticated && page === '')) {
    return <LandingPage session={{ ...session, google_available: googleAvailable, password_available: passwordAvailable, signup_available: signupAvailable }} />
  }
  if (!session.authenticated) return <Login key={page === 'signup' ? 'signup' : 'signin'} mode={page === 'signup' ? 'signup' : 'signin'} signupAvailable={signupAvailable} googleAvailable={googleAvailable} passwordAvailable={passwordAvailable} onLogin={(value) => {
    setSession(value)
    if (!tabs.some((tab) => tab.id === page)) window.location.hash = 'drafts'
  }} />

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b bg-card px-6 py-4">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-4">
          <a href="#home" className="text-lg font-semibold">Sefir</a>
          <nav aria-label="Main navigation" className="flex flex-wrap gap-1">
            {tabs.filter((item) => item.id !== 'settings').map((item) => (
              <a
                key={item.id}
                href={`#${item.id}`}
                aria-current={tab === item.id ? 'page' : undefined}
                className={cn('rounded-lg px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', tab === item.id ? 'bg-primary text-primary-foreground' : 'hover:bg-muted')}
              >
                {item.label}
              </a>
            ))}
          </nav>
          <div className="flex flex-wrap items-center gap-3">
            <ThemeControl />
            <AccountMenu owner={session.owner ?? 'Account'} busy={loggingOut} onLogout={logout} />
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-10">
        {error && <p role="alert" className="mb-4 text-destructive">{error}</p>}
        {tab === 'drafts' && <DraftsPage key={createdDraft ? `${createdDraft.draft_id}:${createdDraft.version}` : 'saved'} initialDraft={createdDraft} onSaved={setCreatedDraft} />}
        {tab === 'contacts' && <ContactsPage onDraft={(selected) => { setContact(selected); window.location.hash = 'new' }} />}
        {tab === 'new' && <GeneratePage key={contact?.id ?? 'choose'} contact={contact} onCreated={(draft) => { setCreatedDraft(draft); window.location.hash = 'drafts' }} />}
        {tab === 'activity' && <ActivityPage />}
        {tab === 'settings' && <Suspense fallback={<p role="status">Loading settings…</p>}><SettingsPage onDeleted={() => {
          setCSRFToken('')
          setSession({ authenticated: false })
          setContact(null)
          setCreatedDraft(null)
          setError('')
          setDeletionRequested(true)
          window.location.hash = 'home'
        }} /></Suspense>}
      </main>
    </div>
  )
}

export default App
