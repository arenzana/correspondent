<script lang="ts">
  // A pane edge you can drag, or focus and move with the arrow keys.
  // Double-click (or Enter) restores the default width.
  let {
    label,
    value,
    min,
    max,
    fallback,
    onresize,
    ondragging,
  }: {
    label: string
    value: number
    min: number
    max: number
    fallback: number
    onresize: (width: number, done: boolean) => void
    ondragging?: (active: boolean) => void
  } = $props()

  let start: { x: number; w: number } | null = null

  const clamp = (w: number) => Math.round(Math.min(max, Math.max(min, w)))

  function down(e: PointerEvent) {
    if (e.button !== 0) return
    e.preventDefault()
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    start = { x: e.clientX, w: value }
    ondragging?.(true)
  }
  function move(e: PointerEvent) {
    if (start) onresize(clamp(start.w + e.clientX - start.x), false)
  }
  function up(e: PointerEvent) {
    if (!start) return
    onresize(clamp(start.w + e.clientX - start.x), true)
    start = null
    ondragging?.(false)
  }
  function key(e: KeyboardEvent) {
    const step = e.shiftKey ? 48 : 16
    const next =
      e.key === 'ArrowLeft' ? value - step
      : e.key === 'ArrowRight' ? value + step
      : e.key === 'Home' ? min
      : e.key === 'End' ? max
      : e.key === 'Enter' ? fallback
      : null
    if (next === null) return
    e.preventDefault()
    e.stopPropagation() // arrows here must not move through articles
    onresize(clamp(next), true)
  }
</script>

<!-- A focusable separator is the WAI-ARIA "window splitter" pattern, which
     is interactive by definition; the linter does not know that role. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<div
  class="resizer"
  role="separator"
  aria-orientation="vertical"
  aria-label={label}
  aria-valuenow={value}
  aria-valuemin={min}
  aria-valuemax={max}
  tabindex="0"
  title="Drag to resize; double-click to reset"
  onpointerdown={down}
  onpointermove={move}
  onpointerup={up}
  onpointercancel={up}
  ondblclick={() => onresize(fallback, true)}
  onkeydown={key}
></div>

<style>
  .resizer {
    position: absolute;
    top: 0;
    bottom: 0;
    right: -4px;
    z-index: 5;
    width: 8px;
    cursor: col-resize;
    touch-action: none;
  }
  /* A hairline in the accent colour appears on hover, drag and focus. */
  .resizer::after {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    left: 3px;
    width: 2px;
    background: var(--accent);
    opacity: 0;
    transition: opacity var(--fast) var(--ease);
  }
  .resizer:hover::after,
  .resizer:active::after,
  .resizer:focus-visible::after {
    opacity: 1;
  }
  .resizer:focus-visible {
    outline: none;
  }
</style>
