const positionKey = 'sefirHistoryPosition'
const guards = new Set<() => boolean>()

export function addNavigationGuard(guard: () => boolean) {
  guards.add(guard)
  return () => { guards.delete(guard) }
}

export function watchHashNavigation(onNavigate: () => void) {
  const readPosition = (): number | undefined => {
    const value = window.history.state?.[positionKey]
    return Number.isSafeInteger(value) ? value : undefined
  }
  const writePosition = (value: number) => {
    window.history.replaceState({ ...window.history.state, [positionKey]: value }, '')
  }
  let position = readPosition() ?? 0
  let url = window.location.href
  let restoring = false
  writePosition(position)

  const navigate = () => {
    const nextPosition = readPosition()
    const nextURL = window.location.href
    if (restoring) {
      if (nextPosition === position && nextURL === url) restoring = false
      return
    }
    if (nextPosition === position && nextURL === url) return
    if (nextPosition !== undefined && nextPosition !== position &&
      !Array.from(guards).every((guard) => guard())) {
      restoring = true
      window.history.go(position - nextPosition)
      return
    }
    position = nextPosition ?? position + 1
    url = nextURL
    writePosition(position)
    onNavigate()
  }

  window.addEventListener('popstate', navigate)
  window.addEventListener('hashchange', navigate)
  return () => {
    window.removeEventListener('popstate', navigate)
    window.removeEventListener('hashchange', navigate)
  }
}
