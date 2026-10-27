// Reading aids for the article: bionic reading (the start of each word in
// bold, for the eye to jump from word to word) and the spotlight (one
// paragraph, or one line, clear; the rest dimmed). The Apple apps carry the
// same logic in apple/App/Resources/reading.js.

/** How many letters of a word to bold: about 40%, at least one. */
export function boldLength(word: string): number {
  const n = [...word].length
  if (n <= 3) return 1
  if (n === 4) return 2
  return Math.ceil(n * 0.4)
}

// Code and preformatted text keep their letters as they are.
const SKIP = new Set(['CODE', 'PRE', 'KBD', 'SAMP', 'SCRIPT', 'STYLE', 'B', 'STRONG'])

/** Article HTML with the start of each word bolded (as <b class="bionic">). */
export function bionicHTML(html: string): string {
  const doc = new DOMParser().parseFromString(`<div>${html}</div>`, 'text/html')
  const root = doc.body.firstElementChild as HTMLElement
  const texts: Text[] = []
  const walker = doc.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode: (n) => {
      for (let p = n.parentElement; p && p !== root; p = p.parentElement) if (SKIP.has(p.tagName)) return NodeFilter.FILTER_REJECT
      return /\p{L}/u.test(n.nodeValue ?? '') ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT
    },
  })
  while (walker.nextNode()) texts.push(walker.currentNode as Text)
  for (const t of texts) {
    const frag = doc.createDocumentFragment()
    for (const part of (t.nodeValue ?? '').split(/(\p{L}[\p{L}\p{M}'’-]*)/u)) {
      if (!part) continue
      if (/^\p{L}/u.test(part)) {
        const chars = [...part]
        const k = boldLength(part)
        const b = doc.createElement('b')
        b.className = 'bionic'
        b.textContent = chars.slice(0, k).join('')
        frag.append(b, chars.slice(k).join(''))
      } else {
        frag.append(part)
      }
    }
    t.replaceWith(frag)
  }
  return root.innerHTML
}

export type Spotlight = 'off' | 'paragraph' | 'line'

/** The blocks a paragraph spotlight steps through: the innermost ones. */
const BLOCKS = 'p, li, blockquote, pre, figure, h1, h2, h3, h4, h5, h6, table, dd, dt'

export function spotlightBlocks(prose: HTMLElement): HTMLElement[] {
  return [...prose.querySelectorAll<HTMLElement>(BLOCKS)].filter((el) => !el.querySelector(BLOCKS))
}

/** The reading line sits a little above the middle, where the eye rests. */
export const READING_LINE = 0.4

/**
 * Keeps one block lit in `prose` as `scroller` scrolls. `step` moves it to
 * the next or previous block and centres that on the reading line; `stop`
 * undoes everything.
 */
export function paragraphSpotlight(scroller: HTMLElement, prose: HTMLElement): { step: (by: number) => void; stop: () => void } {
  const blocks = spotlightBlocks(prose)
  for (const b of blocks) b.classList.add('sl')
  let lit = -1
  const light = (i: number) => {
    if (i === lit || i < 0 || i >= blocks.length) return
    blocks[lit]?.classList.remove('lit')
    blocks[i].classList.add('lit')
    lit = i
  }
  // The block nearest the reading line.
  const nearest = () => {
    const line = scroller.getBoundingClientRect().top + scroller.clientHeight * READING_LINE
    let best = 0
    let bestD = Infinity
    blocks.forEach((b, i) => {
      const r = b.getBoundingClientRect()
      const d = line < r.top ? r.top - line : line > r.bottom ? line - r.bottom : 0
      if (d < bestD) [best, bestD] = [i, d]
    })
    return best
  }
  let frame = 0
  let stepping = false
  const onScroll = () => {
    if (stepping) return
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(() => light(nearest()))
  }
  const step = (by: number) => {
    const i = Math.min(Math.max(lit + by, 0), blocks.length - 1)
    light(i)
    const r = blocks[i].getBoundingClientRect()
    const line = scroller.getBoundingClientRect().top + scroller.clientHeight * READING_LINE
    stepping = true
    scroller.scrollBy({ top: r.top + r.height / 2 - line, behavior: 'smooth' })
    setTimeout(() => (stepping = false), 450)
  }
  light(nearest())
  scroller.addEventListener('scroll', onScroll, { passive: true })
  return {
    step,
    stop: () => {
      scroller.removeEventListener('scroll', onScroll)
      cancelAnimationFrame(frame)
      for (const b of blocks) b.classList.remove('sl', 'lit')
    },
  }
}
