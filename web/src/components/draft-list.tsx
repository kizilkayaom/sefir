import { cn } from '@/lib/utils'
import type { Draft } from '@/types/draft'

type DraftListProps = {
  drafts: Draft[]
  selectedDraft: Draft | null
  onSelect: (draft: Draft) => void
}

export function DraftList({ drafts, selectedDraft, onSelect }: DraftListProps) {
  if (drafts.length === 0) {
    return (
      <div className="rounded-xl border bg-card p-8 text-card-foreground">
        <p>No drafts loaded</p>
      </div>
    )
  }

  return (
    <ul className="space-y-3">
      {drafts.map((draft) => {
        const isSelected =
          selectedDraft?.draft_id === draft.draft_id &&
          selectedDraft?.version === draft.version

        return (
          <li key={`${draft.draft_id}:${draft.version}`}>
            <button
              type="button"
              onClick={() => onSelect(draft)}
              aria-pressed={isSelected}
              className={cn(
                'w-full rounded-xl border bg-card p-6 text-left text-card-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                isSelected ? 'border-primary' : 'border-border',
              )}
            >
              <span className="block break-words font-semibold">
                {draft.subject}
              </span>
              <span className="mt-1 block break-words text-sm text-muted-foreground">
                {draft.recipient_email ?? 'Missing recipient'}
              </span>
              <span className="mt-3 block text-sm text-muted-foreground">
                Version {draft.version}
              </span>
            </button>
          </li>
        )
      })}
    </ul>
  )
}
