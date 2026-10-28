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
    patchPrefs: vi.fn(),
    entries: vi.fn(),
    entry: vi.fn(),
    makeAudio: vi.fn(),
    findAudio: vi.fn(),
    saveSearch: vi.fn(),
    renameSearch: vi.fn(),
    deleteSearch: vi.fn(),
    logout: vi.fn(),
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
  vi.mocked(api.patchPrefs).mockResolvedValue(undefined)
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
    vi.mocked(api.setState).mockRejectedValue(new ApiError(422, 'Server unavailable.'))
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
    // Evening from an hour before sunset (about 19:48 UTC), Night desk 45 minutes after it.
    vi.setSystemTime(new Date('2026-06-21T19:00:00Z'))
    app.applyPrefs()
    expect(document.documentElement.dataset.theme).toBe('evening')
    vi.setSystemTime(new Date('2026-06-21T21:00:00Z'))
    app.applyPrefs()
    expect(document.documentElement.dataset.theme).toBe('night')
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

  it('keeps rows read here where they were: the unread page leaves them out', () => {
    // Unread, with 3 and 2 read here: the server's page now lacks them.
    const at = (id: string, over: Partial<Entry> = {}) => entry(id, { publishedAt: 1_790_000_000 + Number(id), ...over })
    app.entries = [at('3', { read: true }), at('2', { read: true }), at('1')]
    app.boundary = '3'
    app.mergeArrivals([at('5'), at('1')], [at('5')])
    expect(app.entries.map((e) => e.id)).toEqual(['5', '3', '2', '1'])
  })

  it('an article arriving while the list loads is still looked for', async () => {
    let reply!: (v: { entries: Entry[]; cursor: string }) => void
    vi.mocked(api.entries).mockReturnValueOnce(new Promise((ok) => (reply = ok)))
    app.counts = { ...app.counts, newest: '3' }
    const check = vi.spyOn(app, 'checkArrivals').mockResolvedValue(undefined)
    const loading = app.load()
    app.counts = { ...app.counts, newest: '9' } // arrives mid-request
    reply({ entries: [entry('3'), entry('2')], cursor: '' })
    await loading
    expect(app.boundary).toBe('3') // not past the article it hasn't got
    expect(check).toHaveBeenCalled()
    check.mockRestore()
  })

  it('places an arrival between rows, by the list\'s order', () => {
    const at = (id: string) => entry(id, { publishedAt: 1_790_000_000 + Number(id) })
    app.entries = [at('9'), at('6')]
    app.boundary = '9'
    app.mergeArrivals([at('9'), at('7'), at('6')], [at('7')])
    expect(app.entries.map((e) => e.id)).toEqual(['9', '7', '6'])
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

describe('articles arriving', () => {
  const tree = (newest: string) => ({ folders: [], feeds: app.feeds, counts: { unread: 48, starred: 0, today: 48, newest } })

  beforeEach(() => {
    app.page = 'reader'
    app.loading = false
    app.query = ''
    app.atTop = true
    app.listBusyUntil = 0
    app.pending = 0
    app.entries = []
    app.boundary = '3'
    vi.mocked(api.entries).mockReset()
  })

  it('appear in an empty list when the counts are refreshed, however that happens', async () => {
    // Coming back to the app refreshes the counts; no live event arrives.
    vi.mocked(api.tree).mockResolvedValue(tree('9'))
    vi.mocked(api.entries).mockResolvedValue({ entries: [entry('9'), entry('8')], cursor: '' })
    await app.refreshTree()
    await vi.waitFor(() => expect(app.entries.map((e) => e.id)).toEqual(['9', '8']))
    expect(app.boundary).toBe('9')
    expect([...app.arriving]).toEqual(['9', '8']) // animated in
  })

  it('are offered, never inserted, when reading further down', async () => {
    app.entries = [entry('3'), entry('2')]
    app.atTop = false
    vi.mocked(api.tree).mockResolvedValue(tree('9'))
    vi.mocked(api.entries).mockResolvedValue({ entries: [entry('9'), entry('3'), entry('2')], cursor: '' })
    await app.refreshTree()
    await vi.waitFor(() => expect(app.pending).toBe(1))
    expect(app.entries.map((e) => e.id)).toEqual(['3', '2'])
  })

  it('for another list are looked for once', async () => {
    vi.mocked(api.tree).mockResolvedValue(tree('9'))
    vi.mocked(api.entries).mockResolvedValue({ entries: [], cursor: '' })
    await app.refreshTree()
    await vi.waitFor(() => expect(app.boundary).toBe('9'))
    await app.refreshTree()
    expect(api.entries).toHaveBeenCalledTimes(1)
  })
})

describe('closing the article', () => {
  it('goes back to the list', () => {
    app.selectedId = '1'
    app.article = { entry: entry('1'), enclosures: [] }
    app.pane = 'article'
    app.closeArticle()
    expect(app.article).toBeNull()
    expect(app.selectedId).toBeNull()
    expect(app.pane).toBe('list')
  })
})

describe('list rows', () => {
  it('pick up a picture found since the list loaded, when the article opens', async () => {
    app.entries = [entry('1'), entry('2')]
    vi.mocked(api.entry).mockResolvedValue({ entry: { ...entry('1'), imageUrl: '/img/abc', content: '<p>x</p>' }, enclosures: [] })
    app.select('1')
    await vi.waitFor(() => expect(app.entries[0].imageUrl).toBe('/img/abc'))
  })

  it('and when its full article loads', async () => {
    app.entries = [entry('5')]
    app.selectedId = '5'
    app.article = { entry: entry('5'), enclosures: [] }
    vi.mocked(api.fullArticle).mockResolvedValue({ entry: { ...entry('5'), imageUrl: '/img/full', content: '<p>all</p>', fullText: true }, enclosures: [] })
    await app.loadFullArticle()
    expect(app.entries[0].imageUrl).toBe('/img/full')
  })
})

describe('saved searches', () => {
  const duo = { id: 7, name: 'duo', query: 'duo', unread: 2 }

  beforeEach(() => {
    vi.mocked(api.entries).mockReset()
    vi.mocked(api.entries).mockResolvedValue({ entries: [], cursor: '' })
    vi.mocked(api.tree).mockResolvedValue({ folders: [], feeds: app.feeds, counts: app.counts, searches: [duo] })
    app.searches = []
    app.page = 'reader'
  })

  it('keeps the current search, and opens it', async () => {
    vi.mocked(api.saveSearch).mockResolvedValue({ search: duo })
    app.query = 'duo'
    await app.saveSearch()
    expect(api.saveSearch).toHaveBeenCalledWith('duo')
    expect(app.source).toEqual({ kind: 'search', id: 7 })
    expect(app.query).toBe('')
    expect(app.title).toBe('duo')
    // Its list asks for its matches, unread only by default.
    await vi.waitFor(() => expect(api.entries).toHaveBeenCalledWith(expect.objectContaining({ q: 'duo', view: 'unread' })))
  })

  it('marks only its matches read', async () => {
    app.searches = [duo]
    app.source = { kind: 'search', id: 7 }
    app.boundary = '9'
    vi.mocked(api.markAllRead).mockResolvedValue({ ids: [] })
    await app.markAllRead()
    expect(api.markAllRead).toHaveBeenCalledWith({ q: 'duo' }, '9')
  })

  it('and so does a search not saved yet', async () => {
    app.searches = []
    app.source = { kind: 'view', view: 'all' }
    app.query = 'harbour'
    app.boundary = '9'
    vi.mocked(api.markAllRead).mockResolvedValue({ ids: [] })
    await app.markAllRead()
    expect(api.markAllRead).toHaveBeenCalledWith({ view: undefined, q: 'harbour' }, '9')
    app.query = ''
  })

  it('can be removed, with undo', async () => {
    app.searches = [duo]
    app.source = { kind: 'search', id: 7 }
    vi.mocked(api.deleteSearch).mockResolvedValue(undefined)
    await app.deleteSearch(7)
    expect(app.searches).toEqual([])
    expect(app.source).toEqual({ kind: 'view', view: 'unread' })
    vi.mocked(api.saveSearch).mockResolvedValue({ search: duo })
    await app.toasts.at(-1)!.action!.run()
    expect(api.saveSearch).toHaveBeenCalledWith('duo', 'duo')
    expect(app.source).toEqual({ kind: 'search', id: 7 })
  })
})

describe('leaving Settings', () => {
  it('goes back to the articles', () => {
    app.page = 'settings'
    app.pane = 'list'
    app.source = { kind: 'view', view: 'unread' }
    app.leavePage()
    expect(app.page).toBe('reader')
    expect(location.hash).toBe('#/unread')
  })
})

describe('preferences', () => {
  it('keeps sizes in this browser and sends only the other changes, together', () => {
    vi.useFakeTimers()
    vi.mocked(api.patchPrefs).mockClear()
    app.setPrefs({ readSize: 22, uiScale: 1.1 })
    vi.advanceTimersByTime(500)
    expect(api.patchPrefs).not.toHaveBeenCalled()
    expect(JSON.parse(localStorage.getItem('correspondent:sizes') ?? '{}')).toMatchObject({ readSize: 22, uiScale: 1.1 })

    app.setPrefs({ theme: 'evening' })
    app.setPrefs({ readFont: 'sans', measure: 60 })
    vi.advanceTimersByTime(500)
    expect(api.patchPrefs).toHaveBeenCalledTimes(1)
    const sent = vi.mocked(api.patchPrefs).mock.calls[0][0] as Record<string, unknown>
    expect(sent).toMatchObject({ theme: 'evening', readFont: 'sans' })
    expect(Object.keys(sent)).not.toContain('measure')
    expect(Object.keys(sent)).not.toContain('readSize')
    vi.useRealTimers()
  })
})

describe('a session that ends elsewhere', () => {
  it('signs out once on a 401, keeping only this browser\'s sizes', async () => {
    vi.mocked(api.logout).mockResolvedValue(undefined)
    localStorage.setItem('correspondent:prefs', JSON.stringify({ place: { lat: 40.4, lon: -3.7 } }))
    localStorage.setItem('correspondent:sizes', JSON.stringify({ readSize: 22 }))
    app.user = 'reader'
    ;(app as unknown as { watchSignOut(): void }).watchSignOut()
    window.dispatchEvent(new CustomEvent('correspondent:signed-out'))
    window.dispatchEvent(new CustomEvent('correspondent:signed-out'))
    await vi.waitFor(() => expect(localStorage.getItem('correspondent:prefs')).toBeNull())
    expect(api.logout).toHaveBeenCalledTimes(1)
    expect(localStorage.getItem('correspondent:sizes')).not.toBeNull()
  })
})

describe('live updates', () => {
  it('reconnects after the server closes the stream, and catches up once back', async () => {
    vi.useFakeTimers()
    const made: FakeSource[] = []
    class FakeSource {
      static CLOSED = 2
      readyState = 0
      listeners: Record<string, (() => void)[]> = {}
      constructor() {
        made.push(this)
      }
      addEventListener(name: string, fn: () => void) {
        ;(this.listeners[name] ??= []).push(fn)
      }
      close() {
        this.readyState = 2
      }
      fire(name: string) {
        for (const fn of this.listeners[name] ?? []) fn()
      }
    }
    vi.stubGlobal('EventSource', FakeSource)
    const resync = vi.spyOn(app, 'resyncStates').mockResolvedValue(undefined)
    app.user = 'reader'
    ;(app as unknown as { listen(): void }).listen()

    made[0].readyState = 2 // a 502 while the server restarts: closed for good
    made[0].fire('error')
    await vi.advanceTimersByTimeAsync(2000)
    expect(made).toHaveLength(2)
    made[1].fire('open')
    expect(resync).toHaveBeenCalled()

    vi.unstubAllGlobals()
    vi.useRealTimers()
    resync.mockRestore()
  })
})
