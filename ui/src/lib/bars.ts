// On phones, the header and tab bar step aside while you scroll down a
// list or an article, and come back as soon as you scroll up (or reach the
// top). BarTracker decides from scroll positions alone, so it can be tested.

/** Below this width the layout shows one pane at a time (App.svelte). */
export const NARROW = '(max-width: 699px)'

/** Panes that scroll less than this (about the header and tab bar together) keep their bars. */
export const MIN_RANGE = 200

export class BarTracker {
  hidden = false
  /** The furthest point in the current direction: the lowest while shown, the highest while hidden. */
  private anchor = 0
  private quietUntil = 0

  /**
   * Feed it each scroll position (top), the furthest the pane can scroll
   * (max), and the time; it returns whether the bars should be hidden.
   */
  update(top: number, max: number, now: number): boolean {
    if (top < 48) {
      // Near the top, always shown.
      this.anchor = top
      return this.set(false, now)
    }
    // Showing or hiding the bars resizes the pane, which can move the
    // scroll position by itself (at the end of a list, it clamps as the pane
    // grows): that isn't the reader scrolling up.
    if (now < this.quietUntil || (this.hidden && top >= max - 4)) {
      this.anchor = top
      return this.hidden
    }
    const moved = top - this.anchor
    // A pane that scrolls less than the bars are tall would fit without
    // scrolling once they hid, snap back to the top and bring them back:
    // short ones keep their bars.
    if (!this.hidden && moved > 24 && max >= MIN_RANGE) return this.set(true, now, top)
    if (this.hidden && moved < -12) return this.set(false, now, top)
    if ((this.hidden && moved > 0) || (!this.hidden && moved < 0)) this.anchor = top
    return this.hidden
  }

  private set(hidden: boolean, now: number, top = this.anchor): boolean {
    if (hidden !== this.hidden) {
      this.hidden = hidden
      this.quietUntil = now + 350
    }
    this.anchor = top
    return hidden
  }
}
