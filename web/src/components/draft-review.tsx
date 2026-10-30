import { useEffect, useState } from 'react'

import { DraftPreview } from '@/components/draft-preview'
import { DraftEditor } from '@/components/draft-editor'
import { Button } from '@/components/ui/button'
import { useResource } from '@/hooks/use-resource'
import { api, errorMessage } from '@/lib/api'
import type { DraftStatus } from '@/types/api'
import type { Draft } from '@/types/draft'

export function DraftReview({ draft, onSaved, onDirtyChange }: {
  draft: Draft
  onSaved: (draft: Draft) => void
  onDirtyChange: (dirty: boolean) => void
}) {
  const path = `/api/drafts/${draft.draft_id}/versions/${draft.version}`
  const status = useResource<DraftStatus>(`${path}/status`)
  const [reviewed, setReviewed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [submitted, setSubmitted] = useState(false)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  const delivery = status.data?.delivery
  const workflow = status.data?.workflow
  const canEdit = !busy && !submitted && !status.loading && !!status.data &&
    workflow?.status !== 'RUNNING' && !['pending', 'sending'].includes(delivery?.state ?? '')
  const canApprove = !!draft.recipient_email && !delivery && !submitted &&
    ['NOT_STARTED', 'RUNNING'].includes(workflow?.status ?? '')

  useEffect(() => {
    if (workflow?.status !== 'RUNNING' && !['pending', 'sending'].includes(delivery?.state ?? '')) return
    const timer = window.setTimeout(status.reload, 5000)
    return () => window.clearTimeout(timer)
  }, [workflow, delivery, status.reload])

  async function approve() {
    if (!reviewed || !canApprove || busy) return
    setBusy(true)
    setError('')
    try {
      await api(`${path}/approve`, {
        method: 'POST',
        body: JSON.stringify({ content_hash: draft.content_hash }),
      })
      setSubmitted(true)
    } catch (error) {
      setError(errorMessage(error))
    } finally {
      setBusy(false)
      status.reload()
    }
  }

  if (editing) return (
    <DraftEditor draft={draft} onSaved={onSaved} onCancel={() => setEditing(false)} onDirtyChange={onDirtyChange} />
  )

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button variant="outline" disabled={!canEdit} onClick={() => { setReviewed(false); setEditing(true) }}>Edit draft</Button>
      </div>
      <DraftPreview draft={draft} />
      <section aria-label="Approval and delivery" className="space-y-4 rounded-xl border bg-card p-6">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="font-semibold">Approval and delivery</h2>
          <Button variant="ghost" disabled={status.loading || busy} onClick={status.reload}>Refresh status</Button>
        </div>
        {status.loading && <p role="status">Checking status…</p>}
        {status.error && <p role="alert" className="text-sm text-destructive">{status.error}</p>}
        {status.data && (
          <div role="status" className="space-y-2 text-sm">
            <p>Workflow: {workflow?.status.replaceAll('_', ' ').toLowerCase() ?? 'unknown'}</p>
            {delivery && <p>Delivery: <strong>{delivery.state.replaceAll('_', ' ')}</strong></p>}
            {delivery?.gmail_message_id && <p className="break-all text-muted-foreground">Gmail message: {delivery.gmail_message_id}</p>}
            {delivery?.last_error && <p className="text-destructive">{delivery.last_error}</p>}
            {workflow?.failure && <p className="text-destructive">{workflow.failure}</p>}
          </div>
        )}
        {submitted && !delivery && <p role="status" className="text-sm">Approval submitted. Waiting for the worker.</p>}
        {!draft.recipient_email && <p className="text-sm text-destructive">Edit this draft and enter a recipient before approving.</p>}
        {canApprove && (
          <label className="flex items-start gap-3 text-sm">
            <input type="checkbox" className="mt-1" checked={reviewed} onChange={(event) => setReviewed(event.target.checked)} />
            <span>I reviewed version {draft.version} and its recipient, {draft.recipient_email}.</span>
          </label>
        )}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <Button disabled={!canApprove || !reviewed || busy || status.loading} onClick={approve}>
          {busy ? 'Submitting approval…' : 'Approve and send'}
        </Button>
        <p className="text-xs text-muted-foreground">Sent means Gmail accepted the message; it does not confirm inbox delivery.</p>
      </section>
    </div>
  )
}
