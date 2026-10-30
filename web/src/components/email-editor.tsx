import { lazy, Suspense } from 'react'
import type { EmailEditorProps } from '@/components/email-editor-content'

const Editor = lazy(() => import('@/components/email-editor-content').then((module) => ({ default: module.EmailEditorContent })))

export function EmailEditor(props: EmailEditorProps) {
  return (
    <Suspense fallback={<p role="status" className="min-h-64 rounded-lg border p-4 text-muted-foreground">Loading email editor…</p>}>
      <Editor {...props} />
    </Suspense>
  )
}
