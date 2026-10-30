import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { api, errorMessage } from '@/lib/api'

export function GoogleSignIn({ mode, available = false }: { mode: 'signup' | 'signin' | 'link'; available?: boolean }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const label = mode === 'signup' ? 'Sign up with Google' : mode === 'link' ? 'Link Google account' : 'Sign in with Google'

  async function start() {
    setBusy(true)
    setError('')
    try {
      const result = await api<{ url: string }>(mode === 'link' ? '/api/settings/account/google' : '/api/auth/google', {
        method: 'POST',
        body: JSON.stringify(mode === 'link' ? {} : { mode }),
      })
      window.location.assign(result.url)
    } catch (error) {
      setError(errorMessage(error))
      setBusy(false)
    }
  }

  return (
    <div className="space-y-2">
      <Button variant="outline" className="h-11 w-full gap-3 px-5" disabled={busy || !available} onClick={start}>
        <svg aria-hidden="true" className="size-4" viewBox="0 0 18 18">
          <path fill="#4285F4" d="M17.64 9.205c0-.638-.057-1.252-.164-1.841H9v3.481h4.844a4.14 4.14 0 0 1-1.797 2.715v2.258h2.909c1.702-1.567 2.684-3.875 2.684-6.613Z" />
          <path fill="#34A853" d="M9 18c2.43 0 4.468-.806 5.956-2.182l-2.909-2.258c-.806.54-1.835.86-3.047.86-2.344 0-4.328-1.584-5.036-3.712H.957v2.332A9 9 0 0 0 9 18Z" />
          <path fill="#FBBC05" d="M3.964 10.708A5.41 5.41 0 0 1 3.682 9c0-.593.102-1.17.282-1.708V4.96H.957A9 9 0 0 0 0 9c0 1.452.347 2.827.957 4.04l3.007-2.332Z" />
          <path fill="#EA4335" d="M9 3.58c1.322 0 2.508.455 3.44 1.346l2.581-2.581C13.464.892 11.427 0 9 0A9 9 0 0 0 .957 4.96l3.007 2.332C4.672 5.164 6.656 3.58 9 3.58Z" />
        </svg>
        {busy ? 'Opening Google…' : label}
      </Button>
      {!available && <p className="text-xs text-muted-foreground">Google sign-in is not configured yet.</p>}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </div>
  )
}
