import { useEffect, useId, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { api, errorMessage } from '@/lib/api'
import type { ExtractedResume } from '@/types/api'

type ResumeInputProps = {
  value: string
  onChange: (text: string) => void
  disabled: boolean
  onExtractingChange: (extracting: boolean) => void
}

export function ResumeInput({ value, onChange, disabled, onExtractingChange }: ResumeInputProps) {
  const id = useId()
  const [file, setFile] = useState<File | null>(null)
  const [extracting, setExtracting] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const fileInput = useRef<HTMLInputElement | null>(null)
  const activeRequest = useRef<AbortController | null>(null)
  const locked = disabled || extracting
  const textBytes = new TextEncoder().encode(value).length
  const textTooLarge = textBytes > 100000

  useEffect(() => () => {
    activeRequest.current?.abort()
    activeRequest.current = null
    onExtractingChange(false)
  }, [onExtractingChange])

  async function extract() {
    if (disabled || !file || activeRequest.current) return
    setError('')
    setMessage('')
    if (!file.name.toLowerCase().endsWith('.pdf')) {
      setError('Choose a PDF (.pdf) file.')
      return
    }
    if (file.size === 0) {
      setError('PDF file must not be empty.')
      return
    }
    if (file.size > 5 * 1024 * 1024) {
      setError('PDF file must not exceed 5 MiB.')
      return
    }

    const controller = new AbortController()
    activeRequest.current = controller
    setExtracting(true)
    onExtractingChange(true)
    try {
      const body = new FormData()
      body.append('resume', file)
      const result = await api<ExtractedResume>('/api/resume/extract', {
        method: 'POST', body, signal: controller.signal,
      })
      if (controller.signal.aborted || activeRequest.current !== controller) return
      if (!result || typeof result.text !== 'string' || !result.text.trim() ||
        new TextEncoder().encode(result.text).length > 100000 ||
        !Number.isInteger(result.page_count) || result.page_count < 1 || result.page_count > 20) {
        throw new Error('The server returned invalid résumé text. Please try again.')
      }
      onChange(result.text)
      setFile(null)
      if (fileInput.current) fileInput.current.value = ''
      setMessage(`Extracted text from ${result.page_count} ${result.page_count === 1 ? 'page' : 'pages'}. Review and edit it before generating a draft.`)
    } catch (error) {
      if (!controller.signal.aborted && activeRequest.current === controller) setError(errorMessage(error))
    } finally {
      if (!controller.signal.aborted && activeRequest.current === controller) {
        activeRequest.current = null
        setExtracting(false)
        onExtractingChange(false)
      }
    }
  }

  return (
    <section aria-label="Résumé input" className="min-w-0 space-y-4">
      <div className="space-y-3 rounded-lg border bg-muted/30 p-4" aria-busy={extracting}>
        <label htmlFor={`${id}-file`} className="block text-sm font-medium">Résumé PDF</label>
        <input
          ref={fileInput}
          id={`${id}-file`}
          type="file"
          accept=".pdf,application/pdf"
          className="field min-w-0"
          disabled={locked}
          aria-describedby={`${id}-help`}
          onChange={(event) => {
            if (locked || activeRequest.current) return
            setFile(event.target.files?.[0] ?? null)
            setError('')
            setMessage('')
          }}
        />
        <p id={`${id}-help`} className="text-sm leading-6 text-muted-foreground">
          Upload a PDF with selectable text, up to 5 MiB, or paste your résumé below. The PDF is processed on this server and isn’t saved. Review the extracted text before generating a draft.
        </p>
        <Button type="button" variant="outline" disabled={locked || !file} onClick={extract}>
          {extracting ? 'Extracting…' : value.trim() ? 'Replace résumé text' : 'Extract résumé text'}
        </Button>
        {extracting && <p role="status" className="text-sm">Extracting résumé text…</p>}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        {message && <p role="status" className="text-sm">{message}</p>}
      </div>
      <div>
        <label htmlFor={`${id}-text`} className="block text-sm font-medium">Résumé text</label>
        <textarea
          id={`${id}-text`}
          className="field mt-2 min-h-64"
          required
          maxLength={100000}
          disabled={locked}
          value={value}
          aria-invalid={textTooLarge}
          aria-describedby={`${id}-text-help`}
          onChange={(event) => onChange(event.target.value)}
          placeholder="Paste your résumé as plain text, or upload a PDF above."
        />
        <p id={`${id}-text-help`} role={textTooLarge ? 'alert' : undefined} className={`mt-2 text-sm ${textTooLarge ? 'text-destructive' : 'text-muted-foreground'}`}>
          {textTooLarge ? 'Keep résumé text within 100,000 bytes.' : 'You can edit this text before generating a draft.'}
        </p>
      </div>
    </section>
  )
}
