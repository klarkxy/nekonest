import { afterEach, describe, expect, it, vi } from 'vitest'
import { isImageMime, pickAndUpload, prepareFile, uploadAttachment } from './attachments'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('isImageMime', () => {
  it('detects images', () => {
    expect(isImageMime('image/png')).toBe(true)
    expect(isImageMime('text/plain')).toBe(false)
  })
})

describe('prepareFile', () => {
  it('passes through an upload-ready image without requiring createImageBitmap', async () => {
    vi.stubGlobal('createImageBitmap', undefined)
    const file = new File(['png'], 'screen.png', { type: 'image/png' })

    await expect(prepareFile(file)).resolves.toBe(file)
  })

  it('rejects an oversized non-image before uploading', async () => {
    const file = new File(
      [new Uint8Array(4 * 1024 * 1024 + 1)],
      'large.pdf',
      { type: 'application/pdf' }
    )

    await expect(prepareFile(file)).rejects.toThrow(/4MB/)
  })
})

describe('uploadAttachment', () => {
  it('posts multipart data with phone authentication and returns the ref', async () => {
    localStorage.setItem('nekonest_phone_secret', 'test-secret')
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      id: 'a1',
      url: '/api/attachments/a1?k=beef',
      name: 'note.txt',
      mime: 'text/plain',
      size: 4,
      key: 'beef'
    }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    }))
    vi.stubGlobal('fetch', fetchMock)
    const file = new File(['note'], 'note.txt', { type: 'text/plain' })

    const result = await uploadAttachment(file, {
      deviceId: 'device-1',
      sessionId: 'session-1'
    })

    expect(result.url).toBe('/api/attachments/a1?k=beef')
    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/attachments')
    expect(init.headers).toMatchObject({
      Authorization: 'Bearer test-secret',
      'X-Neko-Secret': 'test-secret'
    })
    expect(init.body).toBeInstanceOf(FormData)
    const body = init.body as FormData
    expect(body.get('device_id')).toBe('device-1')
    expect(body.get('session_id')).toBe('session-1')
    expect(body.get('file')).toBeInstanceOf(File)
  })

  it.each([
    ['paper.pdf', 'application/x-pdf', 'application/pdf'],
    ['notes.md', 'text/x-markdown', 'text/markdown'],
    ['data.json', 'text/json', 'application/json']
  ])('normalizes the common MIME alias for %s', async (name, alias, canonical) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      id: 'alias',
      url: '/api/attachments/alias',
      name,
      size: 2
    }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await uploadAttachment(new File(['ok'], name, { type: alias }))

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const uploaded = (init.body as FormData).get('file') as File
    expect(uploaded.type).toBe(canonical)
    expect(result.mime).toBe(canonical)
  })
})

describe('pickAndUpload', () => {
  it('throws AbortError without relying on AbortSignal.throwIfAborted', async () => {
    const legacySignal = { aborted: true } as AbortSignal
    const file = new File(['note'], 'note.txt', { type: 'text/plain' })

    await expect(pickAndUpload([file], { signal: legacySignal }))
      .rejects.toMatchObject({ name: 'AbortError' })
  })
})
