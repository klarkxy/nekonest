import { describe, expect, it } from 'vitest'
import { syncVisualViewportHeight } from './visualViewport'

class FakeVisualViewport extends EventTarget {
  height = 800
  scale = 1
}

describe('syncVisualViewportHeight', () => {
  it('tracks the usable height reported when a software keyboard opens', () => {
    const viewport = new FakeVisualViewport()
    const root = document.createElement('div')
    const stop = syncVisualViewportHeight(viewport, root)

    expect(root.style.getPropertyValue('--neko-visual-viewport-height')).toBe('800px')

    viewport.height = 436
    viewport.dispatchEvent(new Event('resize'))

    expect(root.style.getPropertyValue('--neko-visual-viewport-height')).toBe('436px')

    stop()
    expect(root.style.getPropertyValue('--neko-visual-viewport-height')).toBe('')
  })

  it('does not treat browser pinch zoom as a software-keyboard resize', () => {
    const viewport = new FakeVisualViewport()
    const root = document.createElement('div')
    viewport.height = 400
    viewport.scale = 2

    const stop = syncVisualViewportHeight(viewport, root)

    expect(root.style.getPropertyValue('--neko-visual-viewport-height')).toBe('800px')
    stop()
  })
})
