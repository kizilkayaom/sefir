import { EditorContent, useEditor, useEditorState } from '@tiptap/react'
import StarterKit from '@tiptap/starter-kit'
import { Bold, Italic, Underline, Strikethrough, List, ListOrdered, Quote, Link, Undo2, Redo2, RemoveFormatting } from 'lucide-react'
import { useEffect, useId, useState } from 'react'

import { Button } from '@/components/ui/button'
import { safeEmailHTML, validEmailLink } from '@/lib/email-content'
import type { EmailContent } from '@/lib/email-content'

export type EmailEditorProps = {
  initialHTML?: string
  disabled?: boolean
  onChange: (content: EmailContent) => void
}

export function EmailEditorContent({ initialHTML = '', disabled = false, onChange }: EmailEditorProps) {
  const labelID = useId()
  const [linkOpen, setLinkOpen] = useState(false)
  const [linkURL, setLinkURL] = useState('')
  const editor = useEditor({
    extensions: [StarterKit.configure({
      heading: false,
      code: false,
      codeBlock: false,
      horizontalRule: false,
      trailingNode: false,
      link: {
        openOnClick: false,
        autolink: false,
        isAllowedUri: validEmailLink,
        HTMLAttributes: { target: null, rel: 'nofollow noreferrer' },
      },
    })],
    content: safeEmailHTML(initialHTML),
    injectCSS: false,
    editorProps: {
      attributes: {
        role: 'textbox',
        'aria-labelledby': labelID,
        'aria-multiline': 'true',
        class: 'email-content min-h-64 px-4 py-3 outline-none',
        spellcheck: 'true',
      },
    },
    onUpdate: ({ editor }) => onChange({ html: editor.getHTML(), text: editor.getText() }),
  })
  const state = useEditorState({
    editor,
    selector: ({ editor }) => editor ? {
      bold: editor.isActive('bold'),
      italic: editor.isActive('italic'),
      underline: editor.isActive('underline'),
      strike: editor.isActive('strike'),
      bullets: editor.isActive('bulletList'),
      numbers: editor.isActive('orderedList'),
      quote: editor.isActive('blockquote'),
      link: editor.isActive('link'),
      undo: editor.can().undo(),
      redo: editor.can().redo(),
    } : null,
  })

  useEffect(() => {
    editor?.setEditable(!disabled)
  }, [editor, disabled])

  const actions = [
    { label: 'Bold', icon: Bold, active: state?.bold, run: () => editor?.chain().focus().toggleBold().run() },
    { label: 'Italic', icon: Italic, active: state?.italic, run: () => editor?.chain().focus().toggleItalic().run() },
    { label: 'Underline', icon: Underline, active: state?.underline, run: () => editor?.chain().focus().toggleUnderline().run() },
    { label: 'Strikethrough', icon: Strikethrough, active: state?.strike, run: () => editor?.chain().focus().toggleStrike().run() },
    { label: 'Bullet list', icon: List, active: state?.bullets, run: () => editor?.chain().focus().toggleBulletList().run() },
    { label: 'Numbered list', icon: ListOrdered, active: state?.numbers, run: () => editor?.chain().focus().toggleOrderedList().run() },
    { label: 'Quote', icon: Quote, active: state?.quote, run: () => editor?.chain().focus().toggleBlockquote().run() },
    { label: 'Link', icon: Link, active: state?.link, run: () => {
      setLinkURL(editor?.getAttributes('link').href ?? '')
      setLinkOpen(true)
    } },
    { label: 'Clear formatting', icon: RemoveFormatting, run: () => editor?.chain().focus().unsetAllMarks().clearNodes().run() },
    { label: 'Undo', icon: Undo2, unavailable: !state?.undo, run: () => editor?.chain().focus().undo().run() },
    { label: 'Redo', icon: Redo2, unavailable: !state?.redo, run: () => editor?.chain().focus().redo().run() },
  ]

  return (
    <div className="space-y-2">
      <span id={labelID} className="block text-sm font-medium">Email body</span>
      <div className="overflow-hidden rounded-lg border bg-background focus-within:ring-2 focus-within:ring-ring">
        <div role="toolbar" aria-label="Email formatting" className="flex flex-wrap gap-1 border-b bg-muted/50 p-2">
          {actions.map(({ label, icon: Icon, active, unavailable, run }) => (
            <Button
              key={label}
              type="button"
              size="icon-sm"
              variant={active ? 'secondary' : 'ghost'}
              aria-label={label}
              title={label}
              aria-pressed={active}
              disabled={disabled || !editor || unavailable}
              onMouseDown={(event) => event.preventDefault()}
              onClick={run}
            >
              <Icon aria-hidden="true" className="size-4" />
            </Button>
          ))}
        </div>
        {linkOpen && (
          <div className="space-y-2 border-b p-3">
            <label className="block text-sm font-medium">
              Link URL
              <input className="field mt-1" type="url" placeholder="https://example.com" value={linkURL} disabled={disabled} onChange={(event) => setLinkURL(event.target.value)} onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault()
                  if (validEmailLink(linkURL.trim())) {
                    editor?.chain().focus().extendMarkRange('link').setLink({ href: linkURL.trim() }).run()
                    setLinkOpen(false)
                  }
                }
              }} />
            </label>
            <p className="text-xs text-muted-foreground">Select text to link. Use an http://, https://, or mailto: address.</p>
            {linkURL && !validEmailLink(linkURL.trim()) && <p role="alert" className="text-sm text-destructive">Enter a valid web or email link.</p>}
            <div className="flex flex-wrap gap-2">
              <Button type="button" size="sm" disabled={disabled || !validEmailLink(linkURL.trim())} onClick={() => {
                editor?.chain().focus().extendMarkRange('link').setLink({ href: linkURL.trim() }).run()
                setLinkOpen(false)
              }}>Apply link</Button>
              <Button type="button" size="sm" variant="outline" disabled={disabled} onClick={() => {
                editor?.chain().focus().extendMarkRange('link').unsetLink().run()
                setLinkOpen(false)
              }}>Remove link</Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setLinkOpen(false)}>Cancel link</Button>
            </div>
          </div>
        )}
        <EditorContent editor={editor} />
      </div>
      <p className="text-xs text-muted-foreground">Use the toolbar or keyboard shortcuts to format your email. Shift+Enter adds a line break.</p>
    </div>
  )
}
