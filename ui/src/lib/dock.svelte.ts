// The logo row and the toolbar live in the sidebar; on wide screens with
// the sidebar collapsed they dock on the middle column instead, so they
// are always on screen. Moving between the two, they glide (crossfade);
// with reduced motion they simply switch.

import { crossfade, fade } from 'svelte/transition'
import { cubicOut } from 'svelte/easing'
import { app } from './state.svelte'

const WIDE = '(min-width: 1100px)'

class Dock {
  /** Wide enough for the sidebar to be a column (narrower, it's a drawer). */
  wide = $state(typeof matchMedia === 'undefined' ? true : matchMedia(WIDE).matches)

  constructor() {
    if (typeof matchMedia !== 'undefined') matchMedia(WIDE).addEventListener('change', (e) => (this.wide = e.matches))
  }

  /** Whether the logo row and toolbar sit on the middle column. */
  get docked() {
    return this.wide && !app.prefs.sidebar
  }
}

export const dock = new Dock()

const reduced = () => typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches

export const [send, receive] = crossfade({
  duration: () => (reduced() ? 0 : 260),
  easing: cubicOut,
  fallback: (node) => fade(node, { duration: reduced() ? 0 : 120 }),
})
