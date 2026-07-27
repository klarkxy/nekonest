import { describe, expect, it } from 'vitest'
import { isImageMime } from './attachments'

describe('isImageMime', () => {
  it('detects images', () => {
    expect(isImageMime('image/png')).toBe(true)
    expect(isImageMime('text/plain')).toBe(false)
  })
})
