import { useState } from 'react'
import type { FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { api, errorMessage } from '@/lib/api'

export function WritingStyleSettings({ current, onSaved }: { current: string, onSaved: () => void }) {
  const [presetID, setPresetID] = useState(current)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await api('/api/settings/account/style', { method: 'PUT', body: JSON.stringify({ preset_id: presetID }) })
      onSaved()
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} className="space-y-3">
      <label htmlFor="default-preset" className="block text-sm font-medium">Default writing style
        <select id="default-preset" className="field mt-2" required value={presetID} disabled={busy} onChange={(event) => { setPresetID(event.target.value); setError('') }}>
          <option value="prof">Concise professional</option>
          <option value="warm_prof">Warm professional</option>
          <option value="technical">Technical and direct</option>
          <option value="formal">Formal</option>
        </select>
      </label>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <Button type="submit" variant="outline" disabled={busy}>{busy ? 'Saving…' : 'Save writing style'}</Button>
    </form>
  )
}
