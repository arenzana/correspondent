// Application state. One reactive object, shared by every component.
// Mutations are optimistic: the UI changes immediately and rolls back if the
// server refuses. Without a network, the reader works from its offline copy
// (lib/offline) and queues changes until the connection returns.

import { api, ApiError, type SavedSearch, type AIConfig, type SpeechConfig, type Role, type Counts, type Entry, type Enclosure, type Feed, type Folder, type ID } from './api'
import type { Spotlight } from './reading'
import { coarse, estimateFromTimezone, sunTheme } from './sun'
import * as offline from './offline/db'
import { flush as flushOutbox } from './offline/outbox'
import * as audio from './offline/audio'
import { APP_ICONS, type AppIcon } from './icons'

/**
 * A failure to reach the server at all, as opposed to the server saying no:
 * fetch rejects with a TypeError ("Failed to fetch", "Load failed", …).
 */
const isNetworkError = (e: unknown) => e instanceof TypeError && !(e instanceof ApiError)

export type View = 'unread' | 'today' | 'starred' | 'all'
export type Source =
  | { kind: 'view'; view: View }
  | { kind: 'feed'; id: number }
  | { kind: 'folder'; id: number }
  | { kind: 'search'; id: number }
export type Theme = 'auto' | 'sun' | 'day' | 'night' | 'evening'
export type Pane = 'nav' | 'list' | 'article'
export { APP_ICONS, type AppIcon }

/** Interface text sizes offered in Settings. */
export const UI_SCALES = [
  { value: 0.92, label: 'Small' },
  { value: 1, label: 'Default' },
  { value: 1.1, label: 'Large' },
  { value: 1.2, label: 'Larger' },
] as const

export interface Prefs {
  theme: Theme
  readSize: number
  measure: number
  density: 'comfortable' | 'compact'
  readFont: 'serif' | 'sans'
  /** Bold the start of each word, for the eye to jump from word to word. */
  bionic: boolean
  /** One paragraph or one line clear, the rest dimmed. */
  spotlight: Spotlight
  /** Interface text: a scale on every size, and the typeface. */
  uiScale: number
  uiFont: 'grotesk' | 'system'
  unreadOnly: boolean
  markOnOpen: boolean
  /** Read by the server too: sponsored posts arrive already read. */
  skipSponsored: boolean
  /** Answered the question on a sponsored post with "keep showing them". */
  sponsoredAsked: boolean
  /** Keep read-aloud audio on this device, to listen offline. */
  offlineAudio: boolean
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
  bionic: false,
  spotlight: 'off',
  uiScale: 1,
  uiFont: 'grotesk',
  unreadOnly: true,
  markOnOpen: true,
  skipSponsored: false,
  sponsoredAsked: false,
  offlineAudio: true,
  sidebar: true,
  place: null,
  navWidth: null,
  listWidth: null,
  appIcon: 'wire',
}

/** What the player is playing; local means a saved copy on this device. */
export interface Listening {
  id: ID
  url: string
  local: boolean
  title: string
  feed: string
}

interface Toast {
  id: number
  message: string
  action?: { label: string; run: () => void }
}

/**
 * Sizes belong to the device, not the account: a phone, a laptop and the
 * apps each want their own. They're kept in this browser and never sent.
 */
const DEVICE_KEYS = ['readSize', 'measure', 'uiScale'] as const

function deviceSizes(): Partial<Prefs> | null {
  try {
    const raw = localStorage.getItem('correspondent:sizes')
    return raw ? (JSON.parse(raw) as Partial<Prefs>) : null
  } catch {
    return null
  }
}

function saveDeviceSizes(prefs: Prefs) {
  try {
    localStorage.setItem('correspondent:sizes', JSON.stringify(Object.fromEntries(DEVICE_KEYS.map((k) => [k, prefs[k]]))))
  } catch {
    // Private windows may refuse; sizes then last for the visit.
  }
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
  role = $state<Role>('user')
  /** An invitation or reset link being opened (#/invite/<token>). */
  inviteToken = $state(inviteFromHash())
  readInvite() {
    this.inviteToken = inviteFromHash()
  }
  passkeysAvailable = $state(false)
  /** Whether this server can use AI, and the reader's setup if any. */
  aiAvailable = $state(false)
  ai = $state<AIConfig | null>(null)
  speech = $state<SpeechConfig | null>(null)
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
  searches = $state<SavedSearch[]>([])
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
  /** Whether the list is scrolled to its top (set by the list itself). */
  atTop = $state(true)
  /** Until when the reader is touching the list (ms since epoch): no
   *  arrivals then, so nothing moves under a finger about to tap. */
  listBusyUntil = 0
  /** Entries that just arrived live, for the list to animate in once. */
  arriving = $state<Set<string>>(new Set())

  selectedId = $state<ID | null>(null)
  article = $state<{ entry: Entry; enclosures: Enclosure[] } | null>(null)
  articleError = $state('')
  pane = $state<Pane>('list')
  /** Phones: the header and tab bar step aside while scrolling down (lib/bars). */
  barsHidden = $state(false)
  focusMode = $state(false)

  /** Which page fills the main area: the reader, or subscription management. */
  page = $state<'reader' | 'manage' | 'settings' | 'admin'>('reader')
  paletteOpen = $state(false)
  shortcutsOpen = $state(false)
  addFeedOpen = $state(false)
  readingOpen = $state(false)
  navOpen = $state(false) // drawer on medium widths
  toasts = $state<Toast[]>([])

  feedById = $derived(new Map(this.feeds.map((f) => [f.id, f])))
  selectedIndex = $derived(this.entries.findIndex((e) => e.id === this.selectedId))

  title = $derived.by(() => {
    const s = this.source
    if (s.kind === 'feed') return this.feedById.get(s.id)?.title || 'Feed'
    if (s.kind === 'folder') return this.folders.find((f) => f.id === s.id)?.name ?? 'Folder'
    if (s.kind === 'search') return this.searches.find((x) => x.id === s.id)?.name ?? 'Search'
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
    // "Auto" follows the system in CSS; the status bar colour must follow too.
    window.matchMedia?.('(prefers-color-scheme: dark)').addEventListener?.('change', () => this.applyPrefs())
    try {
      let me: Awaited<ReturnType<typeof api.me>>
      try {
        me = await api.me()
        // Another reader's offline copy never shows to this one: a device
        // signed in as someone else is wiped first.
        const before = await offline.getMeta<{ username: string }>('me').catch(() => undefined)
        if (before && before.username !== me.username) await offline.clear().catch(() => {})
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
      this.aiAvailable = me.aiAvailable
      this.ai = me.ai
      this.speech = me.speech
      this.role = me.role ?? 'user'
      this.passkeysAvailable = !!me.passkeysAvailable
      this.version = me.version
      this.commit = me.commit
      // This browser's own sizes win; the first time, it starts from the account's.
      const sizes = deviceSizes()
      this.prefs = { ...defaultPrefs, ...(me.prefs as Partial<Prefs>), ...(sizes ?? {}) }
      if (!sizes) saveDeviceSizes(this.prefs)
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
    await this.signedIn()
  }

  /** After any sign-in (password, passkey, invitation): start the app. */
  async signedIn() {
    if (location.hash.startsWith('#/invite/')) history.replaceState(null, '', '#/unread')
    this.inviteToken = ''
    this.booting = true
    await this.boot()
  }

  async signOut() {
    await api.logout().catch(() => {})
    // Leave nothing readable behind on a shared device.
    await offline.clear().catch(() => {})
    await audio.clearAudio().catch(() => {})
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
      const withAudio: ID[] = []
      for (const [view, pages] of [['unread', 6], ['starred', 3]] as const) {
        let cursor = ''
        for (let i = 0; i < pages; i++) {
          const r = await api.entries({ view, content: true, limit: 100, cursor: cursor || undefined })
          await offline.saveEntries(r.entries.map((e) => ({ ...e, content: e.content ?? '' })))
          withAudio.push(...r.entries.filter((e) => e.audio).map((e) => e.id))
          if (!r.cursor) break
          cursor = r.cursor
        }
      }
      // A plain snapshot: IndexedDB cannot store Svelte's reactive proxies.
      await offline.saveTree($state.snapshot({ folders: this.folders, feeds: this.feeds, counts: this.counts, searches: this.searches }))
      await offline.prune()
      await this.syncAudio(withAudio)
      this.syncedAt = Date.now()
      await offline.setMeta('syncedAt', this.syncedAt)
    } catch (e) {
      if (isNetworkError(e)) this.online = false
    } finally {
      this.syncing = false
    }
  }

  /**
   * Keeps audio for listening offline: what's playing and queued, then the
   * newest unread and starred articles that have audio, up to AUDIO_LIMIT.
   * Nothing is downloaded when the device asks to save data.
   */
  private async syncAudio(withAudio: ID[]) {
    if (!this.prefs.offlineAudio) return audio.clearAudio()
    const keep = new Set(
      [this.listening?.id, ...this.upNext.map((e) => e.id), ...withAudio].filter((id): id is ID => !!id).slice(0, audio.AUDIO_LIMIT),
    )
    await audio.keepOnly(keep)
    const saveData = (navigator as Navigator & { connection?: { saveData?: boolean } }).connection?.saveData
    if (saveData) return
    for (const id of keep) await audio.save(id).catch(() => {})
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
      return f ? { ...e, read: f.read, starred: f.starred, imageUrl: f.imageUrl, excerpt: f.excerpt } : e
    })
  }

  private syncSoonTimer: ReturnType<typeof setTimeout> | undefined
  private async onNewEntries() {
    clearTimeout(this.syncSoonTimer)
    this.syncSoonTimer = setTimeout(() => void this.syncOffline(), 60_000)
    await this.refreshTree().catch(() => {})
  }

  private checkingArrivals = false
  /**
   * Brings articles newer than the list's boundary into the list: in place
   * when it's at the top, else offered as "N new articles". Called whenever
   * the counts show something newer than the list has.
   */
  async checkArrivals() {
    const starred = this.source.kind === 'view' && this.source.view === 'starred'
    if (starred || this.loading || this.checkingArrivals || this.page !== 'reader') return
    this.checkingArrivals = true
    try {
      const boundary = this.boundary
      const target = this.counts.newest || '0'
      const fresh = await api.entries({ ...this.query_(), limit: 200 }).catch(() => null)
      // The list was reloaded meanwhile: it has them already.
      if (!fresh || boundary !== this.boundary) return
      const newer = fresh.entries.filter((e) => BigInt(e.id) > BigInt(this.boundary))
      if (!newer.length) {
        // Nothing for this list (read elsewhere, or another list's articles).
        if (BigInt(target) > BigInt(this.boundary)) this.boundary = target
        return
      }
      if (!this.atTop || this.query || Date.now() < this.listBusyUntil) {
        // Reading further down: never move the list; offer the new ones.
        this.pending = newer.length
        return
      }
      this.mergeArrivals(fresh.entries, newer)
    } finally {
      this.checkingArrivals = false
    }
  }

  /**
   * Puts newly arrived entries into the list in their place (the server's
   * order), keeping everything already loaded, and marks them so the list
   * animates them in.
   */
  mergeArrivals(page: Entry[], newer: Entry[]) {
    const have = new Set(this.entries.map((e) => e.id))
    const added = newer.filter((e) => !have.has(e.id))
    if (!added.length) return
    const inPage = new Set(page.map((e) => e.id))
    const latest = new Map(page.map((e) => [e.id, e]))
    // The fresh page in order, then anything loaded beyond it.
    this.entries = [
      ...page.filter((e) => have.has(e.id) || added.some((a) => a.id === e.id)).map((e) => latest.get(e.id)!),
      ...this.entries.filter((e) => !inPage.has(e.id)),
    ]
    this.boundary = newer.reduce((m, e) => (BigInt(e.id) > BigInt(m) ? e.id : m), this.boundary)
    this.pending = 0
    this.arriving = new Set(added.map((e) => e.id))
    offline.saveEntries(added).catch(() => {})
    clearTimeout(this.arrivingTimer)
    this.arrivingTimer = setTimeout(() => (this.arriving = new Set()), 1500)
  }
  private arrivingTimer: ReturnType<typeof setTimeout> | undefined

  // ------------------------------------------------------------------ prefs

  setPrefs(patch: Partial<Prefs>) {
    this.prefs = { ...this.prefs, ...patch }
    this.applyPrefs()
    // Sizes stay in this browser; of the rest, only what changed is sent,
    // so another device's newer settings stay.
    const shared: Partial<Prefs> = { ...patch }
    if (DEVICE_KEYS.some((k) => k in shared)) {
      for (const k of DEVICE_KEYS) delete shared[k]
      saveDeviceSizes(this.prefs)
    }
    if (Object.keys(shared).length === 0) return
    this.prefsPatch = { ...this.prefsPatch, ...shared }
    clearTimeout(this.prefsTimer)
    this.prefsTimer = setTimeout(() => {
      const changes = this.prefsPatch
      this.prefsPatch = {}
      api.patchPrefs(changes).catch(() => {})
    }, 400)
  }
  private prefsTimer: ReturnType<typeof setTimeout> | undefined
  private prefsPatch: Partial<Prefs> = {}

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
    // The browser bar and the installed app's status bar match the headers.
    // iOS reads a fresh theme-color tag but ignores edits to an existing
    // one, and paints the status bar from the page background, so replace
    // the tag and set the background outright when the theme changes.
    const sheet = getComputedStyle(root).getPropertyValue('--sheet').trim()
    if (sheet) {
      const current = document.querySelectorAll('meta[name="theme-color"]')
      if (current.length !== 1 || current[0].getAttribute('content') !== sheet) {
        current.forEach((m) => m.remove())
        const meta = document.createElement('meta')
        meta.name = 'theme-color'
        meta.content = sheet
        document.head.append(meta)
      }
      if (window.matchMedia?.('(max-width: 699px)').matches) {
        root.style.backgroundColor = sheet
        document.body.style.backgroundColor = sheet
      } else {
        root.style.backgroundColor = document.body.style.backgroundColor = ''
      }
    }
    const icon = APP_ICONS.some((i) => i.id === this.prefs.appIcon) ? this.prefs.appIcon : 'wire'
    const setHref = (id: string, file: string) => document.getElementById(id)?.setAttribute('href', `/icons/${icon}/${file}`)
    setHref('favicon', 'favicon.svg')
    setHref('favicon-png', 'favicon-32.png')
    setHref('touch-icon', 'apple-touch-icon.png')
    root.dataset.density = this.prefs.density
    root.style.setProperty('--read-size', this.prefs.readSize + 'px')
    root.style.setProperty('--read-measure', this.prefs.measure + 'ch')
    root.style.setProperty('--read-family', this.prefs.readFont === 'serif' ? 'var(--font-read)' : 'var(--font-grotesk)')
    root.style.setProperty('--ui-scale', String(UI_SCALES.some((u) => u.value === this.prefs.uiScale) ? this.prefs.uiScale : 1))
    root.style.setProperty('--font-ui', this.prefs.uiFont === 'system' ? 'var(--font-system)' : 'var(--font-grotesk)')
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
    this.searches = t.searches ?? []
    // However the counts were refreshed (a live event, coming back to the
    // app, a reconnect), articles newer than the list holds are brought in.
    if (BigInt(t.counts.newest || '0') > BigInt(this.boundary || '0')) void this.checkArrivals()
  }

  private query_() {
    const s = this.source
    const q: Parameters<typeof api.entries>[0] = { q: this.query || undefined }
    if (s.kind === 'view') q.view = s.view === 'all' ? undefined : s.view
    if (s.kind === 'feed') q.feed = s.id
    if (s.kind === 'folder') q.folder = s.id
    if (s.kind === 'search') q.q = this.searches.find((x) => x.id === s.id)?.query ?? this.query
    if (s.kind !== 'view' && this.prefs.unreadOnly) q.view = 'unread'
    return q
  }

  async load() {
    const seq = ++this.loadSeq
    this.loading = true
    this.loadError = ''
    this.pending = 0
    this.arriving = new Set()
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

  /** Open subscription management. */
  manage() {
    this.page = 'manage'
    this.navOpen = false
    this.pane = 'list'
    if (location.hash !== '#/manage') history.pushState(null, '', '#/manage')
  }

  /** Open Settings, optionally at a section (its element ID). */
  openSettings(section?: string) {
    this.page = 'settings'
    this.navOpen = false
    this.pane = 'list'
    this.settingsSection = section ?? ''
    if (!location.hash.startsWith('#/settings')) history.pushState(null, '', '#/settings')
  }
  /** Open the admin page (admins only). */
  openAdmin() {
    if (this.role !== 'admin') return
    this.page = 'admin'
    this.navOpen = false
    this.pane = 'list'
    if (location.hash !== '#/admin') history.pushState(null, '', '#/admin')
  }

  /** The section Settings should show first, if any. */
  settingsSection = $state('')

  go(source: Source) {
    this.page = 'reader'
    this.source = source
    this.selectedId = null
    this.article = null
    this.navOpen = false
    this.pane = 'list'
    this.writeHash()
    this.load()
  }

  // ------------------------------------------------------ saved searches

  /** Keeps the current search in the sidebar, and shows it from there. */
  async saveSearch() {
    const query = this.query.trim()
    if (!query) return
    try {
      const { search } = await api.saveSearch(query)
      await this.refreshTree()
      this.query = ''
      this.go({ kind: 'search', id: search.id })
      this.toast(`Saved the search “${search.name}”. It's in the sidebar.`)
    } catch (e) {
      this.toast(`Couldn't save the search. ${(e as Error).message}`)
    }
  }

  async renameSearch(id: number, name: string) {
    name = name.trim()
    const s = this.searches.find((x) => x.id === id)
    if (!s || !name || name === s.name) return
    const before = s.name
    this.searches = this.searches.map((x) => (x.id === id ? { ...x, name } : x))
    try {
      await api.renameSearch(id, name)
    } catch (e) {
      this.searches = this.searches.map((x) => (x.id === id ? { ...x, name: before } : x))
      this.toast(`Couldn't rename the search. ${(e as Error).message}`)
    }
  }

  async deleteSearch(id: number) {
    const s = this.searches.find((x) => x.id === id)
    if (!s) return
    try {
      await api.deleteSearch(id)
      this.searches = this.searches.filter((x) => x.id !== id)
      if (this.source.kind === 'search' && this.source.id === id) this.go({ kind: 'view', view: 'unread' })
      this.toast(`Removed the search “${s.name}”.`, {
        label: 'Undo',
        run: async () => {
          const { search } = await api.saveSearch(s.query, s.name)
          await this.refreshTree()
          this.go({ kind: 'search', id: search.id })
        },
      })
    } catch (e) {
      this.toast(`Couldn't remove the search. ${(e as Error).message}`)
    }
  }

  /** Opens an article found by search, with the matching list behind it. */
  openFound(query: string, id: ID) {
    this.query = query
    this.go({ kind: 'view', view: 'all' })
    this.select(id)
  }

  sourceKey(s: Source = this.source): string {
    return s.kind === 'view' ? s.view : `${s.kind}/${s.id}`
  }

  writeHash() {
    const h = '#/' + this.sourceKey() + (this.selectedId ? '/e/' + this.selectedId : '')
    if (location.hash !== h) history.pushState(null, '', h)
  }

  readHash() {
    if (location.hash === '#/manage' || location.hash.startsWith('#/settings') || location.hash === '#/admin') {
      this.page = location.hash === '#/manage' ? 'manage' : location.hash === '#/admin' && this.role === 'admin' ? 'admin' : 'settings'
      this.pane = 'list'
      return
    }
    this.page = 'reader'
    const m = location.hash.match(/^#\/(unread|today|starred|all|feed\/\d+|folder\/\d+|search\/\d+)(?:\/e\/(\d+))?$/)
    if (!m) return
    const [kind, id] = m[1].split('/')
    this.source = id ? { kind: kind as 'feed' | 'folder' | 'search', id: Number(id) } : { kind: 'view', view: kind as View }
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
      // The server may have found a picture or excerpt since the list loaded
      // (a full article fetched in the background): the row shows it too.
      this.patch([id], { imageUrl: r.entry.imageUrl, excerpt: r.entry.excerpt })
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

  /** Back from Settings, Subscriptions or Admin to the articles. */
  leavePage() {
    this.page = 'reader'
    this.pane = 'list'
    this.writeHash()
  }

  /** Closes the open article, back to the list (Esc). */
  closeArticle() {
    if (!this.selectedId && !this.article) return
    clearTimeout(this.dwell)
    this.selectedId = null
    this.article = null
    this.pane = 'list'
    this.writeHash()
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
    // A search marks only its matches.
    const q = this.query_().q
    const scope =
      s.kind === 'feed' ? { feed: s.id, q } : s.kind === 'folder' ? { folder: s.id, q }
      : s.kind === 'search' ? { q } : { view: s.view === 'today' ? 'today' : undefined, q }
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

  // ------------------------------------------------- subscription management

  private feed(id: number) {
    return this.feeds.find((f) => f.id === id)
  }

  async renameFeed(id: number, title: string) {
    const f = this.feed(id)
    const before = f?.title
    title = title.trim()
    if (!f || !title || title === before) return
    f.title = title
    try {
      await api.updateFeed(id, { title })
    } catch (e) {
      f.title = before!
      this.toast(`Couldn't rename the feed. ${(e as Error).message}`)
    }
  }

  /** Move feeds to a folder (an ID), a new folder (a name), or out of any folder (null). */
  async moveFeeds(ids: number[], to: number | string | null) {
    try {
      for (const id of ids) {
        if (typeof to === 'string') await api.updateFeed(id, { folderName: to })
        else if (to === null) await api.updateFeed(id, { noFolder: true })
        else await api.updateFeed(id, { folderId: to })
      }
      await this.refreshTree()
      const where = to === null ? 'out of folders' : typeof to === 'string' ? `to ${to}` : `to ${this.folders.find((f) => f.id === to)?.name ?? 'the folder'}`
      this.toast(`Moved ${ids.length === 1 ? 'the feed' : `${ids.length} feeds`} ${where}.`)
    } catch (e) {
      await this.refreshTree().catch(() => {})
      this.toast(`Couldn't move the feeds. ${(e as Error).message}`)
    }
  }

  /** Point a feed at a new address; returns an error message, or '' on success. */
  async setFeedURL(id: number, url: string): Promise<string> {
    try {
      await api.updateFeed(id, { url: url.trim() })
      await api.refreshFeed(id).catch(() => {})
      await this.refreshTree()
      const f = this.feed(id)
      this.toast(f?.errorCount ? `Saved, but the new address failed too: ${f.lastError}` : 'Address saved and the feed updated.')
      return ''
    } catch (e) {
      return (e as Error).message
    }
  }

  async setPaused(ids: number[], paused: boolean) {
    for (const id of ids) {
      const f = this.feed(id)
      if (f) f.paused = paused
    }
    try {
      for (const id of ids) await api.updateFeed(id, { paused })
      if (!paused) for (const id of ids) await api.refreshFeed(id).catch(() => {})
      await this.refreshTree()
      this.toast(`${paused ? 'Paused' : 'Resumed'} ${ids.length === 1 ? 'the feed' : `${ids.length} feeds`}.`)
    } catch (e) {
      await this.refreshTree().catch(() => {})
      this.toast(`Couldn't ${paused ? 'pause' : 'resume'} the feeds. ${(e as Error).message}`)
    }
  }

  async refreshFeedNow(id: number) {
    try {
      await api.refreshFeed(id)
      await this.refreshTree()
      const f = this.feed(id)
      this.toast(f?.errorCount ? `Still failing: ${f.lastError}` : `Updated ${f?.title ?? 'the feed'}.`)
    } catch (e) {
      this.toast(`Couldn't update the feed. ${(e as Error).message}`)
    }
  }

  /** Unsubscribe; the feeds' articles go with them. */
  async deleteFeeds(ids: number[]) {
    const names = ids.map((id) => this.feed(id)?.title ?? 'feed')
    try {
      for (const id of ids) await api.deleteFeed(id)
      if (this.source.kind === 'feed' && ids.includes(this.source.id)) this.source = { kind: 'view', view: 'unread' }
      await this.refreshTree()
      this.toast(ids.length === 1 ? `Deleted ${names[0]}.` : `Deleted ${ids.length} feeds.`)
    } catch (e) {
      await this.refreshTree().catch(() => {})
      this.toast(`Couldn't delete. ${(e as Error).message}`)
    }
  }

  async createFolder(name: string) {
    name = name.trim()
    if (!name) return
    try {
      await api.createFolder(name)
      await this.refreshTree()
    } catch (e) {
      this.toast(`Couldn't create the folder. ${(e as Error).message}`)
    }
  }

  async renameFolder(id: number, name: string) {
    const folder = this.folders.find((f) => f.id === id)
    const before = folder?.name
    name = name.trim()
    if (!folder || !name || name === before) return
    folder.name = name
    try {
      await api.renameFolder(id, name)
    } catch (e) {
      folder.name = before!
      this.toast(`Couldn't rename the folder. ${(e as Error).message}`)
    }
  }

  /** Delete a folder; its feeds stay, outside any folder. */
  async deleteFolder(id: number) {
    const name = this.folders.find((f) => f.id === id)?.name
    try {
      await api.deleteFolder(id)
      if (this.source.kind === 'folder' && this.source.id === id) this.source = { kind: 'view', view: 'unread' }
      await this.refreshTree()
      this.toast(`Deleted the folder ${name}. Its feeds are kept.`)
    } catch (e) {
      this.toast(`Couldn't delete the folder. ${(e as Error).message}`)
    }
  }

  /** The article being read aloud; it keeps playing while you browse. */
  listening = $state<Listening | null>(null)
  /** Articles to play after it, in order. */
  upNext = $state<Entry[]>([])
  preparingAudio = $state<ID | null>(null)

  /** Reads the open article aloud: its existing audio, or new audio made with the reader's key. */
  async readAloud() {
    const e = this.article?.entry
    if (!e || this.preparingAudio) return
    if (this.listening?.id === e.id) return
    this.upNext = []
    try {
      await this.play(e, true)
    } catch (err) {
      this.toast(`Couldn't read this aloud. ${(err as Error).message}`)
    }
  }

  /**
   * Plays the list's unread articles that already have audio, in order, from
   * the open one if it's in the list. It never makes audio: that happens only
   * for an article the reader picks (readAloud).
   */
  async listenToList() {
    const from = Math.max(0, this.selectedIndex)
    const queue = this.entries.slice(from).filter((e) => !e.read || e.id === this.selectedId)
    if (!queue.length) {
      this.toast('Nothing unread here to listen to.')
      return
    }
    this.upNext = queue
    await this.playNext()
    if (!this.listening || !queue.some((e) => e.id === this.listening?.id)) {
      this.toast(
        !this.online ? 'None of these are saved for listening offline.'
        : 'None of these have audio yet. Open an article and press l to have it read aloud.',
      )
    }
  }

  /** Moves to the next article in the queue that has audio, skipping any that don't. */
  async playNext() {
    while (this.upNext.length) {
      const [next, ...rest] = this.upNext
      this.upNext = rest
      try {
        await this.play(next, false)
        return
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 404) && !isNetworkError(err)) {
          this.toast(`Skipped “${next.title}”. ${(err as Error).message}`)
        }
      }
    }
  }

  /** The player reached the end: the article counts as read, and the queue moves on. */
  async listenEnded() {
    const done = this.listening
    if (!done) return
    void this.setRead([done.id], true)
    if (this.upNext.length) await this.playNext()
  }

  stopListening() {
    if (this.listening?.local) URL.revokeObjectURL(this.listening.url)
    this.listening = null
    this.upNext = []
  }

  /** Plays an article: a saved copy, else its audio on the server, made first only when make is set. */
  private async play(e: Entry, make: boolean) {
    this.preparingAudio = e.id
    try {
      let url = await audio.savedURL(e.id).catch(() => null)
      const local = url !== null
      url ??= (await (make ? api.makeAudio(e.id) : api.findAudio(e.id))).url
      if (this.listening?.local) URL.revokeObjectURL(this.listening.url)
      this.listening = { id: e.id, url, local, title: e.title, feed: this.feedById.get(e.feedId)?.title ?? '' }
      this.patch([e.id], { audio: true })
      offline.patchEntries([e.id], { audio: true }).catch(() => {})
      if (this.article?.entry.id === e.id) this.article = { ...this.article, entry: { ...this.article.entry, audio: true } }
      if (!local && this.prefs.offlineAudio) void audio.save(e.id).catch(() => {})
    } finally {
      this.preparingAudio = null
    }
  }


  /** The article whose summary is showing, and the one being summarized. */
  summaryOpen = $state<ID | null>(null)
  summarizing = $state<ID | null>(null)

  /** Shows or hides the open article's summary, asking for one if it has none yet. */
  async summarize() {
    const e = this.article?.entry
    if (!e || this.summarizing) return
    if (this.summaryOpen === e.id) {
      this.summaryOpen = null
      return
    }
    if (e.summary) {
      this.summaryOpen = e.id
      return
    }
    this.summarizing = e.id
    try {
      const { summary } = await api.summarize(e.id)
      if (this.article?.entry.id === e.id) this.article = { ...this.article, entry: { ...this.article.entry, summary } }
      offline.patchEntries([e.id], { summary }).catch(() => {})
      this.summaryOpen = e.id
    } catch (err) {
      this.toast(`Couldn't summarize this article. ${(err as Error).message}`)
    } finally {
      this.summarizing = null
    }
  }

  /** The answer to "would you rather not see sponsored posts?". */
  answerSponsored(skip: boolean) {
    if (!skip) {
      this.setPrefs({ sponsoredAsked: true })
      return
    }
    // The server marks the waiting ones read when it sees the setting.
    this.setPrefs({ skipSponsored: true, sponsoredAsked: true })
    const e = this.article?.entry
    if (e && !e.read) void this.setRead([e.id], true)
    this.toast('Sponsored posts will arrive already read. Change this in Settings → Reading.')
  }

  fetchingFull = $state(false)

  /** Replace the open article's text with the full article from its page. */
  async loadFullArticle() {
    const id = this.article?.entry.id
    if (!id || this.fetchingFull) return
    this.fetchingFull = true
    try {
      const r = await api.fullArticle(id)
      if (this.selectedId === id) this.article = { ...r, entry: { ...r.entry, content: r.entry.content ?? '' } }
      this.patch([id], { fullText: true, fullError: '', imageUrl: r.entry.imageUrl, excerpt: r.entry.excerpt })
      offline.saveEntries([{ ...r.entry, content: r.entry.content ?? '' }]).catch(() => {})
    } catch (e) {
      this.toast((e as Error).message)
      this.patch([id], { fullError: (e as Error).message })
    } finally {
      this.fetchingFull = false
    }
  }

  /** Go back to the text the feed published. */
  async showFeedText() {
    const id = this.article?.entry.id
    if (!id) return
    try {
      const r = await api.feedText(id)
      if (this.selectedId === id) this.article = { ...r, entry: { ...r.entry, content: r.entry.content ?? '' } }
      this.patch([id], { fullText: false })
      offline.saveEntries([{ ...r.entry, content: r.entry.content ?? '' }]).catch(() => {})
    } catch (e) {
      this.toast(`Couldn't switch back. ${(e as Error).message}`)
    }
  }

  /** Fetch full articles for every new article in a feed (and its latest ones now). */
  async setFeedFullText(id: number, on: boolean): Promise<boolean> {
    const feed = this.feeds.find((f) => f.id === id)
    if (feed) feed.fullText = on
    try {
      await api.updateFeed(id, { fullText: on })
      this.toast(on ? `Fetching full articles for ${feed?.title ?? 'this feed'}. Recent ones are on their way.` : 'Back to the text the feed publishes.')
      return true
    } catch (e) {
      if (feed) feed.fullText = !on
      this.toast(`Couldn't change it. ${(e as Error).message}`)
      return false
    }
  }

  /** Fetch every article of a feed in full, starting with the open one. */
  async fullTextForFeed(id: number) {
    if (!(await this.setFeedFullText(id, true))) return
    if (this.article?.entry.feedId === id && !this.article.entry.fullText) await this.loadFullArticle()
  }

  /** Write a new parser for a page feed whose page changed. */
  async rebuildFeed(id: number) {
    try {
      const { feed } = await api.rebuildFeed(id)
      const i = this.feeds.findIndex((f) => f.id === id)
      if (i >= 0) this.feeds[i] = { ...this.feeds[i], ...feed }
      this.toast(`Rebuilt ${feed.title}. New articles will follow.`)
    } catch (e) {
      this.toast(`Couldn't rebuild it. ${(e as Error).message}`)
    }
  }

  /** Opt the feed in or out of notifications. */
  async setFeedNotify(id: number, notify: boolean) {
    const feed = this.feeds.find((f) => f.id === id)
    if (feed) feed.notify = notify
    try {
      await api.updateFeed(id, { notify })
      this.toast(notify ? `Notifications on for ${feed?.title ?? 'this feed'}.` : 'Notifications off for this feed.')
    } catch (e) {
      if (feed) feed.notify = !notify
      this.toast(`Couldn't change notifications. ${(e as Error).message}`)
    }
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

function inviteFromHash(): string {
  const m = typeof location === 'undefined' ? null : location.hash.match(/^#\/invite\/([A-Za-z0-9_-]{16,64})$/)
  return m ? m[1] : ''
}

export const app = new AppState()

/** Any modal overlay currently owns the keyboard. */
export function overlayOpen(): boolean {
  return app.paletteOpen || app.shortcutsOpen || app.addFeedOpen
}
