import { useState } from 'react'

import { PageControls } from '@/components/page-controls'
import { useResource } from '@/hooks/use-resource'
import type { Activity, Page } from '@/types/api'

export function ActivityPage() {
  const [offset, setOffset] = useState(0)
  const activity = useResource<Page<Activity>>(`/api/activity?limit=25&offset=${offset}`)
  return (
    <>
      <h1 className="text-3xl font-semibold">Activity</h1>
      <p className="mt-2 text-muted-foreground">Approved emails and their delivery results.</p>
      <PageControls offset={offset} hasMore={activity.data?.has_more ?? false} loading={activity.loading} onPage={setOffset} onRefresh={activity.reload} />
      {activity.loading && <p role="status">Loading activity…</p>}
      {activity.error && <p role="alert" className="text-destructive">{activity.error}</p>}
      {activity.data?.items.length === 0 && <p>No approved emails yet.</p>}
      <ul className="space-y-3">
        {activity.data?.items.map((item) => (
          <li key={item.outbox_id} className="space-y-2 rounded-xl border bg-card p-6">
            <h2 className="break-words font-semibold">{item.subject}</h2>
            <p className="break-words text-sm">{item.recipient_email || 'Missing recipient'} · Version {item.version}</p>
            <p className="text-sm">Delivery: <strong>{item.state.replaceAll('_', ' ')}</strong> · Attempts: {item.attempts}</p>
            <p className="text-sm text-muted-foreground">Approved by {item.approved_by} · Updated {new Date(item.updated_at).toLocaleString()}</p>
            {item.last_error && <p className="break-words text-sm text-destructive">{item.last_error}</p>}
            {item.state === 'needs_attention' && <p className="text-sm">The send outcome is uncertain. Check Gmail Sent and use the recovery command before taking further action.</p>}
            {item.gmail_message_id && <p className="break-all text-xs text-muted-foreground">Gmail message: {item.gmail_message_id}</p>}
          </li>
        ))}
      </ul>
      <p className="mt-6 text-xs text-muted-foreground">Sent means Gmail accepted the message; it does not confirm inbox delivery.</p>
    </>
  )
}
