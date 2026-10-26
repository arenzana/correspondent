<script lang="ts">
  // The Correspondent mark without its tile: the refined "wire" glyph
  // (brand/exports/wire), same geometry, coloured for the current theme.
  // The ring is filled with the surface colour so the spine stops at it.
  let { height = 24, surface = 'var(--fog)' }: { height?: number; surface?: string } = $props()
</script>

<svg
  class="mark"
  viewBox="224 150 208 724"
  height={height}
  width={(height * 208) / 724}
  aria-hidden="true"
  style:--surface={surface}
>
  <path class="spine" d="M328 160 L328 864" />
  <circle class="unread" cx="328" cy="360" r="96" />
  <circle class="read" cx="328" cy="664" r="72" />
</svg>

<style>
  .mark {
    flex: none;
    overflow: visible;
  }
  .spine {
    stroke: color-mix(in oklab, var(--accent) 45%, var(--surface));
    stroke-width: 28;
    stroke-linecap: round;
  }
  .unread {
    fill: var(--accent);
  }
  .read {
    fill: var(--surface);
    stroke: var(--accent);
    stroke-width: 56;
  }
</style>
