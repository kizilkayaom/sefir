import { useEffect } from 'react'

import { addNavigationGuard } from '@/lib/navigation'

export function useUnsavedChanges(dirty: boolean, message = 'Discard your unsaved email changes?') {
  useEffect(() => {
    if (!dirty) return
    const removeGuard = addNavigationGuard(() => window.confirm(message))
    const beforeUnload = (event: BeforeUnloadEvent) => event.preventDefault()
    const navigate = (event: MouseEvent) => {
      if (!(event.target instanceof Element)) return
      const anchor = event.target.closest('a[href]')
      const signOut = event.target.closest('[data-discard-email]')
      if (!signOut && (!(anchor instanceof HTMLAnchorElement) || anchor.href === window.location.href)) return
      if (!window.confirm(message)) {
        event.preventDefault()
        event.stopPropagation()
      }
    }
    window.addEventListener('beforeunload', beforeUnload)
    document.addEventListener('click', navigate, true)
    return () => {
      removeGuard()
      window.removeEventListener('beforeunload', beforeUnload)
      document.removeEventListener('click', navigate, true)
    }
  }, [dirty, message])
}
