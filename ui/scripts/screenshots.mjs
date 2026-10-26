// Regenerates the README screenshots from invented content, so no real
// subscriptions or third-party articles ever land in the repository.
//
// It serves fixture feeds on a local port, starts a throwaway Correspondent
// against a temporary data directory, signs in, and captures each screen with
// the locally installed Chrome. Run with `make screenshots`.

import { spawn, execFileSync } from 'node:child_process'
import { createServer } from 'node:http'
import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { chromium } from 'playwright-core'

const root = resolve(import.meta.dirname, '../..')
const out = join(root, 'docs/screenshots')
const bin = join(root, 'bin/correspondent')
const APP = 'http://127.0.0.1:8099'
const FEEDS = 'http://127.0.0.1:8098'
const USER = 'reader'
const PASSWORD = 'screenshot-password'

// ---------------------------------------------------------------- fixtures

const now = Date.now()
const ago = (h) => new Date(now - h * 3_600_000).toUTCString()

/** Abstract artwork for thumbnails: deterministic shapes in a given palette. */
function art(seed, w = 1200, h = 800) {
  const palettes = [
    ['#d7dde8', '#2742c7', '#18213a', '#b7791f'],
    ['#e9e2d0', '#2f4a8a', '#9a6516', '#2e2416'],
    ['#dfe7e4', '#2b6a5e', '#18213a', '#c7a046'],
    ['#ece4e8', '#7a3b5e', '#2742c7', '#18213a'],
    ['#e4e9f0', '#4a5f8f', '#d08a3c', '#1d2433'],
  ]
  const [bg, a, b, c] = palettes[seed % palettes.length]
  let s = seed * 9301 + 49297
  const r = () => ((s = (s * 9301 + 49297) % 233280) / 233280)
  let shapes = ''
  for (let i = 0; i < 7; i++) {
    const col = [a, b, c][i % 3]
    if (i % 2) shapes += `<circle cx="${r() * w}" cy="${r() * h}" r="${60 + r() * 240}" fill="${col}" opacity="${0.55 + r() * 0.4}"/>`
    else shapes += `<rect x="${r() * w}" y="${r() * h}" width="${80 + r() * 420}" height="${30 + r() * 260}" fill="${col}" opacity="${0.6 + r() * 0.35}" transform="rotate(${r() * 40 - 20} ${w / 2} ${h / 2})"/>`
  }
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}"><rect width="${w}" height="${h}" fill="${bg}"/>${shapes}</svg>`
}

function icon(letter, hue) {
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="7" fill="hsl(${hue} 45% 38%)"/><text x="16" y="22" font-family="Georgia, serif" font-size="18" font-weight="700" text-anchor="middle" fill="#fff">${letter}</text></svg>`
}

const p = (...xs) => xs.map((x) => `<p>${x}</p>`).join('')

const longform = [
  p(
    'Every newsroom keeps a few people whose job is to wait. They sit with the wires, watch the feeds, and decide which of the thousand small events of a morning deserve a second look. Most of the craft is restraint.',
    'The tools they use have changed less than you would think. A list, a time, a source, and a way to set something aside for later. What has changed is the volume: a single desk now sees in an hour what a regional paper once saw in a week.',
  ),
  `<h2>Reading at the speed of arrival</h2>`,
  p(
    'The instinct is to build faster filters. But speed is rarely the problem. The problem is that a fast stream pushes everything to the same weight, and a reader who cannot tell a bulletin from an essay ends up skimming both.',
  ),
  `<figure><img src="${FEEDS}/art/7.svg" alt="Abstract composition of overlapping blue and amber shapes"><figcaption>The morning desk at a regional broadcaster, rebuilt around a single shared timeline.</figcaption></figure>`,
  p(
    'Good reading tools separate two moments that most software collapses into one: noticing that something arrived, and deciding to give it your attention. The first should be cheap and quiet. The second deserves a full page, generous type and no distractions.',
  ),
  `<blockquote><p>The wire is a record of when things happened to reach you. It is not a ranking of what matters.</p></blockquote>`,
  p(
    'That distinction sounds academic until you watch someone use it. Readers who can see time, rather than an algorithm’s guess at importance, make calmer decisions. They know what they have already seen. They trust that nothing was quietly reordered while they looked away.',
    'None of this requires novelty. It requires holding back: one accent colour, one typeface for reading, and a list that stays still while you work through it.',
  ),
].join('')

const feeds = [
  { slug: 'field-notes', title: 'Field Notes', site: 'Field Notes', letter: 'F', hue: 222, folder: 'Writing' },
  { slug: 'the-long-view', title: 'The Long View', site: 'The Long View', letter: 'L', hue: 28, folder: 'Writing' },
  { slug: 'small-systems', title: 'Small Systems', site: 'Small Systems', letter: 'S', hue: 160, folder: 'Engineering' },
  { slug: 'port-and-starboard', title: 'Port & Starboard', site: 'Port & Starboard', letter: 'P', hue: 200, folder: 'Engineering' },
  { slug: 'kernel-weekly', title: 'Kernel Weekly', site: 'Kernel Weekly', letter: 'K', hue: 280, folder: 'Engineering' },
  { slug: 'quiet-type', title: 'Quiet Type', site: 'Quiet Type', letter: 'Q', hue: 340, folder: 'Design' },
  { slug: 'grid-and-margin', title: 'Grid & Margin', site: 'Grid & Margin', letter: 'G', hue: 45, folder: 'Design' },
  { slug: 'cartographers-desk', title: 'The Cartographer’s Desk', site: 'The Cartographer’s Desk', letter: 'C', hue: 100, folder: 'Design' },
]

const articles = [
  ['field-notes', 0.4, 'The patience of a news desk', 'Most of the craft is restraint: deciding which of a thousand small events deserve a second look.', longform, 7],
  ['small-systems', 1.2, 'SQLite is enough for your home server', 'One writer, a write-ahead log, and a backup you have actually restored. That covers more than you think.', p('A single binary and a single file can carry a surprising amount of weight.'), 1],
  ['quiet-type', 2.1, 'Optical sizes, finally on the web', 'Variable fonts with an opsz axis let one family serve a 12-pixel caption and a 48-pixel headline without compromise.', p('Optical sizing is the oldest trick in metal type.'), 2],
  ['port-and-starboard', 3.0, 'What a harbour pilot knows about latency', 'Ships do not stop quickly. Neither do queues. A short essay on planning for the turn you cannot take.', p('Every pilot learns to steer for where the ship will be.'), 3],
  ['kernel-weekly', 4.5, 'Kernel Weekly: io_uring grows up', 'This week: registered buffers by default, a new networking fast path, and a long-awaited cleanup of the timer code.', p('A quieter release cycle than most.'), 0],
  ['the-long-view', 6.0, 'Letters to a younger engineer', 'On choosing boring tools, writing things down, and the strange freedom of a project with a single user.', p('You will be told to move fast.'), 4],
  ['grid-and-margin', 8.0, 'Why your reading column is too wide', 'Somewhere around 70 characters, the eye loses its place on the return sweep. A short history of the measure.', p('Typesetters argued about this for centuries.'), 5],
  ['cartographers-desk', 10.0, 'Drawing coastlines at every zoom level', 'Generalisation is an editorial act. Which headland survives at 1:250,000 is a decision, not an algorithm.', p('A coastline has no true length.'), 6],
  ['small-systems', 22.0, 'Backups you have never restored are a rumour', 'A checklist for proving, not hoping, that your home server can come back from a bad day.', p('Schedule the restore, not just the backup.'), 8],
  ['field-notes', 25.0, 'Notes from a week without notifications', 'I turned everything off and read my feeds twice a day. Here is what changed, and what did not.', p('The first two days were loud.'), 9],
  ['quiet-type', 27.0, 'A serif for screens that does not apologise', 'Newsreader was drawn for on-screen news. It earns its place in long-form reading.', p('Some serifs survive the pixel grid.'), 10],
  ['kernel-weekly', 30.0, 'Kernel Weekly: the scheduler debate, summarised', 'Three proposals, two benchmarks, and one very long mailing-list thread, condensed into ten minutes.', p('The thread ran to 400 messages.'), 0],
  ['port-and-starboard', 46.0, 'Tides, tables and the joy of predictable systems', 'Harbour tide tables are a 200-year-old example of an API that never breaks its contract.', p('Tide tables have not changed format in generations.'), 11],
  ['the-long-view', 50.0, 'In praise of the single-user product', 'When you build for one reader, every decision can be specific. That is a feature, not a limitation.', p('Software for one is a different discipline.'), 12],
  ['grid-and-margin', 70.0, 'Hairlines, rules and the structure of a page', 'Lines on a page should carry meaning. A short guide to using structure instead of decoration.', p('A rule says: these things belong apart.'), 13],
]

function rss(feed) {
  const items = articles
    .filter((a) => a[0] === feed.slug)
    .map(([, h, title, excerpt, body, img], i) => {
      const thumb = img ? `<p><img src="${FEEDS}/art/${img}.svg" alt=""></p>` : ''
      return `<item>
  <title>${title.replace(/&/g, '&amp;')}</title>
  <link>${FEEDS}/${feed.slug}/${i}</link>
  <guid>${feed.slug}-${i}</guid>
  <pubDate>${ago(h)}</pubDate>
  <description><![CDATA[${excerpt}]]></description>
  <content:encoded xmlns:content="http://purl.org/rss/1.0/modules/content/"><![CDATA[${thumb}${body}]]></content:encoded>
</item>`
    })
    .join('\n')
  return `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel>
<title>${feed.title.replace(/&/g, '&amp;')}</title><link>${FEEDS}/${feed.slug}/</link>
<description>Invented feed for screenshots</description>
<image><url>${FEEDS}/icon/${feed.slug}.svg</url><title>x</title><link>${FEEDS}/${feed.slug}/</link></image>
${items}
</channel></rss>`
}

function opml() {
  const folders = [...new Set(feeds.map((f) => f.folder))]
  // A feed whose address is gone, to show a failing subscription.
  const failing = '<outline text="Writing"><outline type="rss" text="The Old Wire" xmlUrl="' + FEEDS + '/old-wire.xml"/></outline>'
  const body = failing + folders
    .map(
      (folder) =>
        `<outline text="${folder}">${feeds
          .filter((f) => f.folder === folder)
          .map((f) => `<outline type="rss" text="${f.title.replace(/&/g, '&amp;')}" xmlUrl="${FEEDS}/${f.slug}.xml" htmlUrl="${FEEDS}/${f.slug}/"/>`)
          .join('')}</outline>`,
    )
    .join('')
  return `<?xml version="1.0"?><opml version="2.0"><head><title>Screenshots</title></head><body>${body}</body></opml>`
}

const fixtures = createServer((req, res) => {
  const url = req.url ?? '/'
  let m
  if ((m = url.match(/^\/art\/(\d+)\.svg$/))) {
    res.writeHead(200, { 'Content-Type': 'image/svg+xml' }).end(art(Number(m[1])))
  } else if ((m = url.match(/^\/icon\/([\w-]+)\.svg$/))) {
    const f = feeds.find((x) => x.slug === m[1])
    res.writeHead(200, { 'Content-Type': 'image/svg+xml' }).end(icon(f?.letter ?? '?', f?.hue ?? 0))
  } else if ((m = url.match(/^\/([\w-]+)\.xml$/))) {
    const f = feeds.find((x) => x.slug === m[1])
    if (!f) return res.writeHead(404).end()
    res.writeHead(200, { 'Content-Type': 'application/rss+xml' }).end(rss(f))
  } else {
    res.writeHead(404).end()
  }
})

// --------------------------------------------------------------------- run

const data = mkdtempSync(join(tmpdir(), 'correspondent-shots-'))
const run = (args, input) => execFileSync(bin, [...args, '-data', data], { input, stdio: ['pipe', 'ignore', 'inherit'] })
let server

try {
  await new Promise((r) => fixtures.listen(8098, '127.0.0.1', r))
  run(['user', 'add', USER], PASSWORD + '\n')
  // Other readers on the server, for the admin page.
  run(['user', 'add', 'marguerite'], 'another-password-1\n')
  run(['user', 'add', 'tomas'], 'another-password-2\n')
  const opmlPath = join(data, 'feeds.opml')
  await import('node:fs').then((fs) => fs.writeFileSync(opmlPath, opml()))
  run(['import-freshrss', USER, '-opml', opmlPath])

  server = spawn(bin, ['serve', '-addr', '127.0.0.1:8099', '-data', data, '-insecure-cookies', '-allow-private-feeds'], {
    stdio: ['ignore', 'ignore', 'inherit'],
  })

  const browser = await chromium.launch({ channel: 'chrome' })
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 })
  const api = ctx.request
  for (let i = 0; ; i++) {
    try {
      const r = await api.post(`${APP}/api/v1/session`, { headers: { 'X-Correspondent': '1' }, data: { username: USER, password: PASSWORD } })
      if (r.ok()) break
    } catch {}
    if (i > 50) throw new Error('server did not start')
    await new Promise((r) => setTimeout(r, 200))
  }
  // Wait for the first poll of every feed.
  for (let i = 0; ; i++) {
    const tree = await (await api.get(`${APP}/api/v1/tree`)).json()
    if (tree.feeds.every((f) => f.lastFetchAt > 0)) break
    if (i > 100) throw new Error('feeds were not polled')
    await new Promise((r) => setTimeout(r, 200))
  }

  // A realistic mix of states: a few read, a couple starred.
  const { entries } = await (await api.get(`${APP}/api/v1/entries?view=all&limit=100`)).json()
  const byTitle = (t) => entries.find((e) => e.title.startsWith(t)).id
  const mutate = (action, ids) => api.post(`${APP}/api/v1/entries/state`, { headers: { 'X-Correspondent': '1' }, data: { action, ids } })
  await mutate('read', [byTitle('SQLite is enough'), byTitle('Backups you have never'), byTitle('Kernel Weekly: the scheduler')])
  await mutate('star', [byTitle('Optical sizes'), byTitle('Letters to a younger')])
  const hero = byTitle('The patience of a news desk')

  // Subscription states for the management screen.
  const tree = await (await api.get(`${APP}/api/v1/tree`)).json()
  const feedID = (title) => tree.feeds.find((f) => f.title === title).id
  const patchFeed = (title, data) => api.patch(`${APP}/api/v1/feeds/${feedID(title)}`, { headers: { 'X-Correspondent': '1' }, data })
  await patchFeed('Kernel Weekly', { paused: true })
  await patchFeed('Field Notes', { notify: true })

  const prefs = (p) => api.put(`${APP}/api/v1/prefs`, { headers: { 'X-Correspondent': '1' }, data: { markOnOpen: false, ...p } })
  const page = await ctx.newPage()

  async function shot(name, { theme = 'day', hash = `#/unread/e/${hero}`, viewport, setup } = {}) {
    await prefs({ theme })
    if (viewport) await page.setViewportSize(viewport)
    // Hash-only navigations don't reload; start every shot from a fresh page.
    await page.goto('about:blank')
    await page.goto(`${APP}/${hash}`)
    await page.waitForSelector(hash.includes('manage') ? '.manage .row' : hash.includes('settings') ? '.settings .wire-preview' : hash.includes('admin') ? '.admin .account' : '.wire .row', { state: 'attached' })
    await page.waitForLoadState('networkidle')
    if (setup) await setup()
    await page.waitForTimeout(400)
    await page.screenshot({ path: join(out, `${name}.png`) })
    console.log('wrote', `docs/screenshots/${name}.png`)
  }

  await shot('desktop-day')
  await shot('desktop-night', { theme: 'night' })
  await shot('reading-evening', {
    theme: 'evening',
    setup: async () => {
      await page.keyboard.press('f')
    },
  })
  await shot('command-palette', {
    setup: async () => {
      await page.keyboard.press('Meta+k')
      await page.keyboard.type('desi')
    },
  })
  await shot('add-feed', {
    theme: 'night',
    setup: async () => {
      await page.getByRole('button', { name: 'Add feed' }).click()
      await page.fill('#feed-url', `${FEEDS}/quiet-type.xml`)
      await page.keyboard.press('Enter')
      await page.waitForSelector('.cand')
    },
  })
  await shot('manage', { hash: '#/manage' })
  await shot('settings', { hash: '#/settings' })
  await shot('admin', { hash: '#/admin' })
  await shot('manage-delete', {
    hash: '#/manage',
    theme: 'night',
    setup: async () => {
      await page.getByRole('button', { name: 'Delete Grid & Margin' }).click()
    },
  })
  await shot('mobile-wire', { viewport: { width: 390, height: 844 }, hash: '#/unread' })
  await shot('mobile-article', { viewport: { width: 390, height: 844 } })
  await shot('mobile-manage', { viewport: { width: 390, height: 844 }, hash: '#/manage' })
  await shot('mobile-settings', { viewport: { width: 390, height: 844 }, hash: '#/settings' })
  await shot('mobile-swipe', {
    viewport: { width: 390, height: 844 },
    hash: '#/unread',
    setup: async () => {
      // Hold a swipe mid-gesture on the second row, past the commit point.
      await page.locator('.wire .row').nth(1).evaluate((li) => {
        const ev = (type, x) => new PointerEvent(type, { clientX: x, clientY: 300, pointerId: 7, pointerType: 'touch', isPrimary: true, bubbles: true })
        li.dispatchEvent(ev('pointerdown', 40))
        li.dispatchEvent(ev('pointermove', 60))
        li.dispatchEvent(ev('pointermove', 170))
      })
    },
  })

  await browser.close()
} finally {
  server?.kill()
  fixtures.close()
  rmSync(data, { recursive: true, force: true })
}
