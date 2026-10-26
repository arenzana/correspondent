import { describe, expect, it } from 'vitest'
import { route, shellAssets, trim } from './sw'

const o = 'https://rss.example.org'
const r = (path: string, method = 'GET', mode: RequestMode = 'cors', origin = o) => route(new URL(path, origin), method, mode, o)

describe('service worker routing', () => {
  it('never caches the APIs, the worker itself, or the per-user manifest', () => {
    expect(r('/api/v1/entries')).toBe('network-only')
    expect(r('/api/v1/events')).toBe('network-only')
    expect(r('/api/greader.php/reader/api/0/token')).toBe('network-only')
    expect(r('/sw.js')).toBe('network-only')
    expect(r('/manifest.webmanifest')).toBe('network-only')
  })
  it('passes through writes and other origins', () => {
    expect(r('/assets/app.js', 'POST')).toBe('network-only')
    expect(r('https://images.example.com/a.jpg', 'GET', 'no-cors', 'https://images.example.com')).toBe('network-only')
  })
  it('serves page loads from the network, falling back to the shell', () => {
    expect(r('/#/unread', 'GET', 'navigate')).toBe('shell')
    expect(r('/feed/12', 'GET', 'navigate')).toBe('shell')
  })
  it('caches immutable assets first', () => {
    expect(r('/assets/index-abc.js')).toBe('cache-first')
    expect(r('/icons/wire/icon-192.png')).toBe('cache-first')
    expect(r('/favicon.svg')).toBe('network-then-cache')
  })
  it('keeps proxied images in their own cache', () => {
    expect(r('/img/sig/256/aHR0cHM6Ly9leGFtcGxlLmNvbQ')).toBe('image')
    expect(r('/img/sig/256/x', 'POST')).toBe('network-only')
  })
})

describe('image cache trimming', () => {
  it('drops the oldest entries beyond the limit', async () => {
    const keys = ['a', 'b', 'c', 'd', 'e'].map((k) => new Request(`${o}/img/${k}`))
    const deleted: string[] = []
    const cache = {
      keys: async () => keys,
      delete: async (k: Request) => (deleted.push(new URL(k.url).pathname), true),
    } as unknown as Cache
    expect(await trim(cache, 3)).toBe(2)
    expect(deleted).toEqual(['/img/a', '/img/b'])
    deleted.length = 0
    expect(await trim(cache, 10)).toBe(0)
    expect(deleted).toEqual([])
  })
})

describe('shell assets', () => {
  it('finds the hashed assets and icons the shell references', () => {
    const html = `<link rel="icon" href="/icons/wire/favicon.svg"><script type="module" src="/assets/index-abc.js"></script>
      <link rel="stylesheet" href="/assets/index-def.css"><link rel="manifest" href="/manifest.webmanifest">`
    expect(shellAssets(html).sort()).toEqual(['/assets/index-abc.js', '/assets/index-def.css', '/icons/wire/favicon.svg'])
  })
})
