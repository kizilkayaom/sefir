import type { Draft } from '@/types/draft'
import { safeEmailHTML } from '@/lib/email-content'

type DraftPreviewProps = {
  draft: Draft | null
}

export function DraftPreview({ draft }: DraftPreviewProps) {
  if (draft === null) {
    return (
      <div className="rounded-xl border bg-card p-6 text-card-foreground">
        <p>Select a draft to review.</p>
      </div>
    )
  }

  return (
    <article className="rounded-xl border bg-card p-6 text-card-foreground">
      <h2 className="break-words text-xl font-semibold">{draft.subject}</h2>
      <dl className="mt-4 space-y-3 text-sm">
        <div>
          <dt className="text-muted-foreground">To</dt>
          <dd className="break-words">
            {draft.recipient_email ?? 'Missing recipient'}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Version</dt>
          <dd>{draft.version}</dd>
        </div>
      </dl>
      {draft.body_html ? (
        <div className="email-content mt-6" dangerouslySetInnerHTML={{ __html: safeEmailHTML(draft.body_html) }} />
      ) : (
        <p className="mt-6 whitespace-pre-wrap break-words leading-relaxed">{draft.body}</p>
      )}
    </article>
  )
}
