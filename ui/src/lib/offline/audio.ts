// Audio kept for listening offline, in its own cache (Cache API: made for
// large files). The player prefers a saved copy, so a saved article plays
// with no network and starts at once.

const CACHE = 'audio-v1'

/** How many articles' audio are kept, at most. */
export const AUDIO_LIMIT = 25

const available = () => typeof caches !== 'undefined'

export const audioPath = (id: string) => `/api/v1/entries/${id}/audio`

/** A playable address for saved audio, or null. Revoke it with URL.revokeObjectURL. */
export async function savedURL(id: string): Promise<string | null> {
  if (!available()) return null
  const hit = await (await caches.open(CACHE)).match(audioPath(id))
  return hit ? URL.createObjectURL(await hit.blob()) : null
}

/** The IDs whose audio is saved. */
export async function saved(): Promise<Set<string>> {
  if (!available()) return new Set()
  const keys = await (await caches.open(CACHE)).keys()
  return new Set(keys.map((k) => new URL(k.url).pathname.split('/')[4]))
}

/** Downloads an article's audio, unless it's already saved. */
export async function save(id: string): Promise<void> {
  if (!available()) return
  const cache = await caches.open(CACHE)
  if (await cache.match(audioPath(id))) return
  const res = await fetch(audioPath(id), { credentials: 'same-origin' })
  if (res.ok && res.headers.get('Content-Type')?.startsWith('audio/')) await cache.put(audioPath(id), res)
}

/** Keeps only the given IDs' audio; returns how many were dropped. */
export async function keepOnly(ids: Set<string>): Promise<number> {
  if (!available()) return 0
  const cache = await caches.open(CACHE)
  let dropped = 0
  for (const k of await cache.keys()) {
    if (!ids.has(new URL(k.url).pathname.split('/')[4])) {
      await cache.delete(k)
      dropped++
    }
  }
  return dropped
}

/** Forget all saved audio (sign-out, or the setting turned off). */
export async function clearAudio(): Promise<void> {
  if (available()) await caches.delete(CACHE)
}
