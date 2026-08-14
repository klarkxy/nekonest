/** Relative last-activity labels. `nowMs` is injectable for deterministic tests. */

export function formatRelativeActivity(
  unixSeconds: number | undefined,
  nowMs: number,
  locale: string
): string {
  const ts = Number(unixSeconds)
  if (!Number.isFinite(ts) || ts <= 0) return ''
  const then = ts * 1000
  const delta = nowMs - then
  const minute = 60_000
  const hour = 60 * minute
  const day = 24 * hour
  const zh = locale.toLowerCase().startsWith('zh')

  if (delta < minute) return zh ? '刚刚' : 'just now'
  if (delta < hour) {
    const n = Math.floor(delta / minute)
    return zh ? `${n} 分钟前` : `${n}m ago`
  }
  if (delta < day) {
    const n = Math.floor(delta / hour)
    return zh ? `${n} 小时前` : `${n}h ago`
  }

  const startOfDay = (ms: number) => {
    const d = new Date(ms)
    d.setHours(0, 0, 0, 0)
    return d.getTime()
  }
  const dayDiff = Math.round((startOfDay(nowMs) - startOfDay(then)) / day)
  if (dayDiff === 1) return zh ? '昨天' : 'yesterday'
  if (dayDiff > 1 && dayDiff < 7) {
    return zh ? `${dayDiff} 天前` : `${dayDiff}d ago`
  }

  return new Date(then).toLocaleDateString(zh ? 'zh-CN' : 'en-US', {
    month: 'short',
    day: 'numeric'
  })
}
