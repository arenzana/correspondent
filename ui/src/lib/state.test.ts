import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Entry } from './api'

vi.mock('./api', async (importOriginal) => ({
  ApiError: (await importOriginal<typeof import('./api')>()).ApiError,
  api: {
    setState: vi.fn(),
    markAllRead: vi.fn(),
    tree: vi.fn(),
    savePrefs: vi.fn(),
    entries: vi.fn(),
    entry: vi.fn(),
  },
}))

const { api, ApiError } = await import('./api')
const { app } = await import('./state.svelte')
const offline = await import('./offline/db')
const networkDown = () => new TypeError('Failed to fetch')

const entry = (id: string, over: Partial<Entry> = {}): Entry => ({
  id, feedId: 1, url: 'https://x.example/' + id, title: 'T' + id, author: '', excerpt: '',
  imageUrl: '', publishedAt: 1_790_000_000, updatedAt: 1_790_000_000, read: false, starred: false, ...over,
})

beforeEach(async () => {
  await offline.clear()
  app.outboxCount = 0
  app.online = true
  vi.mocked(api.tree).mockResolvedValue({ folders: [], feeds: [], counts: { unread: 0, starred: 0, today: 0, newest: '0' } })
  vi.mocked(api.savePrefs).mockResolvedValue(undefined)
  app.entries = [entry('1'), entry('2'), entry('3', { read: true })]
  app.feeds = [{ id: 1, folderId: null, url: '', siteUrl: '', title: 'F', description: '', iconUrl: '', lastFetchAt: 0,
    lastStatus: 0, errorCount: 0, lastError: '', paused: false, unread: 2, total: 3, cadence: [] }]
  app.folders = []
  app.counts = { unread: 2, starred: 0, today: 2, newest: '3' }
  app.toasts = []
  app.source = { kind: 'view', view: 'unread' }
  app.selectedId = null
  app.article = null
})

describe('optimistic mutations', () => {
  it('marks read immediately and adjusts counts', async () => {
    vi.mocked(api.setState).mockResolvedValue({ changed: 1 })
    const p = app.setRead(['1'], true)
    expect(app.entries[0].read).toBe(true) // before the server answers
    expect(app.counts.unread).toBe(1)
    expect(app.feeds[0].unread).toBe(1)
    await p
    expect(api.setState).toHaveBeenCalledWith('read', ['1'])
  })

  it('skips entries already in the target state', async () => {
    vi.mocked(api.setState).mockResolvedValue({ changed: 0 })
    await app.setRead(['3'], true)
    expect(api.setState).not.toHaveBeenCalled()
  })

  it('rolls back and explains when the server refuses', async () => {
    vi.mocked(api.setState).mockRejectedValue(new ApiError(503, 'Server unavailable.'))
    await app.setRead(['1'], true)
    expect(app.entries[0].read).toBe(false)
    expect(app.counts.unread).toBe(2)
    expect(app.toasts.at(-1)?.message).toMatch(/Couldn't update the article\. Server unavailable\./)
  })

  it('rolls back a star the server refuses', async () => {
    vi.mocked(api.setState).mockRejectedValue(new ApiError(400, 'nope'))
    await app.setStarred('2', true)
    expect(app.entries[1].starred).toBe(false)
    expect(app.counts.starred).toBe(0)
  })
})

describe('changes from other clients', () => {
  it('patches visible rows and the open article without touching counts directly', async () => {
    vi.useFakeTimers()
    app.article = { entry: { ...entry('2'), content: '' }, enclosures: [] }
    const before = app.counts.unread
    app.applyRemoteState('read', ['1', '2', 'not-loaded'])
    expect(app.entries.filter((e) => e.read).map((e) => e.id)).toEqual(['1', '2', '3'])
    expect(app.article?.entry.read).toBe(true)
    expect(app.counts.unread).toBe(before) // the server's counts arrive with the debounced refresh
    expect(api.tree).not.toHaveBeenCalled()
    vi.advanceTimersByTime(300)
    expect(api.tree).toHaveBeenCalledTimes(1)
    vi.useRealTimers()
  })

  it('coalesces bursts into one count refresh', () => {
    vi.useFakeTimers()
    for (let i = 0; i < 50; i++) app.applyRemoteState('star', ['1'])
    vi.advanceTimersByTime(300)
    expect(api.tree).toHaveBeenCalledTimes(1)
    expect(app.entries[0].starred).toBe(true)
    vi.useRealTimers()
  })

  it('ignores unknown actions', () => {
    app.applyRemoteState('explode', ['1'])
    expect(app.entries[0]).toEqual(entry('1'))
  })

  it('resyncs visible rows in place after missed events', async () => {
    vi.mocked(api.entries).mockResolvedValue({ entries: [entry('1', { read: true, starred: true }), entry('3')], cursor: '' })
    await app.resyncStates()
    expect(app.entries.map((e) => [e.id, e.read, e.starred])).toEqual([['1', true, true], ['2', false, false], ['3', false, false]])
  })
})

describe('offline', () => {
  it('queues a change instead of rolling it back when the network is down', async () => {
    vi.mocked(api.setState).mockRejectedValue(networkDown())
    await app.setRead(['1'], true)
    expect(app.entries[0].read).toBe(true)
    expect(app.counts.unread).toBe(1)
    expect(app.outboxCount).toBe(1)
    expect((await offline.pending())[0].op).toMatchObject({ kind: 'state', action: 'read', ids: ['1'] })
    expect(app.toasts).toEqual([])
  })

  it('sends queued changes when the connection returns, then confirms', async () => {
    vi.mocked(api.setState).mockRejectedValue(networkDown())
    await app.setStarred('2', true)
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{"changed":1}'))
    vi.mocked(api.entries).mockResolvedValue({ entries: [], cursor: '' })
    await app.flushNow()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/entries/state', expect.objectContaining({ method: 'POST' }))
    expect(app.outboxCount).toBe(0)
    expect(app.toasts.at(-1)?.message).toBe('Synced your offline change.')
  })

  it('lists from the offline copy when the server is unreachable', async () => {
    await offline.saveEntries([entry('7'), entry('8', { read: true })])
    vi.mocked(api.entries).mockRejectedValue(networkDown())
    await app.load() // the Unread view
    expect(app.online).toBe(false)
    expect(app.entries.map((e) => e.id)).toEqual(['7'])
    expect(app.loadError).toBe('')
  })

  it('opens saved articles offline, and explains when one was not saved', async () => {
    await offline.saveEntries([entry('7', { content: '<p>saved</p>' }), entry('8')])
    vi.mocked(api.entry).mockRejectedValue(networkDown())
    app.select('7', { push: false })
    await vi.waitFor(() => expect(app.article?.entry.content).toBe('<p>saved</p>'))
    app.select('8', { push: false })
    await vi.waitFor(() => expect(app.articleError).toMatch(/wasn't saved for offline reading/))
  })

  it('still reports real server errors as errors', async () => {
    vi.mocked(api.entries).mockRejectedValue(new ApiError(500, 'Something went wrong on the server.'))
    await app.load()
    expect(app.loadError).toBe('Something went wrong on the server.')
  })
})

describe('mark all read', () => {
  it('uses the loaded boundary and offers Undo that restores exactly those entries', async () => {
    app.boundary = '3'
    vi.mocked(api.markAllRead).mockResolvedValue({ ids: ['1', '2'] })
    vi.mocked(api.setState).mockResolvedValue({ changed: 2 })
    await app.markAllRead()
    expect(api.markAllRead).toHaveBeenCalledWith({ view: undefined }, '3')
    expect(app.entries.filter((e) => e.read).map((e) => e.id)).toEqual(['1', '2', '3'])
    const toast = app.toasts.at(-1)!
    expect(toast.message).toBe('Marked 2 articles as read.')
    expect(toast.action?.label).toBe('Undo')
    await toast.action!.run()
    expect(api.setState).toHaveBeenCalledWith('unread', ['1', '2'])
    expect(app.entries.filter((e) => !e.read).map((e) => e.id)).toEqual(['1', '2'])
  })

  it('scopes to the current feed', async () => {
    app.source = { kind: 'feed', id: 7 }
    vi.mocked(api.markAllRead).mockResolvedValue({ ids: [] })
    await app.markAllRead()
    expect(api.markAllRead).toHaveBeenCalledWith({ feed: 7 }, expect.any(String))
    expect(app.toasts.at(-1)?.message).toBe('Nothing to mark as read.')
  })

  it('is not available in Starred', async () => {
    app.source = { kind: 'view', view: 'starred' }
    await app.markAllRead()
    expect(api.markAllRead).not.toHaveBeenCalled()
  })
})

describe('navigation', () => {
  it('moves through unread entries only with n', () => {
    vi.mocked(api.entry).mockReturnValue(new Promise(() => {}))
    app.entries = [entry('1', { read: true }), entry('2'), entry('3', { read: true }), entry('4')]
    app.move(1, true)
    expect(app.selectedId).toBe('2')
    app.move(1, true)
    expect(app.selectedId).toBe('4')
  })

  it('reads routes from the URL hash', () => {
    vi.mocked(api.entry).mockReturnValue(new Promise(() => {}))
    history.replaceState(null, '', '#/feed/12/e/1793024993075794')
    app.readHash()
    expect(app.source).toEqual({ kind: 'feed', id: 12 })
    expect(app.selectedId).toBe('1793024993075794')
    expect(app.pane).toBe('article')

    history.replaceState(null, '', '#/starred')
    app.readHash()
    expect(app.source).toEqual({ kind: 'view', view: 'starred' })
  })

  it('ignores malformed hashes', () => {
    app.source = { kind: 'view', view: 'all' }
    history.replaceState(null, '', '#/feed/abc')
    app.readHash()
    expect(app.source).toEqual({ kind: 'view', view: 'all' })
  })
})

describe('opening articles', () => {
  it('treats a missing body as empty once loaded, never as still loading', async () => {
    const titleOnly = entry('9')
    app.entries = [titleOnly]
    vi.mocked(api.entry).mockResolvedValue({ entry: { ...titleOnly }, enclosures: [] }) // no content field
    app.select('9', { push: false })
    expect(app.article?.entry.content).toBeUndefined() // loading
    await vi.waitFor(() => expect(app.article?.entry.content).toBe(''))
  })
})

describe('themes', () => {
  it('maps Day to the default token set and passes others through', () => {
    app.setPrefs({ theme: 'day' })
    expect(document.documentElement.dataset.theme).toBe('light')
    app.setPrefs({ theme: 'night' })
    expect(document.documentElement.dataset.theme).toBe('night')
  })

  it('follows the sun for a stored place', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-06-21T12:00:00Z'))
    app.setPrefs({ theme: 'sun', place: { lat: 40.4, lon: -3.7, estimated: false } })
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(app.sunNext?.theme).toBe('evening')
    vi.setSystemTime(new Date('2026-06-21T21:00:00Z'))
    app.applyPrefs()
    expect(document.documentElement.dataset.theme).toBe('evening')
    vi.useRealTimers()
  })

  it('falls back to a time-zone estimate when location is refused', async () => {
    const getCurrentPosition = vi.fn((_ok: PositionCallback, fail?: PositionErrorCallback | null) =>
      fail?.({ code: 1, message: 'denied' } as GeolocationPositionError))
    Object.defineProperty(navigator, 'geolocation', { value: { getCurrentPosition }, configurable: true })
    app.prefs = { ...app.prefs, place: null }
    await app.useSunTheme()
    expect(app.prefs.theme).toBe('sun')
    expect(app.prefs.place?.estimated).toBe(true)
  })

  it('stores only a coarse location', async () => {
    const getCurrentPosition = vi.fn((ok: PositionCallback) =>
      ok({ coords: { latitude: 40.416775, longitude: -3.70379 } } as GeolocationPosition))
    Object.defineProperty(navigator, 'geolocation', { value: { getCurrentPosition }, configurable: true })
    app.prefs = { ...app.prefs, place: null }
    await app.useSunTheme()
    expect(app.prefs.place).toEqual({ lat: 40.4, lon: -3.7, estimated: false })
  })
})
