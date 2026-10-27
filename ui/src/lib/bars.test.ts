import { describe, expect, it } from 'vitest'
import { BarTracker } from './bars'

describe('the bars on phones', () => {
  // A finger's scroll: many small steps, a frame apart.
  const run = (b: BarTracker, from: number, to: number, t0: number, max = 5000) => {
    let t = t0
    const step = from < to ? 6 : -6
    for (let y = from; step > 0 ? y <= to : y >= to; y += step) b.update(y, max, (t += 16))
    return t
  }

  it('step aside scrolling down, even in small steps, and come back scrolling up', () => {
    const b = new BarTracker()
    let t = run(b, 0, 400, 0)
    expect(b.hidden).toBe(true)
    t = run(b, 400, 392, t + 500)
    expect(b.hidden).toBe(true) // a nudge isn't a scroll up
    run(b, 392, 340, t)
    expect(b.hidden).toBe(false)
  })

  it('step aside on a fling straight to the end of the list', () => {
    const b = new BarTracker()
    b.update(0, 3000, 0)
    expect(b.update(3000, 3000, 16)).toBe(true)
  })

  it('stay put on a list too short to scroll past them', () => {
    const b = new BarTracker()
    b.update(0, 134, 0)
    expect(b.update(134, 134, 16)).toBe(false)
  })

  it('always show near the top', () => {
    const b = new BarTracker()
    const t = run(b, 0, 400, 0)
    b.update(20, 5000, t + 500)
    expect(b.hidden).toBe(false)
  })

  it('ignore the pane resizing itself as they move, and the end of the list', () => {
    const b = new BarTracker()
    // They hide around 72px down; the pane then grows and the position jumps back.
    let t = run(b, 0, 96, 0)
    expect(b.hidden).toBe(true)
    b.update(60, 5000, t + 30)
    expect(b.hidden).toBe(true)
    t = run(b, 96, 400, t + 500)
    // At the end, the pane grows and the position clamps with it: still hidden.
    t = run(b, 400, 1000, t + 500, 1000)
    b.update(960, 960, t + 500)
    expect(b.hidden).toBe(true)
  })
})
