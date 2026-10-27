import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { audioPath, clearAudio, keepOnly, save, saved, savedURL } from './audio'

// A Cache API stand-in: enough of caches.open() for the audio cache.
function fakeCaches() {
  const stores = new Map<string, Map<string, Response>>()
  const key = (r: RequestInfo | URL) => new URL(typeof r === 'string' ? r : r instanceof URL ? r.href : r.url, 'https://rss.example').href
  return {
    async open(name: string) {
      if (!stores.has(name)) stores.set(name, new Map())
      const m = stores.get(name)!
      return {
        async match(r: RequestInfo) { return m.get(key(r))?.clone() },
        async put(r: RequestInfo, res: Response) { m.set(key(r), res) },
        async delete(r: Request) { return m.delete(key(r)) },
        async keys() { return [...m.keys()].map((u) => new Request(u)) },
      }
    },
    async delete(name: string) { return stores.delete(name) },
  }
}

describe('offline audio', () => {
  beforeEach(() => vi.stubGlobal('caches', fakeCaches()))
  afterEach(() => vi.unstubAllGlobals())

  it('saves audio once, plays the saved copy, and keeps only what is asked', async () => {
    const fetch = vi.fn(async () => new Response('ID3', { headers: { 'Content-Type': 'audio/mpeg' } }))
    vi.stubGlobal('fetch', fetch)
    expect(await savedURL('7')).toBeNull()
    await save('7')
    await save('7')
    await save('8')
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(fetch).toHaveBeenCalledWith(audioPath('7'), { credentials: 'same-origin' })
    expect(await saved()).toEqual(new Set(['7', '8']))
    expect(await savedURL('7')).toMatch(/^blob:/)

    expect(await keepOnly(new Set(['8']))).toBe(1)
    expect(await saved()).toEqual(new Set(['8']))
    await clearAudio()
    expect(await saved()).toEqual(new Set())
  })

  it('never saves an error page as audio', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"error":"no"}', { status: 200, headers: { 'Content-Type': 'application/json' } })))
    await save('9')
    expect(await saved()).toEqual(new Set())
  })
})
