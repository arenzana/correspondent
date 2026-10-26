// Swipe gestures for list rows on touch screens. The decision logic is pure
// so it can be tested; the Svelte action only feeds it pointer positions.

export type SwipeAction = 'read' | 'star'

/** Pixels of travel before a gesture is classified. */
export const LOCK_DISTANCE = 10
/** Fraction of the row width needed to commit an action. */
export const COMMIT_FRACTION = 0.28

/**
 * Classifies a gesture once it has moved far enough: horizontal gestures
 * become swipes, vertical ones are left to the browser for scrolling.
 */
export function lockAxis(dx: number, dy: number): 'x' | 'y' | null {
  if (Math.hypot(dx, dy) < LOCK_DISTANCE) return null
  // Require a clearly horizontal motion so diagonal scrolls never swipe.
  return Math.abs(dx) > Math.abs(dy) * 1.5 ? 'x' : 'y'
}

/** The action a released swipe commits, if any: right toggles read, left toggles star. */
export function resolveSwipe(dx: number, width: number): SwipeAction | null {
  const threshold = Math.max(64, width * COMMIT_FRACTION)
  if (dx >= threshold) return 'read'
  if (dx <= -threshold) return 'star'
  return null
}

/** Rubber-band the drag beyond the threshold so the row resists overshooting. */
export function dampen(dx: number, width: number): number {
  const limit = Math.max(64, width * COMMIT_FRACTION) * 1.4
  const abs = Math.abs(dx)
  if (abs <= limit) return dx
  return Math.sign(dx) * (limit + (abs - limit) * 0.25)
}

interface Options {
  onCommit: (action: SwipeAction) => void
}

/**
 * Svelte action. Sets --swipe-dx and data-swipe ("read" | "star" | "") on the
 * node while dragging, so CSS can move the row and reveal the action.
 */
export function swipe(node: HTMLElement, opts: Options) {
  let options = opts
  let start: { x: number; y: number; id: number } | null = null
  let axis: 'x' | 'y' | null = null
  let dx = 0
  let suppressClick = false

  const reset = () => {
    start = null
    axis = null
    dx = 0
    node.style.removeProperty('--swipe-dx')
    node.dataset.swipe = ''
    node.classList.remove('swiping')
  }

  const down = (e: PointerEvent) => {
    if (e.pointerType !== 'touch' || !e.isPrimary) return
    start = { x: e.clientX, y: e.clientY, id: e.pointerId }
  }

  const move = (e: PointerEvent) => {
    if (!start || e.pointerId !== start.id) return
    const mx = e.clientX - start.x
    const my = e.clientY - start.y
    if (!axis) axis = lockAxis(mx, my)
    if (axis !== 'x') {
      if (axis === 'y') reset()
      return
    }
    try {
      node.setPointerCapture(e.pointerId) // keep receiving moves if the finger leaves the row
    } catch {
      /* not capturable (e.g. synthetic events); the gesture still works */
    }
    node.classList.add('swiping')
    dx = mx
    const width = node.clientWidth
    node.style.setProperty('--swipe-dx', `${dampen(dx, width)}px`)
    node.dataset.swipe = resolveSwipe(dx, width) ?? (dx > 0 ? 'read-pending' : 'star-pending')
  }

  const up = (e: PointerEvent) => {
    if (!start || e.pointerId !== start.id) return
    if (axis === 'x') {
      const action = resolveSwipe(dx, node.clientWidth)
      suppressClick = true // a swipe is never also a tap
      if (action) options.onCommit(action)
    }
    reset()
  }

  const click = (e: MouseEvent) => {
    if (suppressClick) {
      e.preventDefault()
      e.stopPropagation()
      suppressClick = false
    }
  }

  node.addEventListener('pointerdown', down)
  node.addEventListener('pointermove', move)
  node.addEventListener('pointerup', up)
  node.addEventListener('pointercancel', reset)
  node.addEventListener('click', click, true)
  return {
    update(next: Options) {
      options = next
    },
    destroy() {
      node.removeEventListener('pointerdown', down)
      node.removeEventListener('pointermove', move)
      node.removeEventListener('pointerup', up)
      node.removeEventListener('pointercancel', reset)
      node.removeEventListener('click', click, true)
    },
  }
}
