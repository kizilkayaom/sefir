import { useState } from 'react'
import type { FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { useUnsavedChanges } from '@/hooks/use-unsaved-changes'
import { api, errorMessage } from '@/lib/api'
import type { WritingStyleProfile } from '@/types/api'

export function WritingProfileEditor({ profile, onSaved, onCancel }: {
  profile: WritingStyleProfile
  onSaved: (profile: WritingStyleProfile) => void
  onCancel: () => void
}) {
  const initialMarkdown = [
    '---',
    `format_version: ${profile.format_version}`,
    `name: ${JSON.stringify(profile.name)}`,
    `language: ${JSON.stringify(profile.language)}`,
    '---',
    profile.content,
  ].join('\n')
  const [markdown, setMarkdown] = useState(initialMarkdown)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const dirty = markdown !== initialMarkdown
  const size = new TextEncoder().encode(markdown).length
  const valid = markdown.trim().length > 0 && size <= 32 * 1024
  const discardMessage = 'Discard your unsaved writing profile changes?'
  useUnsavedChanges(dirty, discardMessage)

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || !dirty || !valid) return
    setBusy(true)
    setError('')
    try {
      const saved = await api<WritingStyleProfile>(
        `/api/settings/writing-styles/${encodeURIComponent(profile.profile_id)}`,
        {
          method: 'PUT',
          body: JSON.stringify({ expected_revision: profile.revision, markdown }),
        },
      )
      onSaved(saved)
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} aria-label="Edit writing profile" className="space-y-4 rounded-xl border bg-card p-4">
      <div>
        <h4 className="font-semibold">Edit writing profile</h4>
        <p className="mt-1 text-sm text-muted-foreground">Update the name, language, and writing guidance. Saving updates revision {profile.revision}.</p>
      </div>
      <div>
        <label htmlFor="writing-profile-markdown" className="block text-sm font-medium">Markdown profile</label>
        <textarea
          id="writing-profile-markdown"
          className="field mt-2 min-h-80 font-mono"
          aria-describedby="writing-profile-markdown-help"
          required
          spellCheck={false}
          disabled={busy}
          value={markdown}
          onChange={(event) => setMarkdown(event.target.value)}
        />
        <p id="writing-profile-markdown-help" className="mt-2 text-sm text-muted-foreground">Keep the YAML header between the --- lines. The complete profile can be up to 32 KiB, with up to 16 KiB of writing guidance.</p>
      </div>
      {!valid && <p role="status" className="text-sm text-destructive">{size > 32 * 1024 ? 'Profile must not exceed 32 KiB.' : 'Profile must not be empty.'}</p>}
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={busy || !dirty || !valid}>{busy ? 'Saving…' : 'Save profile'}</Button>
        <Button type="button" variant="outline" disabled={busy} onClick={() => {
          if (dirty && !window.confirm(discardMessage)) return
          onCancel()
        }}>Cancel editing</Button>
      </div>
    </form>
  )
}
