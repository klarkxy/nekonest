/** The in-progress thinking bubble is the last message while the turn streams. */
export function liveThinkingMessageId(
  messages: Array<{ id: string; type?: string }>,
  streaming: boolean
): string {
  if (!streaming) return ''
  const last = messages[messages.length - 1]
  return last?.type === 'thinking' ? last.id : ''
}
