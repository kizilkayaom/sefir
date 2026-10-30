import DOMPurify from 'dompurify'

export type EmailContent = { html: string; text: string }

export function safeEmailHTML(html: string) {
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS: ['p', 'br', 'strong', 'b', 'em', 'i', 'u', 's', 'strike', 'ul', 'ol', 'li', 'blockquote', 'a'],
    ALLOWED_ATTR: ['href', 'rel', 'start'],
    ALLOWED_URI_REGEXP: /^(https?:\/\/|mailto:)/i,
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
  })
}

export function textToEmailHTML(text: string) {
  const escaped = text.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
  return escaped.replace(/\r\n?/g, '\n').split('\n\n').map((paragraph) => `<p>${paragraph.replaceAll('\n', '<br>')}</p>`).join('')
}

export function validEmailLink(value: string) {
  if (!/^(https?:\/\/|mailto:)/i.test(value) || /\s/u.test(value) ||
    Array.from(value).some((character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127)) return false
  try {
    const url = new URL(value)
    return url.protocol === 'mailto:' ? url.pathname !== '' : url.hostname !== '' && !url.username && !url.password
  } catch {
    return false
  }
}

export function emailFits(content: EmailContent) {
  const encoder = new TextEncoder()
  return !!content.text.trim() && encoder.encode(content.text).length <= 100000 && encoder.encode(content.html).length <= 200000
}
