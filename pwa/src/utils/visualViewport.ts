const VIEWPORT_HEIGHT_PROPERTY = '--neko-visual-viewport-height'

type VisualViewportSize = EventTarget & {
  height: number
  scale: number
}

/**
 * Keep the app shell inside the part of the screen that is actually visible.
 * Some Android WebViews leave 100dvh unchanged when the software keyboard
 * overlays the page, while VisualViewport still reports the usable height.
 */
export function syncVisualViewportHeight(
  viewport: VisualViewportSize | null = window.visualViewport,
  root: HTMLElement = document.documentElement
): () => void {
  if (!viewport) return () => {}

  const update = () => {
    const scale = Number.isFinite(viewport.scale) && viewport.scale > 0
      ? viewport.scale
      : 1
    const height = Math.max(1, Math.round(viewport.height * scale))
    root.style.setProperty(VIEWPORT_HEIGHT_PROPERTY, `${height}px`)
  }

  update()
  viewport.addEventListener('resize', update)

  return () => {
    viewport.removeEventListener('resize', update)
    root.style.removeProperty(VIEWPORT_HEIGHT_PROPERTY)
  }
}
