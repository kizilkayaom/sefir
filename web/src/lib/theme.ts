export type Theme = 'light' | 'dark' | 'system'

export function parseTheme(value: string | null | undefined): Theme {
  return value === 'light' || value === 'dark' ? value : 'system'
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme
  document.documentElement.classList.toggle('dark', theme === 'dark' ||
    (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches))
}

export function getTheme(): Theme {
  return parseTheme(document.documentElement.dataset.theme)
}

export function setTheme(theme: Theme) {
  try {
    localStorage.setItem('sefir-theme', theme)
  } catch {}
  applyTheme(theme)
  window.dispatchEvent(new Event('sefir-theme-change'))
}

export function subscribeToTheme(listener: () => void) {
  const storageChanged = (event: StorageEvent) => {
    if (event.key !== 'sefir-theme' && event.key !== null) return
    applyTheme(parseTheme(event.newValue))
    listener()
  }
  window.addEventListener('sefir-theme-change', listener)
  window.addEventListener('storage', storageChanged)
  return () => {
    window.removeEventListener('sefir-theme-change', listener)
    window.removeEventListener('storage', storageChanged)
  }
}
