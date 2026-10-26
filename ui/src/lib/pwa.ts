// Registers the service worker and offers updates. A new version never takes
// over an open reader by itself: the user is asked, then the page reloads.

import { app } from './state.svelte'

export function registerServiceWorker() {
  if (import.meta.env.DEV || !('serviceWorker' in navigator)) return
  // A tapped notification asks the open reader to show its article.
  navigator.serviceWorker.addEventListener('message', (e) => {
    if (e.data?.type === 'open' && typeof e.data.url === 'string' && e.data.url.startsWith('/#/')) {
      location.hash = e.data.url.slice(1)
      void app.onPopState()
    }
  })
  navigator.serviceWorker
    .register('/sw.js')
    .then((reg) => {
      const offer = (worker: ServiceWorker) => {
        app.toast('A new version of Correspondent is ready.', {
          label: 'Reload',
          run: () => {
            navigator.serviceWorker.addEventListener('controllerchange', () => location.reload(), { once: true })
            worker.postMessage('skip-waiting')
          },
        })
      }
      // An update downloaded in an earlier visit, still waiting.
      if (reg.waiting && navigator.serviceWorker.controller) offer(reg.waiting)
      reg.addEventListener('updatefound', () => {
        const worker = reg.installing
        worker?.addEventListener('statechange', () => {
          // "installed" with an existing controller means an update, not the first install.
          if (worker.state === 'installed' && navigator.serviceWorker.controller) offer(worker)
        })
      })
      // Check for updates when the app comes back to the foreground.
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') reg.update().catch(() => {})
      })
    })
    .catch(() => {
      /* no service worker: the reader still works online */
    })
}
