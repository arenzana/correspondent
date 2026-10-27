import 'fake-indexeddb/auto'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Entry } from './api'

vi.mock('./api', async (importOriginal) => ({
  ApiError: (await importOriginal<typeof import('./api')>()).ApiError,
  api: {
    fullArticle: vi.fn(),
    feedText: vi.fn(),
    updateFeed: vi.fn(),
    deleteFeed: vi.fn(),
    refreshFeed: vi.fn(),
    renameFolder: vi.fn(),
    deleteFolder: vi.fn(),
    createFolder: vi.fn(),
    setState: vi.fn(),
    markAllRead: vi.fn(),
    tree: vi.fn(),
    savePrefs: vi.fn(),
    entries: vi.fn(),
    entry: vi.fn(),
    makeAudio: vi.fn(),
    findAudio: vi.fn(),
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
    lastStatus: 0, errorCount: 0, lastError: '', paused: false, notify: false, fullText: false, page: false, unread: 2, total: 3, starred: 0, latestAt: 0, cadence: [] }]
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

describe('full articles', () => {
  it('replaces the open article with the full one, and can switch back', async () => {
    app.entries = [entry('5', { url: 'https://x.example/5' })]
    app.selectedId = '5'
    app.article = { entry: { ...entry('5', { url: 'https://x.example/5' }), content: '<p>summary</p>' }, enclosures: [] }
    vi.mocked(api.fullArticle).mockResolvedValue({ entry: { ...entry('5'), content: '<p>whole story</p>', fullText: true }, enclosures: [] })
    await app.loadFullArticle()
    expect(app.article?.entry.content).toBe('<p>whole story</p>')
    expect(app.entries[0].fullText).toBe(true)
    vi.mocked(api.feedText).mockResolvedValue({ entry: { ...entry('5'), content: '<p>summary</p>', fullText: false }, enclosures: [] })
    await app.showFeedText()
    expect(app.article?.entry.content).toBe('<p>summary</p>')
    expect(app.entries[0].fullText).toBe(false)
  })

  it('explains a failure and remembers it on the row', async () => {
    app.entries = [entry('6')]
    app.selectedId = '6'
    app.article = { entry: { ...entry('6'), content: '' }, enclosures: [] }
    vi.mocked(api.fullArticle).mockRejectedValue(new ApiError(422, "Couldn't get the full article: the page has no readable article."))
    await app.loadFullArticle()
    expect(app.toasts.at(-1)?.message).toMatch(/no readable article/)
    expect(app.entries[0].fullError).toMatch(/no readable article/)
    expect(app.fetchingFull).toBe(false)
  })

  it('turns full articles on for a feed, rolling back if refused', async () => {
    vi.mocked(api.updateFeed).mockRejectedValueOnce(new ApiError(404, 'Not found.'))
    await app.setFeedFullText(1, true)
    expect(app.feeds[0].fullText).toBe(false)
    vi.mocked(api.updateFeed).mockResolvedValueOnce(undefined)
    await app.setFeedFullText(1, true)
    expect(api.updateFeed).toHaveBeenLastCalledWith(1, { fullText: true })
    expect(app.feeds[0].fullText).toBe(true)
  })

  it('turns full articles on for the open article\'s feed and loads it in full', async () => {
    app.entries = [entry('7')]
    app.selectedId = '7'
    app.article = { entry: { ...entry('7', { url: 'https://x.example/7' }), content: '<p>teaser…</p>' }, enclosures: [] }
    vi.mocked(api.updateFeed).mockRejectedValueOnce(new ApiError(500, 'Down.'))
    await app.fullTextForFeed(app.article.entry.feedId)
    expect(api.fullArticle).not.toHaveBeenCalled()
    vi.mocked(api.updateFeed).mockResolvedValueOnce(undefined)
    vi.mocked(api.fullArticle).mockResolvedValue({ entry: { ...entry('7'), content: '<p>whole</p>', fullText: true }, enclosures: [] })
    await app.fullTextForFeed(app.article.entry.feedId)
    expect(api.updateFeed).toHaveBeenLastCalledWith(app.article.entry.feedId, { fullText: true })
    expect(app.article?.entry.content).toBe('<p>whole</p>')
  })
})

describe('subscription management', () => {
  it('renames optimistically and rolls back if refused', async () => {
    vi.mocked(api.updateFeed).mockResolvedValueOnce(undefined)
    await app.renameFeed(1, '  Wire  ')
    expect(app.feeds[0].title).toBe('Wire')
    expect(api.updateFeed).toHaveBeenCalledWith(1, { title: 'Wire' })
    vi.mocked(api.updateFeed).mockRejectedValueOnce(new ApiError(400, 'Too long.'))
    await app.renameFeed(1, 'Other')
    expect(app.feeds[0].title).toBe('Wire')
    expect(app.toasts.at(-1)?.message).toMatch(/Couldn't rename the feed\. Too long\./)
  })

  it('ignores empty or unchanged names', async () => {
    await app.renameFeed(1, '   ')
    await app.renameFeed(1, 'F')
    expect(api.updateFeed).not.toHaveBeenCalled()
  })

  it('moves feeds to a folder, a new folder, or out of folders', async () => {
    vi.mocked(api.updateFeed).mockResolvedValue(undefined)
    await app.moveFeeds([1, 2], 'Tech')
    expect(api.updateFeed).toHaveBeenCalledWith(1, { folderName: 'Tech' })
    expect(api.updateFeed).toHaveBeenCalledWith(2, { folderName: 'Tech' })
    await app.moveFeeds([1], null)
    expect(api.updateFeed).toHaveBeenCalledWith(1, { noFolder: true })
    expect(app.toasts.at(-1)?.message).toBe('Moved the feed out of folders.')
  })

  it('reports a refused address change without closing anything', async () => {
    vi.mocked(api.updateFeed).mockRejectedValueOnce(new ApiError(409, 'You already subscribe to that address.'))
    expect(await app.setFeedURL(1, 'https://dup.example/feed')).toBe('You already subscribe to that address.')
  })

  it('deletes feeds and leaves a view that no longer exists', async () => {
    vi.mocked(api.deleteFeed).mockResolvedValue(undefined)
    app.source = { kind: 'feed', id: 1 }
    await app.deleteFeeds([1])
    expect(api.deleteFeed).toHaveBeenCalledWith(1)
    expect(app.source).toEqual({ kind: 'view', view: 'unread' })
    expect(app.toasts.at(-1)?.message).toBe('Deleted F.')
  })

  it('opens and leaves the management page through the address bar', () => {
    app.manage()
    expect(app.page).toBe('manage')
    expect(location.hash).toBe('#/manage')
    vi.mocked(api.entries).mockResolvedValue({ entries: [], cursor: '' })
    app.go({ kind: 'view', view: 'starred' })
    expect(app.page).toBe('reader')
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

  it('applies interface text size and typeface, ignoring unknown sizes', () => {
    const root = document.documentElement
    app.setPrefs({ uiScale: 1.2, uiFont: 'system' })
    expect(root.style.getPropertyValue('--ui-scale')).toBe('1.2')
    expect(root.style.getPropertyValue('--font-ui')).toBe('var(--font-system)')
    app.setPrefs({ uiScale: 7, uiFont: 'grotesk' })
    expect(root.style.getPropertyValue('--ui-scale')).toBe('1')
    expect(root.style.getPropertyValue('--font-ui')).toBe('var(--font-grotesk)')
    app.setPrefs({ uiScale: 1 })
  })

  it('colours the status bar like the headers', () => {
    document.head.insertAdjacentHTML('beforeend', '<meta name="theme-color" content="#EEF1F5" media="(prefers-color-scheme: light)">')
    document.documentElement.style.setProperty('--sheet', '#f4ecdc')
    app.setPrefs({ theme: 'evening' })
    const meta = document.querySelector('meta[name="theme-color"]')!
    expect(meta.getAttribute('content')).toBe('#f4ecdc')
    expect(meta.hasAttribute('media')).toBe(false)
    document.documentElement.style.removeProperty('--sheet')
    meta.remove()
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

describe('live arrivals', () => {
  it('slides new articles into place when the list is at the top', () => {
    app.entries = [entry('3'), entry('2'), entry('1')]
    app.boundary = '3'
    app.mergeArrivals([entry('5'), entry('4'), entry('3', { read: true }), entry('2')], [entry('5'), entry('4')])
    expect(app.entries.map((e) => e.id)).toEqual(['5', '4', '3', '2', '1'])
    expect(app.entries[2].read).toBe(true) // the fresh copy wins
    expect([...app.arriving]).toEqual(['5', '4'])
    expect(app.boundary).toBe('5')
    expect(app.pending).toBe(0)
  })

  it('never moves a list the reader is in: it offers the new ones instead', async () => {
    app.entries = [entry('3'), entry('2')]
    app.boundary = '3'
    app.counts = { ...app.counts, newest: '3' }
    vi.mocked(api.tree).mockResolvedValue({ folders: [], feeds: app.feeds, counts: { unread: 3, starred: 0, today: 3, newest: '4' } })
    vi.mocked(api.entries).mockResolvedValue({ entries: [entry('4'), entry('3'), entry('2')], cursor: '' })
    for (const setup of [() => (app.atTop = false), () => { app.atTop = true; app.listBusyUntil = Date.now() + 10_000 }]) {
      app.counts = { ...app.counts, newest: '3' }
      app.pending = 0
      setup()
      await (app as unknown as { onNewEntries(): Promise<void> }).onNewEntries()
      expect(app.entries.map((e) => e.id)).toEqual(['3', '2'])
      expect(app.pending).toBe(1)
    }
    app.atTop = true
    app.listBusyUntil = 0
  })
})

describe('listening to a list', () => {
  const audioURL = (id: string) => ({ url: `/api/v1/entries/${id}/audio` })
  const noAudio = () => new ApiError(404, 'This article has no audio yet.')

  beforeEach(() => {
    app.stopListening()
    app.speech = { provider: 'openai', model: 'gpt-4o-mini-tts', voice: 'alloy', keyHint: '6789' }
    app.entries = [entry('1'), entry('2'), entry('3', { read: true }), entry('4')]
    vi.mocked(api.makeAudio).mockReset()
    vi.mocked(api.findAudio).mockReset()
    vi.mocked(api.setState).mockResolvedValue({ changed: 1 })
  })

  it('plays the unread articles that have audio, in order, and never makes any', async () => {
    vi.mocked(api.findAudio).mockImplementation(async (id) => {
      if (id === '1') throw noAudio()
      return audioURL(id)
    })
    await app.listenToList()
    expect(app.listening?.id).toBe('2')
    expect(app.upNext.map((e) => e.id)).toEqual(['4']) // 3 is read
    expect(app.toasts).toEqual([])

    await app.listenEnded()
    expect(api.setState).toHaveBeenCalledWith('read', ['2'])
    expect(app.listening?.id).toBe('4')
    expect(app.upNext).toEqual([])
    // Even with a key, nothing in a list is read aloud unasked.
    expect(api.makeAudio).not.toHaveBeenCalled()
  })

  it('says so when nothing in the list has audio', async () => {
    vi.mocked(api.findAudio).mockRejectedValue(noAudio())
    await app.listenToList()
    expect(app.listening).toBeNull()
    expect(app.toasts.at(-1)?.message).toMatch(/press l/)
    expect(api.makeAudio).not.toHaveBeenCalled()
  })

  it('starts from the open article', async () => {
    vi.mocked(api.findAudio).mockImplementation(async (id) => audioURL(id))
    app.selectedId = '2'
    await app.listenToList()
    expect(app.listening?.id).toBe('2')
    expect(app.upNext.map((e) => e.id)).toEqual(['4'])
    app.stopListening()
    expect(app.listening).toBeNull()
    expect(app.upNext).toEqual([])
  })

  it('makes audio only for the article the reader picks', async () => {
    vi.mocked(api.makeAudio).mockImplementation(async (id) => audioURL(id))
    app.article = { entry: entry('4'), enclosures: [] }
    await app.readAloud()
    expect(api.makeAudio).toHaveBeenCalledExactlyOnceWith('4')
    expect(app.listening?.id).toBe('4')
  })
})

describe('the sponsored-post question', () => {
  beforeEach(() => {
    app.prefs = { ...app.prefs, skipSponsored: false, sponsoredAsked: false }
    vi.mocked(api.setState).mockResolvedValue({ changed: 1 })
  })

  it('skips sponsored posts from now on, and this one is read', () => {
    app.article = { entry: entry('1', { sponsored: true }), enclosures: [] }
    app.answerSponsored(true)
    expect(app.prefs.skipSponsored).toBe(true)
    expect(api.setState).toHaveBeenCalledWith('read', ['1'])
    expect(app.toasts.at(-1)?.message).toMatch(/Settings → Reading/)
  })

  it('keeps showing them, and stops asking', () => {
    app.answerSponsored(false)
    expect(app.prefs.skipSponsored).toBe(false)
    expect(app.prefs.sponsoredAsked).toBe(true)
  })
})
