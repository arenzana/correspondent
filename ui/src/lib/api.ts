// Thin client for the Correspondent /api/v1. Every mutation carries the
// X-Correspondent header the server requires as CSRF protection.

export type ID = string

export interface Folder {
  id: number
  name: string
  unread: number
}

export interface Feed {
  id: number
  folderId: number | null
  url: string
  siteUrl: string
  title: string
  description: string
  iconUrl: string
  lastFetchAt: number
  lastStatus: number
  errorCount: number
  lastError: string
  paused: boolean
  notify: boolean
  /** New articles are fetched in full from their web page. */
  fullText: boolean
  /** Built from a web page with no feed, read by a shared parser. */
  page: boolean
  unread: number
  total: number
  starred: number
  /** Newest publication time (seconds), 0 if the feed has no articles. */
  latestAt: number
  cadence: number[]
}

export interface Counts {
  unread: number
  starred: number
  today: number
  newest: ID
}

export interface Entry {
  id: ID
  feedId: number
  url: string
  title: string
  author: string
  content?: string
  excerpt: string
  imageUrl: string
  publishedAt: number
  updatedAt: number
  read: boolean
  starred: boolean
  /** The content is the full article fetched from its web page. */
  fullText?: boolean
  /** Why fetching the full article failed, if it did. */
  fullError?: string
}

export interface Enclosure {
  url: string
  type: string
  length?: number
}

export interface Candidate {
  url: string
  title: string
  description: string
  siteUrl: string
  iconUrl: string
  itemCount: number
  latestAt: number
  /** A page with no feed, read by a parser (method: how it finds articles). */
  page?: boolean
  method?: 'hfeed' | 'jsonld' | 'links' | 'css'
  /** The server already had this parser (another reader's). */
  shared?: boolean
  sample?: string[]
}

export type AIProvider = 'anthropic' | 'openai'

export type Role = 'admin' | 'user'

export interface Account {
  id: number
  username: string
  role: Role
  disabled: boolean
  createdAt: number
  lastSeenAt: number
  feeds: number
  articles: number
  passkeys: number
  hasApiPassword: boolean
  hasAI: boolean
}

export interface Invite {
  id: number
  kind: 'invite' | 'reset'
  userId?: number
  username?: string
  role: Role
  note: string
  createdBy: string
  createdAt: number
  expiresAt: number
}

export interface Session {
  id: number
  kind: 'web' | 'greader'
  client: string
  createdAt: number
  lastSeenAt: number
  current: boolean
}

export interface Passkey {
  id: number
  name: string
  createdAt: number
  lastUsedAt: number
}

export interface ServerInfo {
  version: string
  commit: string
  go: string
  startedAt: number
  passkeys: boolean
  databaseBytes?: number
  imageCacheBytes?: number
  backupAt?: number
  stats: {
    accounts: number
    disabled: number
    feeds: number
    failing: number
    paused: number
    articles: number
    parsers: number
    lastFetchAt: number
    polledLastHour: number
  }
  failing: { id: number; title: string; url: string; owner: string; errorCount: number; lastError: string }[]
}

export interface ParserInfo {
  id: number
  url: string
  method: string
  builtin: boolean
  createdBy: string
  updatedAt: number
  feeds: number
  failing: number
}

/** A reader's AI setup; the key itself never comes back, only its hint. */
export interface AIConfig {
  provider: AIProvider
  baseUrl?: string
  model: string
  keyHint?: string
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

async function request<T>(method: string, path: string, body?: unknown, raw = false): Promise<T> {
  const headers: Record<string, string> = {}
  if (method !== 'GET') headers['X-Correspondent'] = '1'
  let payload: BodyInit | undefined
  if (body !== undefined) {
    if (raw) {
      payload = body as BodyInit
    } else {
      headers['Content-Type'] = 'application/json'
      payload = JSON.stringify(body)
    }
  }
  const res = await fetch('/api/v1' + path, { method, headers, body: payload, credentials: 'same-origin' })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 401) window.dispatchEvent(new CustomEvent('correspondent:signed-out'))
    throw new ApiError(res.status, data.error ?? `Request failed (${res.status})`)
  }
  return data as T
}

export interface EntryQuery {
  /** Include full article HTML (for offline reading). */
  content?: boolean
  view?: 'unread' | 'all' | 'starred' | 'today'
  feed?: number
  folder?: number
  q?: string
  cursor?: string
  order?: 'newest' | 'oldest'
  limit?: number
}

function qs(q: object): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(q)) {
    if (v === undefined || v === '' || v === 0 || v === false) continue
    p.set(k, v === true ? '1' : String(v))
  }
  const s = p.toString()
  return s ? '?' + s : ''
}

export const api = {
  login: (username: string, password: string) => request<{ username: string }>('POST', '/session', { username, password }),
  logout: () => request<void>('DELETE', '/session'),
  me: () =>
    request<{
      username: string
      hasApiPassword: boolean
      prefs: Record<string, unknown>
      version: string
      commit: string
      aiAvailable: boolean
      ai: AIConfig | null
      role: Role
      passkeysAvailable: boolean
    }>('GET', '/me'),
  authOptions: () => request<{ passkeys: boolean }>('GET', '/auth'),
  sessions: () => request<{ sessions: Session[] }>('GET', '/sessions'),
  endSession: (id: number) => request<void>('DELETE', `/sessions/${id}`),
  changePassword: (current: string, next: string) => request<void>('PUT', '/password', { current, new: next }),
  invite: (token: string) =>
    request<{ kind: 'invite' | 'reset'; username: string; invitedBy: string; expiresAt: number }>('GET', `/invites/${encodeURIComponent(token)}`),
  acceptInvite: (token: string, username: string, password: string) =>
    request<{ username: string }>('POST', `/invites/${encodeURIComponent(token)}`, { username, password }),
  passkeys: () => request<{ passkeys: Passkey[] }>('GET', '/passkeys'),
  renamePasskey: (id: number, name: string) => request<void>('PATCH', `/passkeys/${id}`, { name }),
  deletePasskey: (id: number) => request<void>('DELETE', `/passkeys/${id}`),
  /** WebAuthn ceremonies: the server's options and the device's answer, as JSON. */
  passkeyRegisterBegin: () => request<{ publicKey: Record<string, unknown> }>('POST', '/passkeys'),
  passkeyRegisterFinish: (credential: unknown, name: string) =>
    request<{ id: number }>('POST', `/passkeys/finish?name=${encodeURIComponent(name)}`, credential),
  passkeyLoginBegin: () => request<{ publicKey: Record<string, unknown> }>('POST', '/session/passkey'),
  passkeyLoginFinish: (credential: unknown) => request<{ username: string }>('POST', '/session/passkey/finish', credential),
  admin: {
    accounts: () => request<{ accounts: Account[]; invites: Invite[] }>('GET', '/admin/accounts'),
    invite: (role: Role, note: string) => request<{ token: string; expiresAt: number }>('POST', '/admin/invites', { role, note }),
    revokeInvite: (id: number) => request<void>('DELETE', `/admin/invites/${id}`),
    updateAccount: (id: number, patch: { role?: Role; disabled?: boolean }) => request<void>('PATCH', `/admin/accounts/${id}`, patch),
    resetLink: (id: number) => request<{ token: string; expiresAt: number }>('POST', `/admin/accounts/${id}/reset`),
    deleteAccount: (id: number) => request<void>('DELETE', `/admin/accounts/${id}`),
    server: () => request<ServerInfo>('GET', '/admin/server'),
    parsers: () => request<{ parsers: ParserInfo[] }>('GET', '/admin/parsers'),
    testParser: (id: number) => request<{ ok: boolean; items?: number; sample?: string[]; error?: string }>('POST', `/admin/parsers/${id}/test`),
    rebuildParser: (id: number) => request<{ method: string; items: number; sample: string[] }>('POST', `/admin/parsers/${id}/rebuild`),
    deleteParser: (id: number) => request<void>('DELETE', `/admin/parsers/${id}`),
  },
  setAI: (config: Omit<AIConfig, 'keyHint'> & { key: string }) => request<{ ai: AIConfig }>('PUT', '/ai', config),
  deleteAI: () => request<void>('DELETE', '/ai'),
  savePrefs: (prefs: object) => request<void>('PUT', '/prefs', prefs),
  setApiPassword: (password: string) => request<void>('PUT', '/api-password', { password }),
  tree: () => request<{ folders: Folder[]; feeds: Feed[]; counts: Counts }>('GET', '/tree'),
  entries: (q: EntryQuery) => request<{ entries: Entry[]; cursor: string }>('GET', '/entries' + qs(q)),
  entry: (id: ID) => request<{ entry: Entry; enclosures: Enclosure[] }>('GET', '/entries/' + id),
  fullArticle: (id: ID) => request<{ entry: Entry; enclosures: Enclosure[] }>('POST', `/entries/${id}/full`),
  feedText: (id: ID) => request<{ entry: Entry; enclosures: Enclosure[] }>('DELETE', `/entries/${id}/full`),
  setState: (action: 'read' | 'unread' | 'star' | 'unstar', ids: ID[]) =>
    request<{ changed: number }>('POST', '/entries/state', { action, ids }),
  markAllRead: (scope: { feed?: number; folder?: number; view?: string }, before: ID) =>
    request<{ ids: ID[] }>('POST', '/entries/mark-all-read', {
      feed: scope.feed ? String(scope.feed) : undefined,
      folder: scope.folder ? String(scope.folder) : undefined,
      view: scope.view,
      before,
    }),
  discover: (url: string) => request<{ candidates: Candidate[] }>('POST', '/discover', { url }),
  discoverAI: (url: string, rebuild = false) => request<{ candidates: Candidate[] }>('POST', '/discover/ai', { url, rebuild }),
  subscribe: (url: string, title: string, folderId: number | null, folderName?: string, page = false) =>
    request<{ id: number }>('POST', '/feeds', { url, title, folderId, folderName, page }),
  rebuildFeed: (id: number) => request<{ feed: Feed }>('POST', `/feeds/${id}/rebuild`),
  pushStatus: (endpoint?: string) =>
    request<{ publicKey: string; subscribed: boolean }>('GET', '/push' + (endpoint ? '?endpoint=' + encodeURIComponent(endpoint) : '')),
  pushSubscribe: (sub: PushSubscriptionJSON) => request<void>('POST', '/push/subscriptions', sub),
  pushUnsubscribe: (endpoint: string) => request<void>('DELETE', '/push/subscriptions', { endpoint }),
  pushTest: () => request<{ sent: number }>('POST', '/push/test'),
  createFolder: (name: string) => request<{ id: number }>('POST', '/folders', { name }),
  updateFeed: (id: number, patch: { title?: string; folderId?: number; folderName?: string; noFolder?: boolean; paused?: boolean; notify?: boolean; url?: string; fullText?: boolean }) =>
    request<void>('PATCH', '/feeds/' + id, patch),
  deleteFeed: (id: number) => request<void>('DELETE', '/feeds/' + id),
  refreshFeed: (id: number) => request<void>('POST', `/feeds/${id}/refresh`),
  renameFolder: (id: number, name: string) => request<void>('PATCH', '/folders/' + id, { name }),
  deleteFolder: (id: number) => request<void>('DELETE', '/folders/' + id),
  importOpml: (file: File) => request<{ added: number }>('POST', '/opml', file, true),
}
