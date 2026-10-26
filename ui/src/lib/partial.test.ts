import { describe, expect, it } from 'vitest'
import { looksPartial } from './partial'

const words = (n: number) => Array.from({ length: n }, (_, i) => `word${i}`).join(' ')

describe('looksPartial', () => {
  it('treats short bodies as summaries', () => {
    expect(looksPartial('')).toBe(true)
    expect(looksPartial('<p>A short teaser.</p>')).toBe(true)
  })
  it('treats longer bodies that trail off as summaries', () => {
    expect(looksPartial(`<p>${words(150)}…</p>`)).toBe(true)
    expect(looksPartial(`<p>${words(150)} [...]</p>`)).toBe(true)
    expect(looksPartial(`<p>${words(150)}</p><p><a href="x">Continue reading →</a></p>`)).toBe(true)
    expect(looksPartial(`<p>${words(150)}</p><p><a href="x">Read more »</a></p>`)).toBe(true)
    expect(looksPartial(`<p>${words(150)}</p><p>The post Hello appeared first on Example.</p>`)).toBe(true)
  })
  it('finds a "continue reading" link followed by other lines', () => {
    // The shape of a real Ghost member feed.
    const html = `<p>${words(110)}</p><p>Until everything dies.</p><p><strong>The first time everything died</strong></p>
<p><a href="https://example.com/post"><strong>Continue reading →</strong></a></p>
<p><a href="https://example.com/members">Get the full-text RSS feed →</a></p>`
    expect(looksPartial(html)).toBe(true)
    expect(looksPartial(`<p>${words(500)}</p><p><a href="x">Continue reading →</a></p>`)).toBe(true)
  })
  it('ignores "read more" in the middle of a complete article', () => {
    expect(looksPartial(`<p>${words(60)} you can read more about it here ${words(150)}.</p>`)).toBe(false)
  })
  it('leaves complete articles alone', () => {
    expect(looksPartial(`<p>${words(150)}.</p>`)).toBe(false)
    // A long article that happens to end with an ellipsis is still an article.
    expect(looksPartial(`<p>${words(600)}…</p>`)).toBe(false)
  })
})
