import { getPhoneSecret } from '@/api/http'
import { tGlobal } from '@/i18n'

export type AttachmentRef = {
  id: string
  url: string
  name: string
  mime: string
  size: number
  key?: string
  /** local preview only */
  previewUrl?: string
}

const MAX_BYTES = 4 * 1024 * 1024
const MAX_EDGE = 1920
const MAX_COUNT = 5

const ALLOWED = new Set([
  'image/jpeg', 'image/jpg', 'image/png', 'image/webp', 'image/gif',
  'text/plain', 'text/markdown', 'application/pdf', 'application/json'
])

const MIME_BY_EXTENSION = new Map([
  ['.md', 'text/markdown'],
  ['.markdown', 'text/markdown'],
  ['.pdf', 'application/pdf'],
  ['.json', 'application/json']
])

const MIME_ALIASES = new Map([
  ['text/md', 'text/markdown'],
  ['text/x-markdown', 'text/markdown'],
  ['application/markdown', 'text/markdown'],
  ['application/x-markdown', 'text/markdown'],
  ['application/x-pdf', 'application/pdf'],
  ['application/acrobat', 'application/pdf'],
  ['application/vnd.pdf', 'application/pdf'],
  ['text/pdf', 'application/pdf'],
  ['text/json', 'application/json'],
  ['text/x-json', 'application/json'],
  ['application/x-json', 'application/json'],
  ['application/ld+json', 'application/json']
])

export function isImageMime(mime: string): boolean {
  return mime.startsWith('image/')
}

function attachmentMime(file: File): string {
  const mime = file.type.toLowerCase().split(';', 1)[0].trim()
  const extension = file.name.toLowerCase().match(/\.[^.]+$/)?.[0] || ''
  const extensionMime = MIME_BY_EXTENSION.get(extension)
  if ((!mime || mime === 'application/octet-stream') && extensionMime) {
    return extensionMime
  }
  const alias = MIME_ALIASES.get(mime)
  return alias && alias === extensionMime ? alias : (mime || 'application/octet-stream')
}

function throwIfAborted(signal?: AbortSignal): void {
  if (!signal?.aborted) return
  if (typeof DOMException === 'function') {
    throw new DOMException('The operation was aborted', 'AbortError')
  }
  const error = new Error('The operation was aborted')
  error.name = 'AbortError'
  throw error
}

type DecodedImage = {
  source: CanvasImageSource
  width: number
  height: number
  close: () => void
}

async function decodeImage(file: File): Promise<DecodedImage> {
  if (typeof createImageBitmap === 'function') {
    try {
      const bitmap = await createImageBitmap(file)
      return {
        source: bitmap,
        width: bitmap.width,
        height: bitmap.height,
        close: () => bitmap.close()
      }
    } catch {
      // Some mobile formats are supported by <img> but not createImageBitmap.
    }
  }

  if (
    typeof Image === 'undefined' ||
    typeof URL.createObjectURL !== 'function'
  ) {
    throw new Error(tGlobal('errors.attachUnreadable', { name: file.name }))
  }

  const objectURL = URL.createObjectURL(file)
  const image = new Image()
  try {
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve()
      image.onerror = () => reject(new Error('image decode failed'))
      image.src = objectURL
    })
    return {
      source: image,
      width: image.naturalWidth,
      height: image.naturalHeight,
      close: () => URL.revokeObjectURL(objectURL)
    }
  } catch {
    URL.revokeObjectURL(objectURL)
    throw new Error(tGlobal('errors.attachDecode', { name: file.name }))
  }
}

/** Pass through upload-ready files; only decode images that need shrinking/conversion. */
export async function prepareFile(file: File): Promise<File> {
  const mime = file.type.toLowerCase()
  if (!mime.startsWith('image/') || mime === 'image/gif') {
    if (file.size > MAX_BYTES) {
      throw new Error(tGlobal('errors.attachTooLarge', { name: file.name }))
    }
    return file
  }

  // Avoid an unnecessary decode/re-encode step. Besides preserving image
  // quality, this keeps normal screenshots working in browsers whose
  // createImageBitmap implementation is missing or incomplete.
  if (ALLOWED.has(mime) && file.size <= MAX_BYTES) {
    return file
  }

  const decoded = await decodeImage(file)
  let { width, height } = decoded
  const scale = Math.min(1, MAX_EDGE / Math.max(width, height))
  width = Math.round(width * scale)
  height = Math.round(height * scale)
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const ctx = canvas.getContext('2d')
  if (!ctx) {
    decoded.close()
    throw new Error(tGlobal('errors.attachCompressFail', { name: file.name }))
  }
  ctx.drawImage(decoded.source, 0, 0, width, height)
  decoded.close()
  const blob: Blob | null = await new Promise(resolve =>
    canvas.toBlob(resolve, 'image/jpeg', 0.85)
  )
  if (!blob) {
    throw new Error(tGlobal('errors.attachCompressFail', { name: file.name }))
  }
  if (blob.size > MAX_BYTES) {
    throw new Error(tGlobal('errors.attachStillLarge', { name: file.name }))
  }
  const base = file.name.replace(/\.[^.]+$/, '') || 'image'
  return new File([blob], `${base}.jpg`, { type: 'image/jpeg' })
}

export async function uploadAttachment(
  file: File,
  opts: { deviceId?: string; sessionId?: string; signal?: AbortSignal } = {}
): Promise<AttachmentRef> {
  if (file.size > MAX_BYTES) {
    throw new Error(tGlobal('errors.attachTooLarge', { name: file.name }))
  }
  const mime = attachmentMime(file)
  if (!ALLOWED.has(mime) && mime !== 'application/octet-stream') {
    throw new Error(tGlobal('errors.attachType', { name: file.name }))
  }
  const uploadFile = mime === file.type
    ? file
    : new File([file], file.name, { type: mime, lastModified: file.lastModified })
  const fd = new FormData()
  fd.append('file', uploadFile)
  if (opts.deviceId) fd.append('device_id', opts.deviceId)
  if (opts.sessionId) fd.append('session_id', opts.sessionId)

  const headers: Record<string, string> = {}
  const secret = getPhoneSecret()
  if (secret) {
    headers.Authorization = `Bearer ${secret}`
    headers['X-Neko-Secret'] = secret
  }
  // Do NOT set Content-Type — browser sets multipart boundary

  const res = await fetch('/api/attachments', {
    method: 'POST',
    headers,
    body: fd,
    signal: opts.signal
  })
  if (!res.ok) {
    const t = await res.text()
    throw new Error(t || tGlobal('errors.attachUpload', { status: res.status }))
  }
  const data = await res.json()
  return {
    id: data.id,
    url: data.url,
    name: data.name || uploadFile.name,
    mime: data.mime || mime,
    size: data.size || uploadFile.size,
    key: data.key,
    previewUrl: isImageMime(data.mime || mime) ? URL.createObjectURL(file) : undefined
  }
}

export async function pickAndUpload(
  fileList: FileList | File[],
  opts: { deviceId?: string; sessionId?: string; signal?: AbortSignal } = {}
): Promise<AttachmentRef[]> {
  const files = Array.from(fileList).slice(0, MAX_COUNT)
  const out: AttachmentRef[] = []
  for (const f of files) {
    throwIfAborted(opts.signal)
    const prepared = await prepareFile(f)
    throwIfAborted(opts.signal)
    out.push(await uploadAttachment(prepared, opts))
  }
  return out
}

export { MAX_COUNT, MAX_BYTES }
