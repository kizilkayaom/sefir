import { useEffect } from 'react'

export function useUnsavedChanges(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return
    const beforeUnload = (event: BeforeUnloadEvent) => event.preventDefault()
    const navigate = (event: MouseEvent) => {
      if (!(event.target instanceof Element)) return
      const anchor = event.target.closest('a[href]')
      const signOut = event.target.closest('[data-discard-email]')
      if (!signOut && (!(anchor instanceof HTMLAnchorElement) || anchor.href === window.location.href)) return
      if (!window.confirm('Discard your unsaved email changes?')) {
        event.preventDefault()
        event.stopPropagation()
      }
    }
    window.addEventListener('beforeunload', beforeUnload)
    document.addEventListener('click', navigate, true)
    return () => {
      window.removeEventListener('beforeunload', beforeUnload)
      document.removeEventListener('click', navigate, true)
    }
  }, [dirty])
}
