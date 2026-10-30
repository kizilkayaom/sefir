import { Monitor, Moon, Sun } from 'lucide-react'
import { useEffect, useSyncExternalStore } from 'react'

import { Button } from '@/components/ui/button'
import { applyTheme, getTheme, setTheme, subscribeToTheme } from '@/lib/theme'
import type { Theme } from '@/lib/theme'

export function ThemeControl() {
  const theme = useSyncExternalStore(subscribeToTheme, getTheme)
  const next: Record<Theme, Theme> = { system: 'light', light: 'dark', dark: 'system' }
  const Icon = theme === 'system' ? Monitor : theme === 'light' ? Sun : Moon
  const label = `Theme: ${theme}. Switch to ${next[theme]} mode`

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const update = () => applyTheme(theme)
    update()
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [theme])

  return (
    <Button variant="ghost" size="icon" className="size-10" aria-label={label} title={label} onClick={() => setTheme(next[theme])}>
      <Icon aria-hidden="true" className="size-5" />
    </Button>
  )
}
