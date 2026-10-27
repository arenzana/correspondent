import { describe, expect, it } from 'vitest'
import { bionicHTML, boldLength, spotlightBlocks } from './reading'

describe('bionic reading', () => {
  it('bolds about the first 40% of a word, at least one letter', () => {
    expect(boldLength('a')).toBe(1)
    expect(boldLength('the')).toBe(1)
    expect(boldLength('word')).toBe(2)
    expect(boldLength('reading')).toBe(3)
    expect(boldLength('árboles')).toBe(3)
  })

  it('bolds the start of each word and keeps markup, links and spacing', () => {
    expect(bionicHTML('<p>Read <a href="/x">this</a> now.</p>')).toBe(
      '<p><b class="bionic">Re</b>ad <a href="/x"><b class="bionic">th</b>is</a> <b class="bionic">n</b>ow.</p>',
    )
  })

  it('leaves code, preformatted text and bold text alone', () => {
    const html = '<p><code>let x</code></p><pre>a b</pre><p><strong>Bold</strong></p>'
    expect(bionicHTML(html)).toBe(html)
  })
})

describe('spotlight', () => {
  it('steps through the innermost blocks', () => {
    const div = document.createElement('div')
    div.innerHTML = '<p>One</p><ul><li>Two</li><li><p>Three</p></li></ul><blockquote><p>Four</p></blockquote>'
    expect(spotlightBlocks(div).map((b) => b.textContent)).toEqual(['One', 'Two', 'Three', 'Four'])
  })
})
