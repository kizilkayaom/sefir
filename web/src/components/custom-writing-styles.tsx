import { useState } from 'react'
import type { FormEvent } from 'react'

import { PageControls } from '@/components/page-controls'
import { Button } from '@/components/ui/button'
import { useResource } from '@/hooks/use-resource'
import { api, errorMessage } from '@/lib/api'
import type { Page, WritingStyleProfile } from '@/types/api'

export function CustomWritingStyles() {
  const [offset, setOffset] = useState(0)
  const [file, setFile] = useState<File | null>(null)
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState('')
  const [uploadMessage, setUploadMessage] = useState('')
  const [deletingID, setDeletingID] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState('')
  const [deleteMessage, setDeleteMessage] = useState('')
  const busy = uploading || deletingID !== null
  const profiles = useResource<Page<WritingStyleProfile>>(
    `/api/settings/writing-styles?limit=25&offset=${offset}`,
  )

  async function upload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busy || !file) return
    const form = event.currentTarget
    setUploadError('')
    setUploadMessage('')
    setDeleteError('')
    setDeleteMessage('')
    if (!file.name.toLowerCase().endsWith('.md')) {
      setUploadError('Choose a Markdown (.md) file.')
      return
    }
    if (file.size === 0) {
      setUploadError('Profile file must not be empty.')
      return
    }
    if (file.size > 32 * 1024) {
      setUploadError('Profile file must not exceed 32 KiB.')
      return
    }
    setUploading(true)
    try {
      const body = new FormData()
      body.append('profile', file)
      await api<WritingStyleProfile>('/api/settings/writing-styles', { method: 'POST', body })
      form.reset()
      setFile(null)
      setUploadMessage('Writing profile uploaded.')
      if (offset === 0) profiles.reload()
      else setOffset(0)
    } catch (error) {
      setUploadError(errorMessage(error))
    } finally {
      setUploading(false)
    }
  }

  async function remove(profile: WritingStyleProfile) {
    if (busy || !window.confirm(`Delete writing profile "${profile.name}"? This cannot be undone.`)) return
    setDeleteError('')
    setDeleteMessage('')
    setUploadError('')
    setUploadMessage('')
    setDeletingID(profile.profile_id)
    try {
      await api(`/api/settings/writing-styles/${profile.profile_id}`, { method: 'DELETE' })
      setDeleteMessage('Writing profile deleted.')
      if (offset > 0 && profiles.data?.items.length === 1) setOffset(offset - 25)
      else profiles.reload()
    } catch (error) {
      setDeleteError(errorMessage(error))
    } finally {
      setDeletingID(null)
    }
  }

  return (
    <section aria-labelledby="custom-writing-styles-heading" className="space-y-3 border-t pt-4">
      <h3 id="custom-writing-styles-heading" className="font-semibold">Custom writing profiles</h3>
      <form onSubmit={upload} aria-label="Upload writing profile" className="space-y-3">
        <label htmlFor="writing-profile-file" className="block text-sm font-medium">Markdown profile</label>
        <input
          id="writing-profile-file"
          type="file"
          accept=".md"
          className="field"
          aria-describedby="writing-profile-file-help"
          disabled={busy}
          onChange={(event) => {
            setFile(event.target.files?.[0] ?? null)
            setUploadError('')
            setUploadMessage('')
          }}
        />
        <p id="writing-profile-file-help" className="text-sm text-muted-foreground">Choose a Markdown (.md) writing profile, up to 32 KiB.</p>
        <a
          href="/writing-profile-example.md"
          download="writing-profile-example.md"
          className="block text-sm text-primary underline underline-offset-4"
        >
          Download example profile
        </a>
        {uploadError && <p role="alert" className="text-sm text-destructive">{uploadError}</p>}
        {uploadMessage && <p role="status" className="text-sm">{uploadMessage}</p>}
        <Button type="submit" variant="outline" disabled={busy || !file}>{uploading ? 'Uploading…' : 'Upload profile'}</Button>
      </form>
      {deleteError && <p role="alert" className="text-sm text-destructive">{deleteError}</p>}
      {deleteMessage && <p role="status" className="text-sm">{deleteMessage}</p>}
      <PageControls
        offset={offset}
        hasMore={profiles.data?.has_more ?? false}
        loading={profiles.loading || busy}
        onPage={setOffset}
        onRefresh={profiles.reload}
      />
      {profiles.loading && <p role="status">Loading writing profiles…</p>}
      {profiles.error && (
        <div role="alert" className="space-y-2">
          <p className="text-destructive">{profiles.error}</p>
          <Button type="button" variant="outline" disabled={busy} onClick={profiles.reload}>Retry</Button>
        </div>
      )}
      {profiles.data?.items.length === 0 && (
        <p className="text-sm text-muted-foreground">No custom writing profiles yet.</p>
      )}
      {profiles.data && profiles.data.items.length > 0 && (
        <ul className="space-y-3">
          {profiles.data.items.map((profile) => (
            <li key={profile.profile_id} className="min-w-0 space-y-2 rounded-lg border p-4 [overflow-wrap:anywhere]">
              <h4 className="font-medium">{profile.name}</h4>
              <p className="text-sm text-muted-foreground">{profile.language} · Revision {profile.revision}</p>
              <details>
                <summary className="cursor-pointer rounded text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                  Preview writing style
                </summary>
                <pre className="mt-3 whitespace-pre-wrap rounded-md bg-muted p-3 text-sm">{profile.content}</pre>
              </details>
              <Button type="button" variant="destructive" disabled={busy} onClick={() => remove(profile)}>
                {deletingID === profile.profile_id ? 'Deleting…' : 'Delete profile'}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
