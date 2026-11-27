import { safeEmailHTML } from '@/lib/email-content'
import type { Draft, PolishSuggestion } from '@/types/draft'

type PolishComparisonProps = {
  draft: Draft
  suggestion: PolishSuggestion
}

export function PolishComparison({ draft, suggestion }: PolishComparisonProps) {
  const versions = [
    { label: 'Original', email: draft },
    { label: 'Suggested wording', email: suggestion },
  ]

  return (
    <section aria-label="Email wording comparison" className="space-y-4">
      <p className="text-sm leading-relaxed text-muted-foreground">
        Review names, facts, dates, and links before accepting. This suggestion has not been saved or sent.
      </p>
      <div className="grid gap-4 xl:grid-cols-2">
        {versions.map(({ label, email }) => (
          <article key={label} aria-label={label} className="min-w-0 rounded-xl border bg-card p-6 text-card-foreground">
            <h2 className="font-semibold">{label}</h2>
            <h3 className="mt-4 break-words text-xl font-semibold">{email.subject}</h3>
            {email.body_html ? (
              <div className="email-content mt-6 break-words" dangerouslySetInnerHTML={{ __html: safeEmailHTML(email.body_html) }} />
            ) : (
              <p className="mt-6 whitespace-pre-wrap break-words leading-relaxed">{email.body}</p>
            )}
          </article>
        ))}
      </div>
    </section>
  )
}
