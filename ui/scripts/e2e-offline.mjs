// End-to-end check of the PWA's offline mode in a real browser against a
// real Correspondent: the app opens without a network, unread articles read
// from the offline copy, and a change made offline reaches the server when
// the connection returns. Run with `make e2e` (needs bin/ built).

import { execFileSync, spawn } from 'node:child_process'
import { createServer } from 'node:http'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { chromium } from 'playwright-core'

const root = resolve(import.meta.dirname, '../..')
const bin = join(root, 'bin/correspondent')
const APP = 'http://127.0.0.1:8097'
const FEEDS = 'http://127.0.0.1:8096'
const USER = 'reader'
const PASSWORD = 'e2e-password-123'

const hour = 3_600_000
const items = Array.from({ length: 6 }, (_, i) => ({
  n: i,
  title: `Dispatch ${i}`,
  date: new Date(Date.now() - (i + 1) * hour).toUTCString(),
  body: `<p>Full text of dispatch ${i}, long enough to read offline.</p>`,
}))
const rss = `<?xml version="1.0"?><rss version="2.0"><channel><title>Wire Service</title><link>${FEEDS}/</link>
${items.map((it) => `<item><title>${it.title}</title><link>${FEEDS}/${it.n}</link><guid>d-${it.n}</guid>
<pubDate>${it.date}</pubDate><description><![CDATA[${it.body}]]></description></item>`).join('\n')}
</channel></rss>`

const fixtures = createServer((req, res) => {
  if (req.url === '/feed.xml') res.writeHead(200, { 'Content-Type': 'application/rss+xml' }).end(rss)
  else res.writeHead(404).end()
})

let failures = 0
const check = (ok, what) => {
  console.log(`${ok ? '  ✓' : '  ✗'} ${what}`)
  if (!ok) failures++
}

const data = mkdtempSync(join(tmpdir(), 'correspondent-e2e-'))
let server
try {
  await new Promise((r) => fixtures.listen(8096, '127.0.0.1', r))
  execFileSync(bin, ['user', 'add', USER, '-data', data], { input: PASSWORD + '\n', stdio: ['pipe', 'ignore', 'inherit'] })
  const opml = join(data, 'feeds.opml')
  writeFileSync(opml, `<opml version="2.0"><body><outline type="rss" text="Wire Service" xmlUrl="${FEEDS}/feed.xml"/></body></opml>`)
  execFileSync(bin, ['import-freshrss', USER, '-opml', opml, '-data', data], { stdio: 'ignore' })
  server = spawn(bin, ['serve', '-addr', '127.0.0.1:8097', '-data', data, '-insecure-cookies', '-allow-private-feeds'], {
    stdio: ['ignore', 'ignore', 'inherit'],
  })

  const browser = await chromium.launch({ channel: 'chrome' })
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  await context.grantPermissions(['notifications'], { origin: APP })
  const page = await context.newPage()
  for (let i = 0; ; i++) {
    try {
      await page.goto(APP)
      break
    } catch {
      if (i > 50) throw new Error('server did not start')
      await new Promise((r) => setTimeout(r, 200))
    }
  }
  await page.fill('input[autocomplete=username]', USER)
  await page.fill('input[type=password]', PASSWORD)
  await page.click('button.primary')
  await page.waitForSelector('.wire .row', { state: 'attached', timeout: 30_000 })

  console.log('Online')
  await page.waitForFunction(() => navigator.serviceWorker.ready.then(() => true), null, { timeout: 15_000 })
  // The first visit installs the worker; a reload puts the page under its control.
  await page.reload()
  await page.waitForFunction(() => !!navigator.serviceWorker.controller, null, { timeout: 15_000 })
  check(true, 'service worker installed and controlling the page')
  // Wait for the offline copy: every unread article saved with its text.
  // (Polled from Node: waitForFunction does not await an async predicate.)
  const savedWithText = () => page.evaluate(async () => {
    const db = await new Promise((ok, no) => {
      const r = indexedDB.open('correspondent')
      r.onsuccess = () => ok(r.result)
      r.onerror = () => no(r.error)
    })
    const all = await new Promise((ok) => {
      const q = db.transaction('entries').objectStore('entries').getAll()
      q.onsuccess = () => ok(q.result)
    })
    return all.filter((e) => !e.read && e.content).length
  })
  for (let i = 0; (await savedWithText()) < 6; i++) {
    if (i > 60) throw new Error('offline copy was not saved')
    await page.waitForTimeout(500)
  }
  check(true, 'all 6 unread articles saved with full text')
  await page.waitForTimeout(500)
  check(!(await page.getByText('Offline', { exact: true }).isVisible()), 'no Offline indicator while online')

  console.log('Offline')
  await context.setOffline(true)
  await page.reload()
  await page.waitForSelector('.wire .row', { state: 'attached', timeout: 15_000 })
  check(true, 'the app opens with no network (from the service worker cache)')
  check(await page.getByText('Offline', { exact: true }).isVisible(), 'the Offline indicator shows')
  check((await page.locator('.wire .row').count()) === 6, 'the Unread list comes from the offline copy')

  await page.locator('.wire .row .hit').first().click()
  const body = page.locator('.prose')
  await body.waitFor({ timeout: 10_000 })
  check((await body.textContent()).includes('Full text of dispatch 0'), 'an unread article opens with its full text')

  await page.keyboard.press('s')
  await page.getByText('1 change waiting').waitFor({ timeout: 5_000 })
  check(true, 'starring offline queues the change ("1 change waiting")')

  console.log('Back online')
  await context.setOffline(false)
  await page.getByText('1 change waiting').waitFor({ state: 'detached', timeout: 30_000 })
  check(true, 'the queued change is sent when the connection returns')
  const starred = await page.evaluate(async () => (await (await fetch('/api/v1/entries?view=starred')).json()).entries.map((e) => e.title))
  check(starred.length === 1 && starred[0] === 'Dispatch 0', 'the server has the star made offline')

  console.log('Phone')
  const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 3 })
  const mobile = await phone.newPage()
  await mobile.goto(APP)
  await mobile.fill('input[autocomplete=username]', USER)
  await mobile.fill('input[type=password]', PASSWORD)
  await mobile.click('button.primary')
  await mobile.waitForSelector('.wire .row', { state: 'attached' })
  const tabbar = mobile.locator('nav.tabbar')
  check(await tabbar.isVisible(), 'the tab bar shows on a phone')
  // Scroll the list hard, past its end, as a finger would.
  await mobile.locator('.wire-scroll').hover()
  for (let i = 0; i < 6; i++) await mobile.mouse.wheel(0, 2000)
  await mobile.waitForTimeout(300)
  const layout = await mobile.evaluate(() => ({
    pageScroll: document.scrollingElement.scrollTop + window.scrollY,
    headTop: document.querySelector('.wire .head').getBoundingClientRect().top,
    barBottom: document.querySelector('nav.tabbar').getBoundingClientRect().bottom,
    height: window.innerHeight,
  }))
  check(layout.pageScroll === 0 && layout.headTop >= 0, 'scrolling the list never moves the page or its header')
  check(Math.abs(layout.barBottom - layout.height) <= 1, 'the tab bar stays at the bottom edge')
  await tabbar.getByRole('button', { name: 'Feeds' }).tap()
  check(await mobile.locator('nav.sidebar').isVisible(), 'Feeds opens the feed list')
  await mobile.locator('nav.sidebar').getByRole('button', { name: /Starred/ }).first().tap()
  check(await mobile.locator('.wire h1').innerText() === 'Starred', 'choosing a view from Feeds opens it')
  await tabbar.getByRole('button', { name: 'Settings' }).tap()
  check(await mobile.getByRole('dialog', { name: 'Settings' }).isVisible(), 'Settings opens from the tab bar')
  await mobile.keyboard.press('Escape')
  await tabbar.getByRole('button', { name: 'Unread' }).tap()
  await mobile.locator('.wire .row .hit').first().tap()
  await mobile.locator('article.pane .bar').waitFor()
  check(!(await tabbar.isVisible()), 'the tab bar steps aside while reading')
  await mobile.getByRole('button', { name: 'Back to articles' }).tap()
  check(await mobile.locator('nav.tabbar').isVisible(), 'and comes back with the list')
  await phone.close()

  console.log('Subscriptions')
  await page.goto(APP + '/#/manage')
  await page.reload()
  const row = page.locator('.manage .row').first()
  await row.waitFor({ timeout: 10_000 })
  await row.locator('button.name').click()
  const rename = row.locator('input.rename')
  await rename.fill('Renamed Wire')
  await rename.press('Enter')
  await page.waitForTimeout(500)
  const names = await page.evaluate(async () => (await (await fetch('/api/v1/tree')).json()).feeds.map((f) => f.title))
  check(names.includes('Renamed Wire'), 'renaming a feed in Subscriptions saves it')
  await page.getByRole('button', { name: 'Delete Renamed Wire' }).click()
  await page.getByRole('button', { name: 'Delete feed' }).click()
  await page.getByText('Deleted Renamed Wire.').waitFor({ timeout: 5_000 })
  const after = await page.evaluate(async () => (await (await fetch('/api/v1/tree')).json()).feeds.length)
  check(after === 0, 'deleting a feed after confirming removes it')

  console.log('Notifications')
  // Deliver a push message to the service worker through DevTools, as a push
  // service would, and check the notification it shows.
  const cdp = await context.newCDPSession(page)
  const registrations = new Promise((ok) => cdp.on('ServiceWorker.workerRegistrationUpdated', (e) => e.registrations.length && ok(e.registrations)))
  await cdp.send('ServiceWorker.enable')
  const [reg] = await registrations
  await cdp.send('ServiceWorker.deliverPushMessage', {
    origin: APP,
    registrationId: reg.registrationId,
    data: JSON.stringify({ title: 'Wire Service', body: 'Dispatch 9', url: 'https://evil.example/', tag: 'feed-1' }),
  })
  let shown = []
  for (let i = 0; i < 20 && !shown.length; i++) {
    await page.waitForTimeout(250)
    shown = await page.evaluate(async () =>
      (await (await navigator.serviceWorker.ready).getNotifications()).map((n) => ({ title: n.title, body: n.body, tag: n.tag, url: n.data?.url })),
    )
  }
  check(shown.length === 1 && shown[0].title === 'Wire Service' && shown[0].body === 'Dispatch 9', 'a push message shows a notification')
  check(shown[0]?.tag === 'feed-1', 'notifications are tagged per feed, so newer ones replace older')
  check(shown[0]?.url === '/', 'a notification can only open an in-app address')

  await browser.close()
} finally {
  server?.kill()
  fixtures.close()
  rmSync(data, { recursive: true, force: true })
}

if (failures) {
  console.error(`\n${failures} check(s) failed`)
  process.exit(1)
}
console.log('\nOffline end-to-end test passed.')
