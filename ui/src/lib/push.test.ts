import { afterEach, describe, expect, it, vi } from 'vitest'
import { BRAVE_PUSH, isBrave, keyBytes, safeTarget } from './push'

describe('push helpers', () => {
  it('decodes a base64url VAPID key to bytes', () => {
    // 65-byte uncompressed P-256 key: 0x04 followed by 64 bytes.
    const bytes = new Uint8Array(65)
    bytes[0] = 4
    bytes[64] = 255
    const b64url = btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
    const out = keyBytes(b64url)
    expect(out.length).toBe(65)
    expect(out[0]).toBe(4)
    expect(out[64]).toBe(255)
  })

  it('opens only in-app paths from a notification', () => {
    expect(safeTarget('/#/feed/7/e/99')).toBe('/#/feed/7/e/99')
    expect(safeTarget('https://evil.example/')).toBe('/')
    expect(safeTarget('//evil.example/x')).toBe('/')
    expect(safeTarget('javascript:alert(1)')).toBe('/')
    expect(safeTarget(undefined)).toBe('/')
  })
})

describe('Brave', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('is recognised by navigator.brave, and nothing else is', async () => {
    vi.stubGlobal('navigator', { brave: { isBrave: async () => true } })
    expect(await isBrave()).toBe(true)
    vi.stubGlobal('navigator', { userAgent: 'Chrome' })
    expect(await isBrave()).toBe(false)
  })

  it('gets told which setting turns push on', () => {
    expect(BRAVE_PUSH).toMatch(/Use Google services for push messaging/)
  })
})
