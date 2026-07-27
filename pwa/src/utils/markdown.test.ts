import { describe, expect, it } from 'vitest'
import { isMarkdownBubble, renderMarkdown } from './markdown'

describe('isMarkdownBubble', () => {
  it('assistant text', () => {
    expect(isMarkdownBubble({ role: 'assistant' })).toBe(true)
    expect(isMarkdownBubble({ role: 'assistant', type: 'text' })).toBe(true)
    expect(isMarkdownBubble({ role: 'assistant', type: 'tool_call' })).toBe(false)
  })
  it('user text', () => {
    expect(isMarkdownBubble({ role: 'user', type: 'text' })).toBe(true)
    expect(isMarkdownBubble({ role: 'system' })).toBe(false)
  })
})

describe('renderMarkdown', () => {
  it('empty', () => {
    expect(renderMarkdown('')).toBe('')
  })
  it('renders bold and strips script', () => {
    const html = renderMarkdown('**hi** <script>alert(1)</script>')
    expect(html).toContain('<strong>')
    expect(html.toLowerCase()).not.toContain('<script')
  })
  it('strips phishing forms', () => {
    const html = renderMarkdown('<form action="https://evil.test"><input name="pw"><button>x</button></form>')
    expect(html.toLowerCase()).not.toContain('<form')
    expect(html.toLowerCase()).not.toContain('<input')
  })
  it('code fence', () => {
    const html = renderMarkdown('```js\nconst a=1\n```')
    expect(html).toContain('<code')
  })
})
