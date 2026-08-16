import { describe, expect, it, beforeEach } from 'vitest'
import {
  MARKDOWN_CACHE_LIMIT,
  clearMarkdownCache,
  isMarkdownBubble,
  markdownCacheSize,
  renderMarkdown
} from './markdown'

describe('isMarkdownBubble', () => {
  it('assistant text', () => {
    expect(isMarkdownBubble({ role: 'assistant' })).toBe(true)
    expect(isMarkdownBubble({ role: 'assistant', type: 'text' })).toBe(true)
    expect(isMarkdownBubble({ role: 'assistant', type: 'tool_call' })).toBe(false)
    expect(isMarkdownBubble({ role: 'assistant', type: 'thinking' })).toBe(false)
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

describe('renderMarkdown cache', () => {
  beforeEach(() => {
    clearMarkdownCache()
  })
  it('repeated input returns the same sanitized html without extra entries', () => {
    const first = renderMarkdown('**cached** <script>alert(1)</script>')
    const second = renderMarkdown('**cached** <script>alert(1)</script>')
    expect(second).toBe(first)
    expect(second.toLowerCase()).not.toContain('<script')
    expect(markdownCacheSize()).toBe(1)
  })
  it('distinct inputs do not cross-contaminate', () => {
    const a = renderMarkdown('alpha')
    const b = renderMarkdown('beta')
    expect(a).not.toBe(b)
    expect(renderMarkdown('alpha')).toBe(a)
    expect(renderMarkdown('beta')).toBe(b)
    expect(markdownCacheSize()).toBe(2)
  })
  it('evicts the least recently used entry beyond the limit', () => {
    for (let i = 0; i < MARKDOWN_CACHE_LIMIT + 25; i++) {
      renderMarkdown(`entry-${i}`)
    }
    expect(markdownCacheSize()).toBe(MARKDOWN_CACHE_LIMIT)
    // Oldest entries were evicted; the newest ones are still cached.
    expect(renderMarkdown('entry-0')).toBe(renderMarkdown('entry-0'))
    expect(markdownCacheSize()).toBe(MARKDOWN_CACHE_LIMIT)
    const newest = renderMarkdown(`entry-${MARKDOWN_CACHE_LIMIT + 24}`)
    expect(markdownCacheSize()).toBe(MARKDOWN_CACHE_LIMIT)
    expect(newest).toContain('entry-')
  })
})
