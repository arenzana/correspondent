// Whether an article body looks like a summary of a longer article on the
// web, so the reader can offer to fetch the rest.

const MIN_FULL = 600
const MAX_TEASER = 2500

// The last words of a teaser: an ellipsis, or a link onward.
const TEASER_END = /(…|\.\.\.|\[…\]|\[\.\.\.\]|read more|continue reading|read the (full|rest)|keep reading|the post .* appeared first on .*)[\s.»→›)\]]*$/i

export function looksPartial(html: string): boolean {
  const text = html.replace(/<[^>]+>/g, ' ').replace(/&nbsp;|&#160;/g, ' ').replace(/\s+/g, ' ').trim()
  if (text.length < MIN_FULL) return true
  return text.length < MAX_TEASER && TEASER_END.test(text)
}
