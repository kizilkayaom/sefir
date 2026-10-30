import { Circle, CircleCheck, Laptop } from 'lucide-react'
import { useState } from 'react'
import type { FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { AuthNotice } from '@/components/auth-notice'
import { GoogleSignIn } from '@/components/google-sign-in'
import { ThemeControl } from '@/components/theme-control'
import { api, errorMessage, setCSRFToken } from '@/lib/api'
import type { Session } from '@/types/api'

function Requirement({ met, children }: { met: boolean; children: string }) {
  const Icon = met ? CircleCheck : Circle
  return (
    <li data-met={met} className={met ? 'flex gap-2 text-emerald-700 dark:text-emerald-400' : 'flex gap-2 text-muted-foreground'}>
      <Icon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
      <span><span className="sr-only">{met ? 'Met: ' : 'Not yet met: '}</span>{children}</span>
    </li>
  )
}

export function Login({ mode = 'signin', signupAvailable = false, googleAvailable = false, passwordAvailable = true, onLogin }: {
  mode?: 'signin' | 'signup'
  signupAvailable?: boolean
  googleAvailable?: boolean
  passwordAvailable?: boolean
  onLogin: (session: Session) => void
}) {
  const setupRequired = mode === 'signup'
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const usernameValid = /^[A-Za-z0-9._-]{3,32}$/.test(username.trim())
  const passwordLength = Array.from(password).length
  const requirements = [
    { label: '8–128 characters', met: passwordLength >= 8 && passwordLength <= 128 },
    { label: 'At least one letter', met: /\p{L}/u.test(password) },
    { label: 'At least one number', met: /\p{Nd}/u.test(password) },
    { label: 'At least one symbol (such as !, @, or #)', met: /[\p{P}\p{S}]/u.test(password) },
  ]
  const passwordsMatch = confirmation !== '' && password === confirmation
  const setupValid = usernameValid && requirements.every((rule) => rule.met) && passwordsMatch

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || (setupRequired && (!signupAvailable || !setupValid))) return
    setBusy(true)
    setError('')
    try {
      const session = await api<Session>(setupRequired ? '/api/signup' : '/api/login', {
        method: 'POST',
        body: JSON.stringify({
          username: username.trim(),
          password,
          ...(setupRequired ? { confirm_password: confirmation } : {}),
        }),
      })
      setCSRFToken(session.csrf_token ?? '')
      setPassword('')
      setConfirmation('')
      onLogin(session)
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  if (setupRequired && !signupAvailable) {
    return (
      <main className="mx-auto max-w-md px-6 py-12 sm:py-20">
        <div className="mb-8 flex items-center justify-between"><a href="#home" className="text-sm font-medium text-primary hover:underline">← Back to Sefir</a><ThemeControl /></div>
        <div className="rounded-2xl border bg-card p-7 sm:p-9">
          <span className="mb-6 flex size-12 items-center justify-center rounded-xl bg-primary/10 text-primary"><Laptop aria-hidden="true" className="size-6" /></span>
          <h1 className="text-3xl font-semibold tracking-tight">Registration is closed</h1>
          <p className="mt-4 leading-7 text-muted-foreground">This instance is reserved for existing accounts. You can run your own copy of Sefir using the public source and setup guide.</p>
          <AuthNotice />
          <a href="#self-host" className="mt-6 inline-flex min-h-11 w-full items-center justify-center rounded-lg bg-primary px-5 text-sm font-medium text-primary-foreground hover:bg-primary/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">Run your own Sefir</a>
          <p className="mt-6 text-sm text-muted-foreground">Already have an account?</p>
          <a href="#signin" className="mt-3 inline-flex min-h-11 w-full items-center justify-center rounded-lg border px-5 text-sm font-medium hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">Sign in</a>
        </div>
      </main>
    )
  }

  return (
    <main className="mx-auto max-w-md px-6 py-12 sm:py-20">
      <div className="mb-8 flex items-center justify-between"><a href="#home" className="text-sm font-medium text-primary hover:underline">← Back to Sefir</a><ThemeControl /></div>
      <h1 className="text-3xl font-semibold">{setupRequired ? 'Create your account' : 'Welcome to Sefir'}</h1>
      <p className="mt-2 text-muted-foreground">
        {setupRequired ? 'Choose Google or a username and password for your Sefir account.' : signupAvailable ? 'Sign in to review your outreach.' : 'Use an existing account on this instance. Registration is closed.'}
      </p>
      <AuthNotice />
      <div className="mt-6"><GoogleSignIn mode={setupRequired ? 'signup' : 'signin'} available={googleAvailable} /></div>
      {(setupRequired || passwordAvailable) && <>
      <div className="my-6 flex items-center gap-3 text-xs text-muted-foreground"><span className="h-px flex-1 bg-border" />or use your password<span className="h-px flex-1 bg-border" /></div>
      <form onSubmit={submit} className="space-y-5 rounded-xl border bg-card p-6">
        <div className="space-y-2">
          <label htmlFor="username" className="block text-sm font-medium">Username</label>
          <input
            id="username"
            name="username"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            required
            maxLength={32}
            disabled={busy}
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            aria-describedby={setupRequired ? 'username-requirements' : undefined}
            className="field"
          />
          {setupRequired && (
            <ul id="username-requirements" aria-live="polite" className="text-sm">
              <Requirement met={usernameValid}>3–32 letters, numbers, dots, underscores, or hyphens</Requirement>
            </ul>
          )}
        </div>
        <div className="space-y-2">
          <label htmlFor="password" className="block text-sm font-medium">Password</label>
          <input
            id="password"
            name="password"
            type="password"
            autoComplete={setupRequired ? 'new-password' : 'current-password'}
            required
            disabled={busy}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            aria-describedby={setupRequired ? 'password-requirements' : undefined}
            className="field"
          />
          {setupRequired && (
            <ul id="password-requirements" aria-live="polite" className="space-y-1 text-sm">
              {requirements.map((rule) => <Requirement key={rule.label} met={rule.met}>{rule.label}</Requirement>)}
            </ul>
          )}
        </div>
        {setupRequired && (
          <div className="space-y-2">
            <label htmlFor="confirm-password" className="block text-sm font-medium">Confirm password</label>
            <input
              id="confirm-password"
              name="confirm_password"
              type="password"
              autoComplete="new-password"
              required
              disabled={busy}
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              aria-describedby="password-match"
              aria-invalid={confirmation !== '' && !passwordsMatch}
              className="field"
            />
            <ul id="password-match" aria-live="polite" className="text-sm">
              <Requirement met={passwordsMatch}>Passwords match</Requirement>
            </ul>
          </div>
        )}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <Button type="submit" className="w-full" disabled={busy || (setupRequired && !setupValid)}>
          {setupRequired ? (busy ? 'Creating account…' : 'Create account') : (busy ? 'Signing in…' : 'Sign in')}
        </Button>
      </form>
      </>}
      <p className="mt-6 text-center text-sm text-muted-foreground">
        {setupRequired ? 'Already have an account? ' : signupAvailable ? 'Don’t have an account? ' : 'Interested in your own workspace? '}
        <a href={setupRequired ? '#signin' : signupAvailable ? '#signup' : '#self-host'} className="rounded font-medium text-primary underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring">
          {setupRequired ? 'Sign in' : signupAvailable ? 'Sign up' : 'Run your own Sefir'}
        </a>
      </p>
    </main>
  )
}
