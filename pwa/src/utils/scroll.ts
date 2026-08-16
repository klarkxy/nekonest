/**
 * Chat-follow scrolling helpers. Kept numeric so the pin decision is
 * unit-testable without a layout engine; DOM wrappers stay trivial.
 */
export const NEAR_BOTTOM_THRESHOLD_PX = 96

export function isNearBottomValue(
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
  threshold: number = NEAR_BOTTOM_THRESHOLD_PX
): boolean {
  if (clientHeight <= 0) return true
  return scrollHeight - scrollTop - clientHeight <= threshold
}

export function isNearBottom(el: HTMLElement, threshold: number = NEAR_BOTTOM_THRESHOLD_PX): boolean {
  return isNearBottomValue(el.scrollTop, el.clientHeight, el.scrollHeight, threshold)
}

export function pinToBottom(el: HTMLElement): void {
  el.scrollTop = el.scrollHeight
}
