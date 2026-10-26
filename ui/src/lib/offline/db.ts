// Offline storage (IndexedDB). Holds what the reader needs without a
// network: the feed tree, article summaries and, for unread and starred
// articles, their full text; plus the outbox of changes made offline.
// Shared by the app and the service worker.

import { openDB, type DBSchema, type IDBPDatabase } from 'idb'
import type { Counts, Entry, Feed, Folder } from '../api'

export interface Tree {
  folders: Folder[]
  feeds: Feed[]
  counts: Counts
}

/** A change waiting to reach the server. */
export type OutboxOp =
  | { kind: 'state'; action: 'read' | 'unread' | 'star' | 'unstar'; ids: string[]; at: number }
  | { kind: 'markAll'; scope: { feed?: number; folder?: number; view?: string }; before: string; at: number }

interface Schema extends DBSchema {
  entries: { key: string; value: Entry & { savedAt: number }; indexes: { feedId: number; publishedAt: number } }
  meta: { key: string; value: unknown }
  outbox: { key: number; value: OutboxOp }
}

const NAME = 'correspondent'
let db: Promise<IDBPDatabase<Schema>> | null = null

export function open(): Promise<IDBPDatabase<Schema>> {
  db ??= openDB<Schema>(NAME, 1, {
    upgrade(d) {
      const entries = d.createObjectStore('entries', { keyPath: 'id' })
      entries.createIndex('feedId', 'feedId')
      entries.createIndex('publishedAt', 'publishedAt')
      d.createObjectStore('meta')
      d.createObjectStore('outbox', { autoIncrement: true })
    },
  })
  return db
}

/** Forget everything (sign-out). */
export async function clear(): Promise<void> {
  const d = await open()
  const tx = d.transaction(['entries', 'meta', 'outbox'], 'readwrite')
  await Promise.all([tx.objectStore('entries').clear(), tx.objectStore('meta').clear(), tx.objectStore('outbox').clear(), tx.done])
}

export async function saveTree(tree: Tree): Promise<void> {
  await (await open()).put('meta', tree, 'tree')
}

export async function loadTree(): Promise<Tree | undefined> {
  return (await (await open()).get('meta', 'tree')) as Tree | undefined
}

export async function setMeta(key: string, value: unknown): Promise<void> {
  await (await open()).put('meta', value, key)
}

export async function getMeta<T>(key: string): Promise<T | undefined> {
  return (await (await open()).get('meta', key)) as T | undefined
}

/**
 * Store entries. A summary never overwrites full text already saved, so
 * listing an article again doesn't lose what was downloaded for offline use.
 */
export async function saveEntries(entries: Entry[]): Promise<void> {
  if (!entries.length) return
  const d = await open()
  const tx = d.transaction('entries', 'readwrite')
  const now = Date.now()
  await Promise.all(
    entries.map(async (e) => {
      const prev = e.content === undefined ? await tx.store.get(e.id) : undefined
      await tx.store.put({ ...e, content: e.content ?? prev?.content, savedAt: now })
    }),
  )
  await tx.done
}

export async function getEntry(id: string): Promise<Entry | undefined> {
  return (await open()).get('entries', id)
}

export async function patchEntries(ids: string[], change: Partial<Entry>): Promise<void> {
  const d = await open()
  const tx = d.transaction('entries', 'readwrite')
  await Promise.all(
    ids.map(async (id) => {
      const e = await tx.store.get(id)
      if (e) await tx.store.put({ ...e, ...change })
    }),
  )
  await tx.done
}

export interface OfflineQuery {
  view?: 'unread' | 'starred' | 'today' | 'all'
  feed?: number
  folder?: number
  q?: string
}

/** The stored entries matching a list, newest published first. */
export async function queryEntries(query: OfflineQuery, feedsInFolder: (folder: number) => Set<number>): Promise<Entry[]> {
  const all = await (await open()).getAll('entries')
  const inFolder = query.folder ? feedsInFolder(query.folder) : null
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const needle = query.q?.trim().toLowerCase()
  return all
    .filter((e) => {
      if (query.feed && e.feedId !== query.feed) return false
      if (inFolder && !inFolder.has(e.feedId)) return false
      if (query.view === 'unread' && e.read) return false
      if (query.view === 'starred' && !e.starred) return false
      if (query.view === 'today' && e.publishedAt * 1000 < today.getTime()) return false
      if (needle && !`${e.title} ${e.excerpt} ${e.author}`.toLowerCase().includes(needle)) return false
      return true
    })
    .sort((a, b) => b.publishedAt - a.publishedAt || (BigInt(b.id) > BigInt(a.id) ? 1 : -1))
    .map(({ savedAt: _, ...e }) => e)
}

/** Drop stored read, unstarred articles beyond the newest `keep`. */
export async function prune(keep = 1500): Promise<void> {
  const d = await open()
  const all = await d.getAll('entries')
  const removable = all.filter((e) => e.read && !e.starred).sort((a, b) => b.savedAt - a.savedAt)
  const tx = d.transaction('entries', 'readwrite')
  await Promise.all(removable.slice(keep).map((e) => tx.store.delete(e.id)))
  await tx.done
}

export async function enqueue(op: OutboxOp): Promise<void> {
  await (await open()).add('outbox', op)
}

export async function pending(): Promise<{ key: number; op: OutboxOp }[]> {
  const d = await open()
  const keys = await d.getAllKeys('outbox')
  const ops = await d.getAll('outbox')
  return keys.map((key, i) => ({ key, op: ops[i] }))
}

export async function remove(keys: number[]): Promise<void> {
  const d = await open()
  const tx = d.transaction('outbox', 'readwrite')
  await Promise.all(keys.map((k) => tx.store.delete(k)))
  await tx.done
}
