import { useState } from 'react'

import { DraftList } from '@/components/draft-list'
import { DraftPreview } from '@/components/draft-preview'
import { DraftReview } from '@/components/draft-review'
import { PageControls } from '@/components/page-controls'
import { Button } from '@/components/ui/button'
import { useResource } from '@/hooks/use-resource'
import { cn } from '@/lib/utils'
import type { Page } from '@/types/api'
import type { Draft } from '@/types/draft'

export function DraftsPage({ initialDraft = null, onSaved }: {
  initialDraft?: Draft | null
  onSaved: (draft: Draft) => void
}) {
  const [offset, setOffset] = useState(0)
  const [selected, setSelected] = useState<Draft | null>(initialDraft)
  const [hasEdits, setHasEdits] = useState(false)
  const drafts = useResource<Page<Draft>>(`/api/drafts?limit=25&offset=${offset}`)
  const detail = useResource<Draft>(selected ? `/api/drafts/${selected.draft_id}/versions/${selected.version}` : null)

  function selectDraft(next: Draft | null) {
    if (selected?.draft_id === next?.draft_id && selected?.version === next?.version) return
    if (hasEdits && !window.confirm('Discard your unsaved email changes?')) return
    setHasEdits(false)
    setSelected(next)
  }

  return (
    <>
      <h1 className="text-3xl font-semibold">Drafts</h1>
      <p className="mt-2 text-muted-foreground">Review your outreach before sending.</p>
      <div className="mt-6 grid items-start gap-6 lg:grid-cols-3">
        <div className={cn('min-w-0', selected !== null && 'hidden lg:block')}>
          <PageControls offset={offset} hasMore={drafts.data?.has_more ?? false} loading={drafts.loading} onPage={setOffset} onRefresh={drafts.reload} />
          {drafts.loading && <p role="status">Loading drafts…</p>}
          {drafts.error && <p role="alert" className="text-destructive">{drafts.error}</p>}
          {drafts.data && <DraftList drafts={drafts.data.items} selectedDraft={selected} onSelect={selectDraft} />}
        </div>
        <div className={cn('min-w-0 lg:col-span-2', selected === null && 'hidden lg:block')}>
          {selected && <Button variant="outline" className="mb-4 lg:hidden" onClick={() => selectDraft(null)}>Back to drafts</Button>}
          {!selected && <DraftPreview draft={null} />}
          {detail.loading && <p role="status">Loading draft…</p>}
          {detail.error && <div role="alert" className="space-y-3 text-destructive"><p>{detail.error}</p><Button variant="outline" onClick={detail.reload}>Retry</Button></div>}
          {detail.data && <DraftReview
            key={`${detail.data.draft_id}:${detail.data.version}:${detail.data.content_hash}`}
            draft={detail.data}
            onDirtyChange={setHasEdits}
            onSaved={(saved) => { setHasEdits(false); setSelected(saved); setOffset(0); drafts.reload(); onSaved(saved) }}
          />}
        </div>
      </div>
    </>
  )
}
