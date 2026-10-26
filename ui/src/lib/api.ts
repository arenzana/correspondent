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
  unread: number
  total: number
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
  for (const [k, v] of Object.entries(q)) if (v !== undefined && v !== '' && v !== 0) p.set(k, String(v))
  const s = p.toString()
  return s ? '?' + s : ''
}

export const api = {
  login: (username: string, password: string) => request<{ username: string }>('POST', '/session', { username, password }),
  logout: () => request<void>('DELETE', '/session'),
  me: () => request<{ username: string; hasApiPassword: boolean; prefs: Record<string, unknown> }>('GET', '/me'),
  savePrefs: (prefs: object) => request<void>('PUT', '/prefs', prefs),
  setApiPassword: (password: string) => request<void>('PUT', '/api-password', { password }),
  tree: () => request<{ folders: Folder[]; feeds: Feed[]; counts: Counts }>('GET', '/tree'),
  entries: (q: EntryQuery) => request<{ entries: Entry[]; cursor: string }>('GET', '/entries' + qs(q)),
  entry: (id: ID) => request<{ entry: Entry; enclosures: Enclosure[] }>('GET', '/entries/' + id),
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
  subscribe: (url: string, title: string, folderId: number | null, folderName?: string) =>
    request<{ id: number }>('POST', '/feeds', { url, title, folderId, folderName }),
  updateFeed: (id: number, patch: { title?: string; folderId?: number; folderName?: string; noFolder?: boolean; paused?: boolean }) =>
    request<void>('PATCH', '/feeds/' + id, patch),
  deleteFeed: (id: number) => request<void>('DELETE', '/feeds/' + id),
  refreshFeed: (id: number) => request<void>('POST', `/feeds/${id}/refresh`),
  renameFolder: (id: number, name: string) => request<void>('PATCH', '/folders/' + id, { name }),
  deleteFolder: (id: number) => request<void>('DELETE', '/folders/' + id),
  importOpml: (file: File) => request<{ added: number }>('POST', '/opml', file, true),
}
