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
// The newest dispatch is shaped like a real link post: an emoji, a long
// quotation, a wide picture, and a line of links.
items[0].body = `<p>📎 Linking to <a href="https://example.com/newsroom/2026/10/an-unusually-long-address-for-a-press-release/">example.com</a></p>
<p>Full text of dispatch 0, long enough to read offline.</p>
<blockquote><p>${'A quotation that runs on for a while, with nano-texture options and Dual 16-core engines. '.repeat(6)}</p></blockquote>
<p><img src="${FEEDS}/wide.svg" alt="A wide picture" width="1600" height="400"></p>
<p><a href="https://example.com/a">Read it at example.com →</a>  ·  <a href="https://example.com/b">Permalink</a></p>`
const wideSVG = '<svg xmlns="http://www.w3.org/2000/svg" width="1600" height="400"><rect width="1600" height="400" fill="#c84"/></svg>'
const rss = () => `<?xml version="1.0"?><rss version="2.0"><channel><title>Wire Service</title><link>${FEEDS}/</link>
${items.map((it) => `<item><title>${it.title}</title><link>${FEEDS}/${it.n}</link><guid>d-${it.n}</guid>
<pubDate>${it.date}</pubDate><description><![CDATA[${it.body}]]></description></item>`).join('\n')}
</channel></rss>`

// A site with no feed: a listing of headlines under /lab/, and the posts.
const labPage = `<html><head><title>Lab notes</title></head><body>${[1, 2, 3]
  .map((n) => `<div><a href="/lab/note-${n}"><h2>Lab note number ${n}, in full</h2></a><time datetime="2026-10-0${n}">x</time></div>`)
  .join('')}</body></html>`
const labArticle = (path) => `<html><head><title>${path}</title></head><body><article><h1>${path}</h1><p>${
  'The whole lab note, as published on its own page, with every detail the listing leaves out. '.repeat(5)}</p></article></body></html>`

const fixtures = createServer((req, res) => {
  if (req.url === '/feed.xml') res.writeHead(200, { 'Content-Type': 'application/rss+xml' }).end(rss())
  else if (req.url === '/wide.svg') res.writeHead(200, { 'Content-Type': 'image/svg+xml' }).end(wideSVG)
  else if (req.url === '/lab') res.writeHead(200, { 'Content-Type': 'text/html' }).end(labPage)
  else if (req.url.startsWith('/lab/')) res.writeHead(200, { 'Content-Type': 'text/html' }).end(labArticle(req.url))
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
  server = spawn(bin, ['serve', '-addr', '127.0.0.1:8097', '-data', data, '-insecure-cookies', '-allow-private-feeds', '-public-url', 'http://localhost:8097'], {
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
  await page.fill('input[autocomplete^=username]', USER)
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

  console.log('Motion')
  await page.goto(APP + '/#/unread')
  await page.reload()
  await page.locator('.wire .row .hit').nth(1).waitFor()
  await page.locator('.wire .row .hit').first().click()
  await page.locator('.wire .cursor').waitFor()
  const rowBox = (i) => page.evaluate((i) => {
    const days = document.querySelector('.wire .days').getBoundingClientRect()
    const r = document.querySelectorAll('.wire .row')[i].getBoundingClientRect()
    return { top: r.top - days.top, height: r.height }
  }, i)
  const cursorBox = () => page.evaluate(() => {
    const days = document.querySelector('.wire .days').getBoundingClientRect()
    const r = document.querySelector('.wire .cursor').getBoundingClientRect()
    return { top: r.top - days.top, height: r.height }
  })
  await page.locator('.wire .row .hit').first().focus()
  await page.keyboard.press('j')
  await page.waitForTimeout(120)
  const mid = await cursorBox()
  if (process.env.SHOT_DIR) await page.locator('.wire').screenshot({ path: process.env.SHOT_DIR + '/cursor-mid.png' })
  const [first, second] = [await rowBox(0), await rowBox(1)]
  check(mid.height > Math.max(first.height, second.height) + 4, `the selection stretches as it moves (${Math.round(mid.height)}px mid-move)`)
  await page.waitForTimeout(700)
  const settled = await cursorBox()
  if (process.env.SHOT_DIR) await page.locator('.wire').screenshot({ path: process.env.SHOT_DIR + '/cursor-settled.png' })
  check(Math.abs(settled.top - second.top) < 1 && Math.abs(settled.height - second.height) < 1, 'and settles on the next row')
  await page.keyboard.press('Escape')
  await page.locator('.wire .cursor').waitFor({ state: 'detached', timeout: 2_000 })
  check((await page.locator('.prose').count()) === 0, 'Esc closes the article')

  // A new article while the list is at the top slides in, without a prompt
  // (once the reader's last touch on the list is a moment ago).
  await page.waitForTimeout(1600)
  items.unshift({ n: 9, title: 'Dispatch 9', date: new Date().toUTCString(), body: '<p>Fresh off the wire.</p>' })
  await page.evaluate(async () => {
    const feed = (await (await fetch('/api/v1/tree')).json()).feeds.find((f) => f.title === 'Wire Service')
    await fetch(`/api/v1/feeds/${feed.id}/refresh`, { method: 'POST', headers: { 'X-Correspondent': '1' } })
  })
  await page.locator('.wire .row .title', { hasText: 'Dispatch 9' }).waitFor({ timeout: 15_000 })
  const firstTitle = await page.locator('.wire .row .title').first().innerText()
  check(firstTitle === 'Dispatch 9' && !(await page.locator('.pending').isVisible()), 'a new article slides in at the top of the list')

  console.log('Phone')
  const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 3 })
  const mobile = await phone.newPage()
  await mobile.goto(APP)
  await mobile.fill('input[autocomplete^=username]', USER)
  await mobile.fill('input[type=password]', PASSWORD)
  await mobile.click('button.primary')
  await mobile.waitForSelector('.wire .row', { state: 'attached' })
  const tabbar = mobile.locator('nav.tabbar')
  check(await tabbar.isVisible(), 'the tab bar shows on a phone')
  // Scroll the list hard, past its end, as a finger would.
  await mobile.locator('.wire-scroll').hover()
  for (let i = 0; i < 6; i++) await mobile.mouse.wheel(0, 2000)
  await mobile.waitForTimeout(300)
  const bars = () => mobile.evaluate(() => ({
    pageScroll: document.scrollingElement.scrollTop + window.scrollY,
    headTucked: document.querySelector('.wire .head').classList.contains('tucked'),
    headTop: document.querySelector('.wire .head').getBoundingClientRect().top,
    barBottom: document.querySelector('nav.tabbar').getBoundingClientRect().bottom,
    height: window.innerHeight,
  }))
  const layout = await bars()
  check(layout.pageScroll === 0, 'scrolling the list never moves the page')
  // A shorter screen makes the same list long enough to scroll past them.
  await mobile.mouse.wheel(0, -4000)
  await mobile.setViewportSize({ width: 390, height: 480 })
  await mobile.waitForTimeout(300)
  await mobile.locator('.wire-scroll').hover()
  for (let i = 0; i < 6; i++) await mobile.mouse.wheel(0, 2000)
  await mobile.waitForTimeout(400)
  const tucked = await bars()
  check(tucked.headTucked && tucked.barBottom > tucked.height + 20, 'scrolling down a long list tucks the header and the tab bar away')
  check(!layout.headTucked && layout.headTop >= 0, 'a list too short to scroll past the bars keeps them')
  await mobile.mouse.wheel(0, -200)
  await mobile.waitForTimeout(400)
  const back = await bars()
  check(!back.headTucked && back.headTop >= 0 && Math.abs(back.barBottom - back.height) <= 1, 'scrolling up brings them back, the tab bar at the bottom edge')
  await mobile.setViewportSize({ width: 390, height: 844 })
  await mobile.waitForTimeout(300)
  await tabbar.getByRole('button', { name: 'Feeds' }).tap()
  check(await mobile.locator('nav.sidebar').waitFor({ timeout: 5000 }).then(() => true, () => false), 'Feeds opens the feed list')
  await mobile.locator('nav.sidebar').getByRole('button', { name: /Starred/ }).first().tap()
  check(await mobile.locator('.wire h1').innerText() === 'Starred', 'choosing a view from Feeds opens it')
  await tabbar.getByRole('button', { name: 'Settings' }).tap()
  const settings = mobile.locator('section.settings')
  await settings.waitFor({ timeout: 5000 }).catch(() => {})
  check(await settings.isVisible() && (await mobile.evaluate(() => location.hash)) === '#/settings', 'Settings opens from the tab bar, as a page')
  // Interface text: size and typeface apply at once, everywhere, previews included.
  await settings.getByRole('button', { name: 'Larger' }).tap()
  await settings.getByRole('button', { name: 'System', exact: true }).tap()
  const text = await mobile.evaluate(() => ({
    size: getComputedStyle(document.querySelector('.settings h1')).fontSize,
    preview: getComputedStyle(document.querySelector('.settings .pv-title')).fontSize,
    font: getComputedStyle(document.body).fontFamily,
    icons: [...document.querySelectorAll('.icon-choice img')].every((i) => i.complete && i.naturalWidth > 0),
  }))
  check(Math.abs(parseFloat(text.size) - 23 * 1.2) < 0.01 && Math.abs(parseFloat(text.preview) - 16.5 * 1.2) < 0.01 && text.font.startsWith('system-ui'),
    `interface text size and typeface change from Settings (${text.size}, ${text.font.split(',')[0]})`)
  check(text.icons, 'every app icon choice shows in Settings')
  // The section buttons move through the page and follow it.
  await settings.locator('.chips').getByRole('button', { name: 'Apps & sync' }).tap()
  await mobile.waitForTimeout(900)
  check(await settings.locator('.chips [aria-current="true"]').innerText() === 'Apps & sync' && await settings.getByText('Reeder and other apps').isVisible(),
    'the section buttons jump to a section and follow the page')
  if (process.env.SHOT_DIR) await mobile.screenshot({ path: process.env.SHOT_DIR + '/settings-larger.png' })
  await settings.locator('.chips').getByRole('button', { name: 'Appearance' }).tap()
  await settings.getByRole('button', { name: 'Default' }).tap()
  await settings.getByRole('button', { name: 'Grotesk' }).first().tap()
  // Dispatch 0, the link post, is the one starred earlier.
  await tabbar.getByRole('button', { name: 'Starred' }).tap()
  await mobile.locator('.wire .row .hit', { hasText: 'Dispatch 0' }).tap()
  await mobile.locator('article.pane .bar').waitFor()
  check(!(await tabbar.isVisible()), 'the tab bar steps aside while reading')
  // Nothing in the article may be wider than the screen, and dragging
  // sideways or past the end must not move the page or its toolbar.
  await mobile.locator('article.pane .scroll').hover()
  for (let i = 0; i < 4; i++) await mobile.mouse.wheel(600, 2000)
  await mobile.waitForTimeout(300)
  const reading = await mobile.evaluate(() => {
    const w = window.innerWidth
    const wide = [...document.querySelectorAll('article.pane *')]
      .filter((el) => el.getClientRects().length && !el.closest('pre, table'))
      .filter((el) => { const r = el.getBoundingClientRect(); return r.right > w + 0.5 || r.left < -0.5 })
      .map((el) => el.tagName + '.' + el.className)
    const scroll = document.querySelector('article.pane .scroll')
    return {
      wide,
      pageX: document.scrollingElement.scrollLeft + window.scrollX,
      pageY: document.scrollingElement.scrollTop + window.scrollY,
      paneX: scroll.scrollLeft,
      barTucked: document.querySelector('article.pane .bar').classList.contains('tucked'),
    }
  })
  const picture = mobile.locator('article.pane .prose img')
  await picture.evaluate((img) => img.complete || new Promise((r) => (img.onload = img.onerror = r)))
  const pic = await picture.evaluate((img) => ({ src: img.getAttribute('src'), loaded: img.naturalWidth > 0 }))
  check(pic.src.startsWith('/img/') && pic.loaded, 'article pictures load through the image proxy')
  check(reading.wide.length === 0, `the article fits the screen${reading.wide.length ? ': ' + reading.wide.slice(0, 5).join(', ') : ''}`)
  check(reading.pageX === 0 && reading.pageY === 0 && reading.paneX === 0, 'dragging the article never moves the page')
  check(reading.barTucked, 'reading on tucks the toolbar away')
  await mobile.mouse.wheel(0, -200)
  await mobile.waitForTimeout(400)
  check(await mobile.locator('article.pane .bar').evaluate((b) => !b.classList.contains('tucked') && b.getBoundingClientRect().top === 0), 'scrolling up brings the toolbar back')
  await mobile.getByRole('button', { name: 'Back to articles' }).tap()
  check(await mobile.locator('nav.tabbar').isVisible(), 'and comes back with the list')
  // A summary-only article offers full articles for the whole feed. (By
  // name: the list may still be taking in the newest arrival.)
  await tabbar.getByRole('button', { name: 'Unread' }).tap()
  await mobile.locator('.wire .row .hit', { hasText: 'Dispatch 9' }).tap()
  const always = mobile.getByRole('button', { name: /Always load full articles from Wire Service/ })
  await always.waitFor()
  if (process.env.SHOT) await mobile.screenshot({ path: process.env.SHOT })
  check(await always.isVisible(), 'a summary offers full articles for its whole feed')
  await always.tap()
  await mobile.getByText(/Fetching full articles for Wire Service/).waitFor()
  const fullOn = await mobile.evaluate(async () => (await (await fetch('/api/v1/tree')).json()).feeds[0].fullText)
  check(fullOn && !(await always.isVisible()), 'which turns it on for the feed')
  await mobile.getByRole('button', { name: 'Back to articles' }).tap()
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

  console.log('Sites without feeds')
  await page.keyboard.press('Escape')
  await page.goto(APP + '/#/manage')
  await page.reload()
  await page.getByRole('button', { name: 'Add feed' }).first().click()
  const dialog = page.getByRole('dialog', { name: 'Add feed' })
  await dialog.locator('#feed-url').fill(FEEDS + '/lab')
  await dialog.getByRole('button', { name: 'Find feeds' }).click()
  await dialog.getByText(/No feed: read from the page/).waitFor({ timeout: 15_000 })
  check(await dialog.getByText('Lab note number 2, in full').isVisible(), 'a site without a feed is read from its page, with sample titles')
  await dialog.getByRole('button', { name: 'Subscribe' }).click()
  await dialog.waitFor({ state: 'detached', timeout: 15_000 })
  const labFeed = await page.evaluate(async () => (await (await fetch('/api/v1/tree')).json()).feeds.find((f) => f.page))
  check(labFeed && labFeed.total === 3 && labFeed.fullText, 'subscribing follows the page, with full articles')
  await page.goto(APP + '/#/manage')
  await page.reload()
  const badge = await page.locator('.manage .row .kind').first().waitFor({ timeout: 10_000 }).then(() => true, () => false)
  check(badge, 'Subscriptions marks it as a page')
  const full = await page.evaluate(async () => (await (await fetch('/api/v1/entries?view=all&content=1')).json()).entries.filter((e) => e.fullText).length)
  check(full === 3, `its articles arrive in full (${full} of 3)`)

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

  console.log('Several readers')
  // Passkeys need a domain, not an IP: this part runs on localhost.
  const HOST = 'http://localhost:8097'
  const adminCtx = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  const admin = await adminCtx.newPage()
  await admin.goto(HOST)
  await admin.fill('input[autocomplete="username webauthn"]', USER)
  await admin.fill('input[type=password]', PASSWORD)
  await admin.click('button.primary')
  await admin.locator('.shell').waitFor()
  await admin.goto(HOST + '/#/admin')
  await admin.reload()
  const adminPage = admin.locator('section.admin')
  await adminPage.waitFor()
  const adminRow = adminPage.locator('.account', { hasText: USER })
  const sawAdmin = await adminRow.waitFor({ timeout: 10_000 }).then(() => adminRow.getByText('Admin').isVisible(), () => false)
  check(sawAdmin, 'the first account is the admin and sees the admin page')
  await adminPage.getByPlaceholder(/Who is it for/).fill('Carol')
  await adminPage.getByRole('button', { name: 'Create invite link' }).click()
  const link = await adminPage.locator('.fresh code').innerText()
  check(link.startsWith(HOST + '/#/invite/'), 'an invitation link is made, shown once')

  const carolCtx = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  const carol = await carolCtx.newPage()
  // A virtual authenticator, as on a phone with Face ID.
  const authn = await carolCtx.newCDPSession(carol)
  await authn.send('WebAuthn.enable')
  await authn.send('WebAuthn.addVirtualAuthenticator', {
    options: { protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true },
  })
  await carol.goto(link)
  await carol.getByText(`${USER} invited you.`, { exact: false }).waitFor()
  await carol.getByLabel('Username').fill('carol')
  await carol.getByLabel('Password', { exact: true }).fill('carol-password-1')
  await carol.getByLabel('Password again').fill('carol-password-1')
  await carol.getByRole('button', { name: 'Create account' }).click()
  await carol.locator('.shell').waitFor({ timeout: 15_000 })
  const carolMe = await carol.evaluate(async () => (await (await fetch('/api/v1/me')).json()))
  check(carolMe.username === 'carol' && carolMe.role === 'user', 'opening the link creates the account and signs in, as a reader')
  await carol.goto(HOST + '/#/admin')
  await carol.reload()
  check(!(await carol.locator('section.admin').isVisible()), 'readers never see the admin page')
  const adminAPI = await carol.evaluate(async () => (await fetch('/api/v1/admin/accounts')).status)
  check(adminAPI === 403, 'and the server refuses them its admin API')

  // A passkey, then signing in with it.
  await carol.goto(HOST + '/#/settings')
  await carol.reload()
  await carol.getByRole('button', { name: 'Add a passkey on this device' }).click()
  await carol.locator('.settings .list li', { hasText: 'Added' }).first().waitFor({ timeout: 10_000 })
  check(await carol.locator('.settings .list li', { hasText: 'not used yet' }).count() === 1, 'a passkey can be added from Settings')
  await carol.getByRole('button', { name: 'Sign out' }).last().click()
  await carol.locator('main.login').waitFor({ timeout: 10_000 })
  const button = carol.getByRole('button', { name: 'Sign in with a passkey' })
  // The browser may already have signed in through passkey autofill.
  await Promise.race([button.click().catch(() => {}), carol.locator('.shell').waitFor({ timeout: 10_000 })])
  await carol.locator('.shell').waitFor({ timeout: 10_000 })
  const viaPasskey = await carol.evaluate(async () => (await (await fetch('/api/v1/me')).json()).username)
  check(viaPasskey === 'carol', 'and signs in with it, no password')

  // Disabling signs the reader out at once.
  await admin.reload()
  await adminPage.locator('.account', { hasText: 'carol' }).getByRole('button', { name: 'Disable' }).click()
  await adminPage.locator('.account', { hasText: 'carol' }).getByText('Disabled').waitFor()
  const afterDisable = await carol.evaluate(async () => (await fetch('/api/v1/me')).status)
  check(afterDisable === 401, 'disabling an account ends its sessions')
  await carolCtx.close()
  await adminCtx.close()

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
