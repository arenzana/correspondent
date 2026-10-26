import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Entry } from './api'

vi.mock('./api', () => ({
  api: {
    setState: vi.fn(),
    markAllRead: vi.fn(),
    tree: vi.fn(),
    savePrefs: vi.fn(),
    entries: vi.fn(),
    entry: vi.fn(),
  },
}))

const { api } = await import('./api')
const { app } = await import('./state.svelte')

const entry = (id: string, over: Partial<Entry> = {}): Entry => ({
  id, feedId: 1, url: 'https://x.example/' + id, title: 'T' + id, author: '', excerpt: '',
  imageUrl: '', publishedAt: 1_790_000_000, updatedAt: 1_790_000_000, read: false, starred: false, ...over,
})

beforeEach(() => {
  vi.mocked(api.tree).mockResolvedValue({ folders: [], feeds: [], counts: { unread: 0, starred: 0, today: 0, newest: '0' } })
  vi.mocked(api.savePrefs).mockResolvedValue(undefined)
  app.entries = [entry('1'), entry('2'), entry('3', { read: true })]
  app.feeds = [{ id: 1, folderId: null, url: '', siteUrl: '', title: 'F', description: '', iconUrl: '', lastFetchAt: 0,
    lastStatus: 0, errorCount: 0, lastError: '', unread: 2, total: 3, cadence: [] }]
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
    vi.mocked(api.setState).mockRejectedValue(new Error('Server unavailable.'))
    await app.setRead(['1'], true)
    expect(app.entries[0].read).toBe(false)
    expect(app.counts.unread).toBe(2)
    expect(app.toasts.at(-1)?.message).toMatch(/Couldn't update the article\. Server unavailable\./)
  })

  it('rolls back a failed star', async () => {
    vi.mocked(api.setState).mockRejectedValue(new Error('nope'))
    await app.setStarred('2', true)
    expect(app.entries[1].starred).toBe(false)
    expect(app.counts.starred).toBe(0)
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
