// Contact sheet: every option at 1024, 180, 32 and 16 px, on light and dark grounds -> brand/options.png
//   node brand/source/sheet.mjs
import { chromium } from '/Users/isma/dev/correspondent/ui/node_modules/playwright-core/index.mjs'
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const S = dirname(fileURLToPath(import.meta.url))
const B = resolve(S, '..')
const options = [
  ['wire', '1 · wire (refined)'],
  ['arrival', '2 · arrival'],
  ['byline', '3 · byline'],
  ['morse', '4 · morse'],
]
const ground = { light: '#EEF1F5', dark: '#10151F' }
const ink = { light: '#18213A', dark: '#EEF1F5' }
const quiet = { light: '#5B6478', dark: '#8A93A6' }

const img = (o, th, px, show = px, cls = '') =>
  `<figure><img class="${cls}" src="${B}/exports/${o}/${o}-${th}-${px}.png" width="${show}" height="${show}"><figcaption>${px}${show !== px ? ` ×${show / px}` : ''}</figcaption></figure>`

const panel = (o, bg) => `
  <section class="panel" style="background:${ground[bg]};color:${quiet[bg]}">
    ${img(o, bg, 1024)}
    <div class="col">
      ${img(o, bg, 180)}
      <div class="row">${img(o, 'light', 32)}${img(o, 'dark', 32)}${img(o, 'light', 16)}${img(o, 'dark', 16)}</div>
      <div class="row">${img(o, bg, 16, 128, 'px')}${img(o, bg, 32, 128, 'px')}</div>
      ${o === 'wire' ? `<div class="row before"><figure><img src="${B}/../ui/public/favicon.svg" width="32" height="32"><figcaption>was 32</figcaption></figure><figure><img src="${B}/../ui/public/favicon.svg" width="16" height="16"><figcaption>was 16</figcaption></figure></div>` : ''}
    </div>
  </section>`

const apps = [['wire', 'wire'], ['byline', 'byline (app colours)'], ['morse', 'morse']]
const A = (o, f) => `${B}/exports/${o}/app/${f}`
const appRow = ([o, label]) => `
  <div class="app"><h3>${label}</h3><div class="row">
    <figure><img src="${A(o, 'apple-touch-icon.png')}" width="180" height="180"><figcaption>apple-touch-icon 180</figcaption></figure>
    <figure><div class="safe"><img src="${A(o, 'icon-maskable-512.png')}" width="256" height="256"><i></i></div><figcaption>maskable 512 (shown ½, safe zone drawn here only)</figcaption></figure>
    <figure><img src="${A(o, 'icon-512.png')}" width="180" height="180"><figcaption>icon-512 (any)</figcaption></figure>
    <figure><img src="${A(o, 'mac-icon-1024.png')}" width="256" height="256"><figcaption>mac-icon 1024 (shown ¼)</figcaption></figure>
    <figure><img src="${A(o, 'favicon-32.png')}" width="32" height="32"><figcaption>favicon 32</figcaption></figure>
    <figure><img src="${A(o, 'favicon.svg')}" width="16" height="16"><figcaption>svg @16</figcaption></figure>
    <figure><img class="px" src="${A(o, 'favicon-32.png')}" width="128" height="128"><figcaption>32 ×4</figcaption></figure>
  </div></div>`

const html = `<!doctype html><meta charset="utf-8"><style>
  * { box-sizing: border-box; margin: 0 }
  body { background: #FFFFFF; font: 22px/1.3 -apple-system, 'Helvetica Neue', sans-serif; color: #18213A; padding: 48px }
  h1 { font: 600 44px/1 'Newsreader', Georgia, serif; margin-bottom: 8px }
  p.sub { color: #5B6478; margin-bottom: 40px }
  .opt { margin-bottom: 48px }
  .opt h2 { font-size: 28px; font-weight: 600; margin: 0 0 14px 4px }
  .pair { display: flex; gap: 24px }
  .panel { display: flex; gap: 40px; padding: 48px; border-radius: 20px; align-items: flex-start }
  .col { display: flex; flex-direction: column; gap: 36px; width: 320px }
  .row { display: flex; gap: 22px; align-items: flex-end }
  figure { display: flex; flex-direction: column; align-items: center; gap: 8px }
  figcaption { font-size: 15px; font-variant-numeric: tabular-nums }
  img { display: block }
  img.px { image-rendering: pixelated }
  .apps { display: flex; flex-wrap: wrap; gap: 24px; margin-bottom: 48px }
  .app { background: #FFFFFF; border: 1px solid #D9DEE7; border-radius: 20px; padding: 32px 36px; color: #5B6478 }
  .app h3 { font-size: 24px; color: #18213A; margin-bottom: 20px }
  .app .row { gap: 28px }
  .safe { position: relative }
  .safe i { position: absolute; left: 10%; top: 10%; width: 80%; height: 80%; border-radius: 50%; border: 2px dashed #FF2D55; box-sizing: border-box }
</style>
<h1>Correspondent: icon options</h1>
<p class="sub">Each option on a light ground (light tile) and a dark ground (dark tile). 1024 shown at 1:1; the 16 and 32 px rows show both tiles; pixel-doubled crops are the actual 16 and 32 px files.</p>
<div class="opt"><h2>App and PWA sets (chosen: wire primary; byline and morse as alternates)</h2><div class="apps">${apps.map(appRow).join('')}</div></div>
${options.map(([o, label]) => `<div class="opt"><h2>${label}</h2><div class="pair">${panel(o, 'light')}${panel(o, 'dark')}</div></div>`).join('')}
`
mkdirSync(`${S}/.tmp`, { recursive: true })
writeFileSync(`${S}/.tmp/options.html`, html)
const browser = await chromium.launch({ channel: 'chrome' })
const page = await browser.newPage({ viewport: { width: 3120, height: 800 }, deviceScaleFactor: 1 })
await page.goto(`file://${S}/.tmp/options.html`)
await page.waitForLoadState('networkidle')
await page.screenshot({ path: `${B}/options.png`, fullPage: true })
await browser.close()
console.log('wrote', `${B}/options.png`)
