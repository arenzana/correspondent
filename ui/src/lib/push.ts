// Web Push on this device: whether it's possible here, and turning it on or
// off. Which feeds notify is a separate, per-feed choice (Feed.notify).

import { api } from './api'

export type PushSupport = 'supported' | 'needs-install' | 'unsupported'

/** iPhone and iPad only allow push for apps added to the Home Screen. */
export function pushSupport(): PushSupport {
  const ios = /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
  const standalone = matchMedia('(display-mode: standalone)').matches || (navigator as Navigator & { standalone?: boolean }).standalone === true
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
    return ios && !standalone ? 'needs-install' : 'unsupported'
  }
  return 'supported'
}

/** The VAPID key, as PushManager.subscribe wants it. */
export function keyBytes(base64url: string): Uint8Array<ArrayBuffer> {
  const pad = '='.repeat((4 - (base64url.length % 4)) % 4)
  const raw = atob((base64url + pad).replace(/-/g, '+').replace(/_/g, '/'))
  const out = new Uint8Array(new ArrayBuffer(raw.length))
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i)
  return out
}

export async function deviceSubscribed(): Promise<boolean> {
  if (pushSupport() !== 'supported') return false
  const reg = await navigator.serviceWorker.ready
  const sub = await reg.pushManager.getSubscription()
  if (!sub) return false
  const { subscribed } = await api.pushStatus(sub.endpoint)
  return subscribed
}

/** Ask permission and register this device. Throws with a readable message. */
export async function subscribeDevice(): Promise<void> {
  if (pushSupport() === 'needs-install') {
    throw new Error('On iPhone and iPad, add Correspondent to your Home Screen first, then turn notifications on there.')
  }
  if (pushSupport() !== 'supported') throw new Error("This browser doesn't support notifications.")
  const permission = await Notification.requestPermission()
  if (permission !== 'granted') throw new Error('Notifications are blocked for this site. Allow them in your browser settings.')
  const { publicKey } = await api.pushStatus()
  const reg = await navigator.serviceWorker.ready
  let sub: PushSubscription
  try {
    sub =
      (await reg.pushManager.getSubscription()) ??
      (await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(publicKey) }))
  } catch (e) {
    // Brave ships with web push turned off and says only "push service error".
    if (await isBrave()) throw new Error(BRAVE_PUSH)
    throw e
  }
  await api.pushSubscribe(sub.toJSON())
}

export async function unsubscribeDevice(): Promise<void> {
  const reg = await navigator.serviceWorker.ready
  const sub = await reg.pushManager.getSubscription()
  if (!sub) return
  await api.pushUnsubscribe(sub.endpoint).catch(() => {})
  await sub.unsubscribe()
}

export const BRAVE_PUSH =
  'Brave turns notifications from websites off by default. In Brave, open Settings → Privacy and security, turn on ' +
  '“Use Google services for push messaging”, restart Brave, then turn notifications on here again.'

/** Whether this is Brave, which hides itself as Chrome but offers navigator.brave. */
export async function isBrave(): Promise<boolean> {
  const b = (navigator as Navigator & { brave?: { isBrave?: () => Promise<boolean> } }).brave
  try {
    return (await b?.isBrave?.()) === true
  } catch {
    return false
  }
}

/** Only in-app paths may be opened from a notification. */
export function safeTarget(url: unknown): string {
  return typeof url === 'string' && url.startsWith('/') && !url.startsWith('//') ? url : '/'
}
