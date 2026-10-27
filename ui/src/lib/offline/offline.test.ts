import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Entry } from '../api'
import * as db from './db'
import { flush, plan } from './outbox'

const entry = (id: string, over: Partial<Entry> = {}): Entry => ({
  id, feedId: 1, url: '', title: 'T' + id, author: '', excerpt: '', imageUrl: '',
  publishedAt: 1_790_000_000 + Number(id), updatedAt: 0, read: false, starred: false, ...over,
})

beforeEach(async () => {
  await db.clear()
})

describe('offline store', () => {
  it('keeps downloaded text when a summary of the same article is saved later', async () => {
    await db.saveEntries([entry('1', { content: '<p>full</p>' })])
    await db.saveEntries([entry('1', { title: 'renamed' })]) // summary: no content
    const e = await db.getEntry('1')
    expect(e?.content).toBe('<p>full</p>')
    expect(e?.title).toBe('renamed')
  })

  it('answers list queries newest first, with the reader filters', async () => {
    await db.saveEntries([
      entry('1', { read: true }),
      entry('2', { starred: true }),
      entry('3', { feedId: 2, title: 'Harbour pilots' }),
    ])
    const ids = async (q: db.OfflineQuery) => (await db.queryEntries(q, () => new Set([2]))).map((e) => e.id)
    expect(await ids({})).toEqual(['3', '2', '1'])
    expect(await ids({ view: 'unread' })).toEqual(['3', '2'])
    expect(await ids({ view: 'starred' })).toEqual(['2'])
    expect(await ids({ folder: 9 })).toEqual(['3'])
    expect(await ids({ q: 'harbour' })).toEqual(['3'])
  })

  it('prunes old read articles but never starred or unread ones', async () => {
    await db.saveEntries([entry('1', { read: true }), entry('2', { read: true, starred: true }), entry('3')])
    await db.prune(0)
    expect((await db.queryEntries({}, () => new Set())).map((e) => e.id).sort()).toEqual(['2', '3'])
  })

  it('forgets everything on clear', async () => {
    await db.saveEntries([entry('1')])
    await db.enqueue({ kind: 'state', action: 'read', ids: ['1'], at: 1 })
    await db.clear()
    expect(await db.getEntry('1')).toBeUndefined()
    expect(await db.pending()).toEqual([])
  })
})

describe('outbox plan', () => {
  it('keeps the last action per article and axis within a run', () => {
    const steps = plan([
      { kind: 'state', action: 'read', ids: ['1', '2'], at: 1 },
      { kind: 'state', action: 'unread', ids: ['2'], at: 2 },
      { kind: 'state', action: 'star', ids: ['1'], at: 3 },
      { kind: 'state', action: 'unstar', ids: ['1'], at: 4 },
    ])
    expect(steps).toEqual([
      { kind: 'state', action: 'read', ids: ['1'], at: 1 },
      { kind: 'state', action: 'unread', ids: ['2'], at: 1 },
      { kind: 'state', action: 'unstar', ids: ['1'], at: 1 },
    ])
  })

  it('keeps mark-all in order relative to other changes', () => {
    const steps = plan([
      { kind: 'state', action: 'unread', ids: ['1'], at: 1 },
      { kind: 'markAll', scope: { feed: 3 }, before: '9', at: 2 },
      { kind: 'state', action: 'unread', ids: ['1'], at: 3 },
    ])
    expect(steps.map((s) => s.kind)).toEqual(['state', 'markAll', 'state'])
  })
})

describe('outbox flush', () => {
  const ok = () => new Response('{}', { status: 200 })

  it('sends queued changes in order with the CSRF header, then clears them', async () => {
    await db.enqueue({ kind: 'state', action: 'star', ids: ['1'], at: 1 })
    await db.enqueue({ kind: 'markAll', scope: { folder: 4 }, before: '99', at: 2 })
    const fetchFn = vi.fn(async () => ok())
    expect(await flush(fetchFn as unknown as typeof fetch)).toEqual({ sent: 2, left: 0 })
    const [first, second] = fetchFn.mock.calls as unknown as [string, RequestInit][]
    expect(first[0]).toBe('/api/v1/entries/state')
    expect(JSON.parse(first[1].body as string)).toEqual({ action: 'star', ids: ['1'] })
    expect((first[1].headers as Record<string, string>)['X-Correspondent']).toBe('1')
    expect(JSON.parse(second[1].body as string)).toEqual({ folder: '4', before: '99' })
    expect(await db.pending()).toEqual([])
  })

  it('replays a mark-all made in a search with its search, so only the matches are read', async () => {
    await db.enqueue({ kind: 'markAll', scope: { q: 'duo' }, before: '99', at: 1 })
    const fetchFn = vi.fn(async () => ok())
    await flush(fetchFn as unknown as typeof fetch)
    const [[, init]] = fetchFn.mock.calls as unknown as [string, RequestInit][]
    expect(JSON.parse(init.body as string)).toEqual({ q: 'duo', before: '99' })
  })

  it('keeps everything queued when the network is down', async () => {
    await db.enqueue({ kind: 'state', action: 'read', ids: ['1'], at: 1 })
    const r = await flush((async () => { throw new TypeError('Failed to fetch') }) as unknown as typeof fetch)
    expect(r.stopped).toBe('offline')
    expect(await db.pending()).toHaveLength(1)
  })

  it('stops on sign-out and server errors, retrying later', async () => {
    await db.enqueue({ kind: 'state', action: 'read', ids: ['1'], at: 1 })
    expect((await flush((async () => new Response('', { status: 401 })) as unknown as typeof fetch)).stopped).toBe('signed-out')
    expect((await flush((async () => new Response('', { status: 503 })) as unknown as typeof fetch)).stopped).toBe('server')
    expect(await db.pending()).toHaveLength(1)
  })

  it('drops a change the server rejects outright, so it cannot block the queue', async () => {
    await db.enqueue({ kind: 'state', action: 'read', ids: ['gone'], at: 1 })
    await flush((async () => new Response('{}', { status: 400 })) as unknown as typeof fetch)
    expect(await db.pending()).toEqual([])
  })

  it('splits large batches to the server limit', async () => {
    const ids = Array.from({ length: 2500 }, (_, i) => String(i + 1))
    await db.enqueue({ kind: 'state', action: 'read', ids, at: 1 })
    const fetchFn = vi.fn(async () => ok())
    await flush(fetchFn as unknown as typeof fetch)
    expect(fetchFn).toHaveBeenCalledTimes(3)
  })

  it('does not remove changes queued while a flush is running', async () => {
    await db.enqueue({ kind: 'state', action: 'read', ids: ['1'], at: 1 })
    const fetchFn = vi.fn(async () => {
      await db.enqueue({ kind: 'state', action: 'star', ids: ['2'], at: 2 })
      return ok()
    })
    await flush(fetchFn as unknown as typeof fetch)
    const left = await db.pending()
    expect(left).toHaveLength(1)
    expect(left[0].op).toMatchObject({ action: 'star', ids: ['2'] })
  })
})
