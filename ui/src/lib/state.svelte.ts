// Application state. One reactive object, shared by every component.
// Mutations are optimistic: the UI changes immediately and rolls back if the
// server refuses. Without a network, the reader works from its offline copy
// (lib/offline) and queues changes until the connection returns.

import { api, ApiError, type Counts, type Entry, type Enclosure, type Feed, type Folder, type ID } from './api'
import { coarse, estimateFromTimezone, sunTheme } from './sun'
import * as offline from './offline/db'
import { flush as flushOutbox } from './offline/outbox'

/**
 * A failure to reach the server at all, as opposed to the server saying no:
 * fetch rejects with a TypeError ("Failed to fetch", "Load failed", …).
 */
const isNetworkError = (e: unknown) => e instanceof TypeError && !(e instanceof ApiError)

export type View = 'unread' | 'today' | 'starred' | 'all'
export type Source = { kind: 'view'; view: View } | { kind: 'feed'; id: number } | { kind: 'folder'; id: number }
export type Theme = 'auto' | 'sun' | 'day' | 'night' | 'evening'
export type Pane = 'nav' | 'list' | 'article'
export type AppIcon = 'wire' | 'byline' | 'morse'
export const APP_ICONS: { id: AppIcon; label: string }[] = [
  { id: 'wire', label: 'Wire' },
  { id: 'byline', label: 'Byline' },
  { id: 'morse', label: 'Morse' },
]

export interface Prefs {
  theme: Theme
  readSize: number
  measure: number
  density: 'comfortable' | 'compact'
  readFont: 'serif' | 'sans'
  unreadOnly: boolean
  markOnOpen: boolean
  sidebar: boolean
  /** Coarse position (about 11 km) for the Sun theme; never more precise. */
  place: { lat: number; lon: number; estimated: boolean } | null
  /** Pane widths in pixels; null means the responsive default. */
  navWidth: number | null
  listWidth: number | null
  appIcon: AppIcon
}

const defaultPrefs: Prefs = {
  theme: 'auto',
  readSize: 19,
  measure: 68,
  density: 'comfortable',
  readFont: 'serif',
  unreadOnly: true,
  markOnOpen: true,
  sidebar: true,
  place: null,
  navWidth: null,
  listWidth: null,
  appIcon: 'wire',
}

interface Toast {
  id: number
  message: string
  action?: { label: string; run: () => void }
}

function loadLocalPrefs(): Prefs {
  try {
    return { ...defaultPrefs, ...JSON.parse(localStorage.getItem('correspondent:prefs') ?? '{}') }
  } catch {
    return { ...defaultPrefs }
  }
}

class AppState {
  user = $state<string | null>(null)
  booting = $state(true)
  hasApiPassword = $state(false)
  version = $state('')
  commit = $state('')
  /** Whether the server is reachable; the reader works offline when not. */
  online = $state(typeof navigator === 'undefined' ? true : navigator.onLine)
  /** Changes made offline, waiting to be sent. */
  outboxCount = $state(0)
  /** When the offline copy was last refreshed (ms), or null. */
  syncedAt = $state<number | null>(null)
  prefs = $state<Prefs>(loadLocalPrefs())

  folders = $state<Folder[]>([])
  feeds = $state<Feed[]>([])
  counts = $state<Counts>({ unread: 0, starred: 0, today: 0, newest: '0' })

  source = $state<Source>({ kind: 'view', view: 'unread' })
  query = $state('')
  entries = $state<Entry[]>([])
  cursor = $state('')
  loading = $state(false)
  loadError = $state('')
  /** Newest entry ID seen when the list was loaded: the mark-all-read boundary. */
  boundary = $state<ID>('0')
  /** New articles arrived since the list was loaded; shown as a pill, never inserted under the reader. */
  pending = $state(0)

  selectedId = $state<ID | null>(null)
  article = $state<{ entry: Entry; enclosures: Enclosure[] } | null>(null)
  articleError = $state('')
  pane = $state<Pane>('list')
  focusMode = $state(false)

  paletteOpen = $state(false)
  shortcutsOpen = $state(false)
  addFeedOpen = $state(false)
  settingsOpen = $state(false)
  readingOpen = $state(false)
  navOpen = $state(false) // drawer on medium widths
  toasts = $state<Toast[]>([])

  feedById = $derived(new Map(this.feeds.map((f) => [f.id, f])))
  selectedIndex = $derived(this.entries.findIndex((e) => e.id === this.selectedId))

  title = $derived.by(() => {
    const s = this.source
    if (s.kind === 'feed') return this.feedById.get(s.id)?.title || 'Feed'
    if (s.kind === 'folder') return this.folders.find((f) => f.id === s.id)?.name ?? 'Folder'
    return { unread: 'Unread', today: 'Today', starred: 'Starred', all: 'All articles' }[s.view]
  })

  /** Whether the current list hides read articles. */
  unreadFilter = $derived(
    this.source.kind === 'view' ? this.source.view === 'unread' : this.prefs.unreadOnly,
  )

  private dwell: ReturnType<typeof setTimeout> | undefined
  private loadSeq = 0
  private toastSeq = 0

  // ---------------------------------------------------------------- session

  async boot() {
    this.applyPrefs()
    try {
      let me: Awaited<ReturnType<typeof api.me>>
      try {
        me = await api.me()
        offline.setMeta('me', me).catch(() => {})
      } catch (e) {
        // Opened without a network: carry on from the offline copy if there is one.
        const saved = isNetworkError(e) ? await offline.getMeta<typeof me>('me').catch(() => undefined) : undefined
        if (!saved) throw e
        me = saved
        this.online = false
      }
      this.user = me.username
      this.hasApiPassword = me.hasApiPassword
      this.version = me.version
      this.commit = me.commit
      this.prefs = { ...defaultPrefs, ...(me.prefs as Partial<Prefs>) }
      this.applyPrefs()
      this.readHash()
      await Promise.all([this.refreshTree(), this.load()])
      this.listen()
      this.watchConnection()
      this.syncedAt = (await offline.getMeta<number>('syncedAt').catch(() => undefined)) ?? null
      this.outboxCount = (await offline.pending().catch(() => [])).length
      void this.flushNow()
      // Refresh the offline copy as soon as the first screen is up.
      void this.syncOffline()
    } catch {
      this.user = null
    } finally {
      this.booting = false
    }
  }

  async signIn(username: string, password: string) {
    await api.login(username, password)
    this.booting = true
    await this.boot()
  }

  async signOut() {
    await api.logout().catch(() => {})
    // Leave nothing readable behind on a shared device.
    await offline.clear().catch(() => {})
    if ('caches' in window) for (const k of await caches.keys()) if (k.startsWith('correspondent-')) await caches.delete(k)
    location.hash = ''
    location.reload()
  }

  private events: EventSource | undefined
  private listen() {
    this.events?.close()
    this.events = new EventSource('/api/v1/events')
    this.events.addEventListener('refresh', () => this.onNewEntries())
    this.events.addEventListener('state', (e) => {
      try {
        const { action, ids } = JSON.parse((e as MessageEvent).data)
        this.applyRemoteState(action, ids)
      } catch {
        /* malformed event: ignore, the next resync corrects it */
      }
    })
    this.events.addEventListener('resync', () => this.resyncStates())
  }

  // ---------------------------------------------------------------- offline

  private watchConnection() {
    window.addEventListener('online', () => {
      this.online = true
      void this.flushNow().then(() => this.syncOffline())
      if (this.loadError || !this.entries.length) void this.load()
    })
    window.addEventListener('offline', () => (this.online = false))
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') void this.flushNow()
    })
    // While changes are waiting, keep trying (iOS has no Background Sync).
    setInterval(() => {
      if (this.outboxCount > 0) void this.flushNow()
    }, 30_000)
    setInterval(() => void this.syncOffline(), 15 * 60_000)
  }

  /** Queue a change made without a network. */
  private async queue(op: offline.OutboxOp) {
    await offline.enqueue(op)
    this.outboxCount = (await offline.pending()).length
    // Chrome/Android send it from the service worker even if the app closes.
    navigator.serviceWorker?.ready
      .then((r) => (r as ServiceWorkerRegistration & { sync?: { register(tag: string): Promise<void> } }).sync?.register('outbox'))
      .catch(() => {})
  }

  /** Send queued changes, then pick up the server's view of them. */
  async flushNow() {
    if (!this.outboxCount && !(await offline.pending().catch(() => [])).length) return
    const r = await flushOutbox()
    this.outboxCount = (await offline.pending()).length
    if (r.stopped === 'offline') this.online = false
    if (r.sent) {
      this.online = true
      this.refreshTreeSoon()
      void this.resyncStates()
      if (!this.outboxCount) this.toast(`Synced ${r.sent === 1 ? 'your offline change' : 'your offline changes'}.`)
    }
  }

  private syncing = false
  /**
   * Refresh the offline copy: the feed tree, plus the full text of unread
   * (up to 600) and starred (up to 300) articles, so they read offline.
   */
  async syncOffline() {
    if (this.syncing || !this.online || !this.user) return
    this.syncing = true
    try {
      for (const [view, pages] of [['unread', 6], ['starred', 3]] as const) {
        let cursor = ''
        for (let i = 0; i < pages; i++) {
          const r = await api.entries({ view, content: true, limit: 100, cursor: cursor || undefined })
          await offline.saveEntries(r.entries.map((e) => ({ ...e, content: e.content ?? '' })))
          if (!r.cursor) break
          cursor = r.cursor
        }
      }
      // A plain snapshot: IndexedDB cannot store Svelte's reactive proxies.
      await offline.saveTree($state.snapshot({ folders: this.folders, feeds: this.feeds, counts: this.counts }))
      await offline.prune()
      this.syncedAt = Date.now()
      await offline.setMeta('syncedAt', this.syncedAt)
    } catch (e) {
      if (isNetworkError(e)) this.online = false
    } finally {
      this.syncing = false
    }
  }

  private feedsInFolder = (folder: number) => new Set(this.feeds.filter((f) => f.folderId === folder).map((f) => f.id))

  private treeTimer: ReturnType<typeof setTimeout> | undefined
  private refreshTreeSoon() {
    clearTimeout(this.treeTimer)
    this.treeTimer = setTimeout(() => this.refreshTree().catch(() => {}), 250)
  }

  /**
   * A change made anywhere (Reeder, another tab, another device) arrives
   * here. Visible rows and the open article are patched in place; counts are
   * reloaded from the server, so nothing is counted twice when our own
   * changes echo back.
   */
  applyRemoteState(action: string, ids: ID[]) {
    const change: Partial<Entry> | null =
      action === 'read' ? { read: true }
      : action === 'unread' ? { read: false }
      : action === 'star' ? { starred: true }
      : action === 'unstar' ? { starred: false }
      : null
    if (!change) return
    if (action === 'read' && this.selectedId && ids.includes(this.selectedId)) clearTimeout(this.dwell)
    this.patch(ids, change)
    offline.patchEntries(ids, change).catch(() => {})
    this.refreshTreeSoon()
  }

  /** After missed events: re-read the state of the visible rows, in place. */
  async resyncStates() {
    this.refreshTreeSoon()
    if (!this.entries.length) return
    const r = await api.entries({ ...this.query_(), view: undefined, limit: Math.min(200, this.entries.length) }).catch(() => null)
    if (!r) return
    const fresh = new Map(r.entries.map((e) => [e.id, e]))
    this.entries = this.entries.map((e) => {
      const f = fresh.get(e.id)
      return f ? { ...e, read: f.read, starred: f.starred } : e
    })
  }

  private syncSoonTimer: ReturnType<typeof setTimeout> | undefined
  private async onNewEntries() {
    clearTimeout(this.syncSoonTimer)
    this.syncSoonTimer = setTimeout(() => void this.syncOffline(), 60_000)
    const before = BigInt(this.counts.newest || '0')
    await this.refreshTree()
    const starred = this.source.kind === 'view' && this.source.view === 'starred'
    if (starred || BigInt(this.counts.newest || '0') <= before) return
    const fresh = await api.entries({ ...this.query_(), limit: 200 }).catch(() => null)
    if (fresh) this.pending = fresh.entries.filter((e) => BigInt(e.id) > BigInt(this.boundary)).length
  }

  // ------------------------------------------------------------------ prefs

  setPrefs(patch: Partial<Prefs>) {
    this.prefs = { ...this.prefs, ...patch }
    this.applyPrefs()
    clearTimeout(this.prefsTimer)
    this.prefsTimer = setTimeout(() => api.savePrefs(this.prefs).catch(() => {}), 400)
  }
  private prefsTimer: ReturnType<typeof setTimeout> | undefined

  /** When the Sun theme will next change, for the settings popover. */
  sunNext = $state<{ theme: 'day' | 'evening' | 'night'; at: Date } | null>(null)
  private sunTimer: ReturnType<typeof setTimeout> | undefined

  /**
   * Switches to the Sun theme: Day from sunrise to sunset, Evening after.
   * Asks for location once; refusing falls back to a time-zone estimate.
   */
  async useSunTheme() {
    let place = this.prefs.place
    if (!place || place.estimated) {
      place = await new Promise((resolve) => {
        const fallback = () => resolve({ ...estimateFromTimezone(), estimated: true })
        if (!('geolocation' in navigator)) return fallback()
        navigator.geolocation.getCurrentPosition(
          (p) => resolve({ lat: coarse(p.coords.latitude), lon: coarse(p.coords.longitude), estimated: false }),
          fallback,
          { enableHighAccuracy: false, maximumAge: 86_400_000, timeout: 10_000 },
        )
      })
    }
    this.setPrefs({ theme: 'sun', place })
  }

  /** The theme actually shown right now. */
  private resolvedTheme(): string {
    clearTimeout(this.sunTimer)
    this.sunNext = null
    const { theme, place } = this.prefs
    if (theme !== 'sun') return theme === 'day' ? 'light' : theme
    const p = place ?? { ...estimateFromTimezone(), estimated: true }
    const now = new Date()
    const r = sunTheme(now, p.lat, p.lon)
    // What comes next: Day → Evening → Night desk → Day.
    this.sunNext = { theme: r.theme === 'day' ? 'evening' : r.theme === 'evening' ? 'night' : 'day', at: r.next }
    // Re-evaluate at the next sunrise or sunset (plus a second of margin).
    this.sunTimer = setTimeout(() => this.applyPrefs(), Math.min(r.next.getTime() - now.getTime() + 1000, 2 ** 31 - 1))
    return r.theme === 'day' ? 'light' : r.theme
  }

  applyPrefs() {
    const root = document.documentElement
    root.dataset.theme = this.resolvedTheme()
    const icon = APP_ICONS.some((i) => i.id === this.prefs.appIcon) ? this.prefs.appIcon : 'wire'
    const setHref = (id: string, file: string) => document.getElementById(id)?.setAttribute('href', `/icons/${icon}/${file}`)
    setHref('favicon', 'favicon.svg')
    setHref('favicon-png', 'favicon-32.png')
    setHref('touch-icon', 'apple-touch-icon.png')
    root.dataset.density = this.prefs.density
    root.style.setProperty('--read-size', this.prefs.readSize + 'px')
    root.style.setProperty('--read-measure', this.prefs.measure + 'ch')
    root.style.setProperty('--read-family', this.prefs.readFont === 'serif' ? 'var(--font-read)' : 'var(--font-ui)')
    try {
      localStorage.setItem('correspondent:prefs', JSON.stringify(this.prefs))
    } catch {
      /* private mode: prefs still sync to the server */
    }
  }

  // ------------------------------------------------------------------ data

  async refreshTree() {
    let t: offline.Tree
    try {
      t = await api.tree()
      this.online = true
      offline.saveTree(t).catch(() => {})
    } catch (e) {
      const saved = isNetworkError(e) ? await offline.loadTree().catch(() => undefined) : undefined
      if (!saved) throw e
      t = saved
      this.online = false
    }
    this.folders = t.folders
    this.feeds = t.feeds
    this.counts = t.counts
  }

  private query_() {
    const s = this.source
    const q: Parameters<typeof api.entries>[0] = { q: this.query || undefined }
    if (s.kind === 'view') q.view = s.view === 'all' ? undefined : s.view
    if (s.kind === 'feed') q.feed = s.id
    if (s.kind === 'folder') q.folder = s.id
    if (s.kind !== 'view' && this.prefs.unreadOnly) q.view = 'unread'
    return q
  }

  async load() {
    const seq = ++this.loadSeq
    this.loading = true
    this.loadError = ''
    this.pending = 0
    try {
      const r = await api.entries({ ...this.query_(), limit: 60 })
      if (seq !== this.loadSeq) return
      this.online = true
      this.entries = r.entries
      this.cursor = r.cursor
      this.boundary = r.entries.reduce((m, e) => (BigInt(e.id) > BigInt(m) ? e.id : m), this.counts.newest || '0')
      offline.saveEntries(r.entries).catch(() => {})
    } catch (e) {
      if (seq !== this.loadSeq) return
      if (isNetworkError(e)) {
        // Offline: show what the offline copy has for this list.
        this.online = false
        const q = this.query_()
        this.entries = await offline.queryEntries({ ...q, view: q.view ?? 'all' }, this.feedsInFolder).catch(() => [])
        this.cursor = ''
        this.boundary = this.entries.reduce((m, e) => (BigInt(e.id) > BigInt(m) ? e.id : m), '0')
      } else {
        this.loadError = (e as Error).message
      }
    } finally {
      if (seq === this.loadSeq) this.loading = false
    }
  }

  async loadMore() {
    if (!this.cursor || this.loading || !this.online) return
    const seq = this.loadSeq
    this.loading = true
    try {
      const r = await api.entries({ ...this.query_(), cursor: this.cursor, limit: 60 })
      if (seq !== this.loadSeq) return
      const seen = new Set(this.entries.map((e) => e.id))
      this.entries = [...this.entries, ...r.entries.filter((e) => !seen.has(e.id))]
      this.cursor = r.cursor
    } catch (e) {
      this.toast((e as Error).message)
    } finally {
      this.loading = false
    }
  }

  // ------------------------------------------------------------ navigation

  go(source: Source) {
    this.source = source
    this.selectedId = null
    this.article = null
    this.navOpen = false
    this.pane = 'list'
    this.writeHash()
    this.load()
  }

  sourceKey(s: Source = this.source): string {
    return s.kind === 'view' ? s.view : `${s.kind}/${s.id}`
  }

  writeHash() {
    const h = '#/' + this.sourceKey() + (this.selectedId ? '/e/' + this.selectedId : '')
    if (location.hash !== h) history.pushState(null, '', h)
  }

  readHash() {
    const m = location.hash.match(/^#\/(unread|today|starred|all|feed\/\d+|folder\/\d+)(?:\/e\/(\d+))?$/)
    if (!m) return
    const [kind, id] = m[1].split('/')
    this.source = id ? { kind: kind as 'feed' | 'folder', id: Number(id) } : { kind: 'view', view: kind as View }
    if (m[2]) {
      this.selectedId = m[2]
      this.pane = 'article'
      this.openArticle(m[2])
    } else {
      this.pane = 'list'
    }
  }

  async onPopState() {
    const key = this.sourceKey()
    const sel = this.selectedId
    this.readHash()
    if (this.sourceKey() !== key) await this.load()
    if (!location.hash.includes('/e/') && sel) {
      this.selectedId = null
      this.article = null
      this.pane = 'list'
    }
  }

  select(id: ID, opts: { push?: boolean } = {}) {
    if (this.selectedId === id && this.article) {
      this.pane = 'article'
      return
    }
    this.selectedId = id
    this.pane = 'article'
    if (opts.push !== false) this.writeHash()
    this.openArticle(id)
  }

  private async openArticle(id: ID) {
    clearTimeout(this.dwell)
    this.articleError = ''
    const summary = this.entries.find((e) => e.id === id)
    if (summary) this.article = { entry: { ...summary, content: undefined }, enclosures: [] }
    try {
      const r = await api.entry(id)
      if (this.selectedId !== id) return
      // The API omits an empty body; once loaded, "no content" must not look
      // like "still loading" (title-only feeds would spin forever).
      this.article = { ...r, entry: { ...r.entry, content: r.entry.content ?? '' } }
      offline.saveEntries([this.article.entry]).catch(() => {})
    } catch (e) {
      if (this.selectedId !== id) return
      const saved = isNetworkError(e) ? await offline.getEntry(id).catch(() => undefined) : undefined
      if (saved?.content !== undefined) {
        this.online = false
        this.article = { entry: saved, enclosures: [] }
      } else {
        this.articleError = isNetworkError(e)
          ? "This article wasn't saved for offline reading. It will open when you're back online."
          : (e as Error).message
        return
      }
    }
    // Mark read after a short, cancelable dwell rather than instantly, so
    // skimming past with j/k does not consume articles.
    const entry = this.entries.find((e) => e.id === id) ?? this.article?.entry
    if (this.prefs.markOnOpen && entry && !entry.read) {
      this.dwell = setTimeout(() => {
        if (this.selectedId === id) this.setRead([id], true)
      }, 900)
    }
  }

  move(delta: 1 | -1, unreadOnly = false) {
    const list = this.entries
    if (!list.length) return
    let i = this.selectedIndex
    for (let step = 0; step < list.length; step++) {
      i = i < 0 ? (delta > 0 ? 0 : list.length - 1) : i + delta
      if (i < 0 || i >= list.length) {
        if (delta > 0 && this.cursor) this.loadMore()
        return
      }
      if (!unreadOnly || !list[i].read) {
        this.select(list[i].id)
        if (delta > 0 && i > list.length - 8) this.loadMore()
        return
      }
    }
  }

  back() {
    if (this.pane === 'article') {
      history.back()
    } else if (this.pane === 'list') {
      this.pane = 'nav'
    }
  }

  // ------------------------------------------------------------- mutations

  private patch(ids: ID[], change: Partial<Entry>) {
    const set = new Set(ids)
    this.entries = this.entries.map((e) => (set.has(e.id) ? { ...e, ...change } : e))
    if (this.article && set.has(this.article.entry.id)) {
      this.article = { ...this.article, entry: { ...this.article.entry, ...change } }
    }
  }

  private adjustCounts(ids: ID[], read: boolean) {
    // Local count bookkeeping so the sidebar responds instantly.
    const d = read ? -1 : 1
    for (const id of ids) {
      const e = this.entries.find((x) => x.id === id)
      if (!e) continue
      const feed = this.feeds.find((f) => f.id === e.feedId)
      if (feed) feed.unread = Math.max(0, feed.unread + d)
      const folder = feed?.folderId ? this.folders.find((f) => f.id === feed.folderId) : undefined
      if (folder) folder.unread = Math.max(0, folder.unread + d)
      this.counts.unread = Math.max(0, this.counts.unread + d)
    }
  }

  async setRead(ids: ID[], read: boolean) {
    const targets = ids.filter((id) => this.entries.find((e) => e.id === id)?.read !== read)
    if (!targets.length) return
    this.adjustCounts(targets, read)
    this.patch(targets, { read })
    offline.patchEntries(targets, { read }).catch(() => {})
    try {
      await api.setState(read ? 'read' : 'unread', targets)
    } catch (e) {
      if (isNetworkError(e)) {
        await this.queue({ kind: 'state', action: read ? 'read' : 'unread', ids: targets, at: Date.now() })
        return
      }
      offline.patchEntries(targets, { read: !read }).catch(() => {})
      this.patch(targets, { read: !read })
      this.adjustCounts(targets, !read)
      this.toast(`Couldn't update the article. ${(e as Error).message}`)
    }
  }

  async setStarred(id: ID, starred: boolean) {
    this.patch([id], { starred })
    this.counts.starred += starred ? 1 : -1
    offline.patchEntries([id], { starred }).catch(() => {})
    try {
      await api.setState(starred ? 'star' : 'unstar', [id])
    } catch (e) {
      if (isNetworkError(e)) {
        await this.queue({ kind: 'state', action: starred ? 'star' : 'unstar', ids: [id], at: Date.now() })
        return
      }
      offline.patchEntries([id], { starred: !starred }).catch(() => {})
      this.patch([id], { starred: !starred })
      this.counts.starred -= starred ? 1 : -1
      this.toast(`Couldn't update the star. ${(e as Error).message}`)
    }
  }

  toggleRead() {
    const e = this.entries.find((x) => x.id === this.selectedId) ?? this.article?.entry
    if (!e) return
    clearTimeout(this.dwell)
    this.setRead([e.id], !e.read)
  }

  toggleStar() {
    const e = this.entries.find((x) => x.id === this.selectedId) ?? this.article?.entry
    if (e) this.setStarred(e.id, !e.starred)
  }

  async markAllRead() {
    const s = this.source
    if (s.kind === 'view' && s.view === 'starred') return
    const scope =
      s.kind === 'feed' ? { feed: s.id } : s.kind === 'folder' ? { folder: s.id } : { view: s.view === 'today' ? 'today' : undefined }
    try {
      const { ids } = await api.markAllRead(scope, this.boundary)
      if (!ids.length) {
        this.toast('Nothing to mark as read.')
        return
      }
      this.patch(ids, { read: true })
      await this.refreshTree()
      this.toast(`Marked ${ids.length} ${ids.length === 1 ? 'article' : 'articles'} as read.`, {
        label: 'Undo',
        run: async () => {
          for (let i = 0; i < ids.length; i += 1000) await api.setState('unread', ids.slice(i, i + 1000))
          this.patch(ids, { read: false })
          await this.refreshTree()
          this.toast('Restored as unread.')
        },
      })
    } catch (e) {
      if (!isNetworkError(e)) {
        this.toast(`Couldn't mark as read. ${(e as Error).message}`)
        return
      }
      // Offline: mark what's visible now; the server applies the same
      // boundary when the change is sent.
      const ids = this.entries.filter((x) => !x.read).map((x) => x.id)
      this.adjustCounts(ids, true)
      this.patch(ids, { read: true })
      offline.patchEntries(ids, { read: true }).catch(() => {})
      await this.queue({ kind: 'markAll', scope, before: this.boundary, at: Date.now() })
      this.toast("Marked as read. It will sync when you're back online.")
    }
  }

  async showPending() {
    await this.load()
    document.querySelector('.wire-scroll')?.scrollTo({ top: 0 })
  }

  // ---------------------------------------------------------------- toasts

  toast(message: string, action?: Toast['action']) {
    const id = ++this.toastSeq
    this.toasts = [...this.toasts, { id, message, action }]
    setTimeout(() => this.dismiss(id), action ? 8000 : 4000)
  }

  dismiss(id: number) {
    this.toasts = this.toasts.filter((t) => t.id !== id)
  }
}

export const app = new AppState()

/** Any modal overlay currently owns the keyboard. */
export function overlayOpen(): boolean {
  return app.paletteOpen || app.shortcutsOpen || app.addFeedOpen || app.settingsOpen
}
