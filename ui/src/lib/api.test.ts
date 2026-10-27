import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'

describe('requests', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('mark all read carries the search, so only its matches are read', async () => {
    const fetch = vi.fn(async () => new Response('{"ids":[]}', { headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetch)
    await api.markAllRead({ q: 'duo' }, '9')
    await api.markAllRead({ feed: 3 }, '9')
    const bodies = fetch.mock.calls.map((c) => JSON.parse((c as unknown as [string, RequestInit])[1].body as string))
    expect(bodies[0]).toEqual({ q: 'duo', before: '9' })
    expect(bodies[1]).toEqual({ feed: '3', before: '9' })
  })
})
