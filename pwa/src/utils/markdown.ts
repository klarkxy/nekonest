import { marked } from 'marked'
import DOMPurify from 'dompurify'
import type { Config as DOMPurifyConfig } from 'dompurify'

marked.setOptions({
  gfm: true,
  breaks: true
})

/** Allowlist: no forms/inputs/styles that enable credential phishing via v-html. */
const PURIFY: DOMPurifyConfig = {
  ALLOWED_TAGS: [
    'a', 'p', 'br', 'hr', 'pre', 'code', 'blockquote',
    'ul', 'ol', 'li', 'strong', 'em', 'b', 'i', 'u', 's', 'del', 'ins',
    'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
    'table', 'thead', 'tbody', 'tr', 'th', 'td',
    'span', 'div', 'img', 'sup', 'sub'
  ],
  ALLOWED_ATTR: [
    'href', 'title', 'target', 'rel', 'class',
    'src', 'alt', 'width', 'height'
  ],
  ALLOW_DATA_ATTR: false,
  // Block javascript: / data: navigation
  ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i
}

export function renderMarkdown(src: string): string {
  if (!src) return ''
  const html = marked.parse(src, { async: false }) as string
  return DOMPurify.sanitize(html, PURIFY)
}

export function isMarkdownBubble(msg: { role: string; type?: string }): boolean {
  if (msg.role === 'assistant') {
    return !msg.type || msg.type === 'text' || msg.type === 'assistant'
  }
  // user text can also benefit from light md (code fences etc.)
  if (msg.role === 'user') {
    return !msg.type || msg.type === 'text'
  }
  return false
}
