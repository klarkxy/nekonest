export type NotificationIdentity = {
  tag?: string
  device_id?: string
  session_id?: string
}

/** Keep a complete server tag, or derive one that cannot collide across sessions. */
export function notificationTag(data: NotificationIdentity): string {
  const supplied = data.tag?.trim()
  if (supplied) return supplied
  return [
    'nekonest',
    data.device_id || 'unknown-device',
    data.session_id || 'general'
  ].join(':')
}
