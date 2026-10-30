const notices: Record<string, string> = {
  signup_closed: 'Registration is closed on this instance. Please use an existing Sefir account to sign in.',
  linked: 'Google sign-in is linked to your account.',
  cancelled: 'Google sign-in was cancelled. You can try again.',
  expired: 'This sign-in attempt expired. Please start again.',
  failed: 'Google sign-in could not be completed. Check the Google client configuration and try again.',
  already_linked: 'These accounts already have different Google sign-in details. Your existing sign-in methods still work.',
}

export function AuthNotice() {
  const result = new URLSearchParams(window.location.search).get('auth')
  if (!result || !notices[result]) return null
  return <p role={result === 'linked' ? 'status' : 'alert'} className="mt-4 text-sm">{notices[result]}</p>
}
