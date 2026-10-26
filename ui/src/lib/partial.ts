// Whether an article body looks like a summary of a longer article on the
// web, so the reader can offer to fetch the rest.

const MIN_FULL = 600
// Teasers are rarely longer than this; one that ends by sending you onward
// ("Continue reading") may run to several paragraphs.
const MAX_TEASER = 2500
const MAX_ONWARD = 8000
// How far from the end a "continue reading" link may sit: feeds often add a
// line or two after it (a subscribe link, "The post … appeared first on …").
const TAIL = 300

const ELLIPSIS_END = /(…|\.\.\.|\[…\]|\[\.\.\.\])[\s.»→›)\]]*$/
const ONWARD = /\b(read more|continue reading|read the (full|rest)|keep reading|the post .{1,200} appeared first on)\b/i

export function looksPartial(html: string): boolean {
  const text = html.replace(/<[^>]+>/g, ' ').replace(/&nbsp;|&#160;/g, ' ').replace(/\s+/g, ' ').trim()
  if (text.length < MIN_FULL) return true
  if (text.length < MAX_ONWARD && ONWARD.test(text.slice(-TAIL))) return true
  return text.length < MAX_TEASER && ELLIPSIS_END.test(text)
}
