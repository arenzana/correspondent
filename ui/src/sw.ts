/// <reference lib="webworker" />
// Correspondent's service worker: makes the reader installable, opens it
// without a network, and sends changes queued offline (Background Sync).
//
// Caching rules (see route()):
//   - the API and the Reader API are never cached here; the app keeps its
//     own offline copy of data in IndexedDB
//   - page loads: network first, falling back to the cached app shell
//   - hashed assets, icons and fonts: cache first (they never change)
//   - anything else from this origin: network, refreshing the cache

import { flush } from './lib/offline/outbox'

declare const self: ServiceWorkerGlobalScope
declare const __BUILD_ID__: string

const CACHE = `correspondent-${__BUILD_ID__}`
const SHELL = '/'

export type Strategy = 'network-only' | 'shell' | 'cache-first' | 'network-then-cache'

/** Decides how a request is handled. Pure, so it can be tested. */
export function route(url: URL, method: string, mode: RequestMode, origin: string): Strategy {
  if (method !== 'GET' || url.origin !== origin) return 'network-only'
  if (url.pathname.startsWith('/api/') || url.pathname === '/sw.js' || url.pathname === '/manifest.webmanifest') {
    return 'network-only'
  }
  if (mode === 'navigate') return 'shell'
  if (url.pathname.startsWith('/assets/') || url.pathname.startsWith('/icons/')) return 'cache-first'
  return 'network-then-cache'
}

/** The asset URLs an app shell references, so they can be cached on install. */
export function shellAssets(html: string): string[] {
  const urls = new Set<string>()
  for (const m of html.matchAll(/(?:src|href)="(\/(?:assets|icons)\/[^"]+)"/g)) urls.add(m[1])
  return [...urls]
}

if (typeof self !== 'undefined' && 'registration' in self) {
  self.addEventListener('install', (event) => {
    event.waitUntil(
      (async () => {
        const cache = await caches.open(CACHE)
        const res = await fetch(SHELL, { cache: 'no-cache' })
        if (res.ok) {
          const html = await res.clone().text()
          await cache.put(SHELL, res)
          await cache.addAll(shellAssets(html))
        }
      })(),
    )
    // Wait for the app to ask before taking over, so an open reader is
    // never switched to new code mid-session.
  })

  self.addEventListener('activate', (event) => {
    event.waitUntil(
      (async () => {
        for (const key of await caches.keys()) {
          if (key.startsWith('correspondent-') && key !== CACHE) await caches.delete(key)
        }
        await self.clients.claim()
      })(),
    )
  })

  self.addEventListener('message', (event) => {
    if (event.data === 'skip-waiting') self.skipWaiting()
  })

  self.addEventListener('fetch', (event) => {
    const req = event.request
    const strategy = route(new URL(req.url), req.method, req.mode, self.location.origin)
    if (strategy === 'network-only') return
    event.respondWith(handle(req, strategy))
  })

  // Chrome and Android: send queued changes even if the app was closed
  // before the connection came back.
  self.addEventListener('sync', (event) => {
    const e = event as ExtendableEvent & { tag: string }
    if (e.tag === 'outbox') {
      e.waitUntil(
        flush().then((r) => {
          if (r.stopped === 'offline' || r.stopped === 'server') throw new Error('retry later')
        }),
      )
    }
  })
}

async function handle(req: Request, strategy: Strategy): Promise<Response> {
  const cache = await caches.open(CACHE)
  if (strategy === 'shell') {
    try {
      const res = await fetch(req)
      if (res.ok) await cache.put(SHELL, res.clone())
      return res
    } catch {
      return (await cache.match(SHELL)) ?? new Response('Offline', { status: 503, headers: { 'Content-Type': 'text/plain' } })
    }
  }
  if (strategy === 'cache-first') {
    const hit = await cache.match(req)
    if (hit) return hit
    const res = await fetch(req)
    if (res.ok) await cache.put(req, res.clone())
    return res
  }
  try {
    const res = await fetch(req)
    if (res.ok) await cache.put(req, res.clone())
    return res
  } catch {
    return (await cache.match(req)) ?? Response.error()
  }
}
