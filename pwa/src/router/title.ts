import { tGlobal } from '@/i18n'

export function routePageTitle(name: string | symbol | null | undefined): string {
  switch (name) {
    case 'devices':
      return tGlobal('title.devices')
    case 'device-detail':
      return tGlobal('title.deviceDetail')
    case 'session-detail':
      return tGlobal('title.session')
    case 'pair':
      return tGlobal('title.pair')
    case 'setup':
      return tGlobal('title.setup')
    default:
      return ''
  }
}

export function setDocumentTitle(pageTitle: string, detail?: string) {
  const brand = tGlobal('title.brand')
  const sep = tGlobal('title.sep')
  if (detail && pageTitle) {
    document.title = `${detail}${sep}${pageTitle}${sep}${brand}`
    return
  }
  if (pageTitle) {
    document.title = `${pageTitle}${sep}${brand}`
    return
  }
  document.title = brand
}
