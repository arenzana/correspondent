<script lang="ts">
  // Favicon with a lettered fallback tile. The tile hue is derived from the
  // feed title so each feed keeps a stable, muted colour.
  let { url, title, size = 16 }: { url: string; title: string; size?: number } = $props()
  let failed = $state(false)

  const letter = $derived((title.match(/[\p{L}\p{N}]/u)?.[0] ?? '·').toUpperCase())
  const hue = $derived([...title].reduce((h, c) => (h * 31 + c.charCodeAt(0)) % 360, 7))
</script>

{#if url && !failed}
  <img
    class="icon"
    src={url}
    alt=""
    width={size}
    height={size}
    loading="lazy"
    referrerpolicy="no-referrer"
    onerror={() => (failed = true)}
    style:--s="{size}px"
  />
{:else}
  <span class="tile" aria-hidden="true" style:--s="{size}px" style:--h={hue}>{letter}</span>
{/if}

<style>
  .icon,
  .tile {
    width: var(--s);
    height: var(--s);
    flex: none;
    border-radius: 4px;
  }
  .icon {
    object-fit: contain;
  }
  .tile {
    display: inline-grid;
    place-items: center;
    font-size: calc(var(--s) * 0.62);
    font-weight: 600;
    line-height: 1;
    color: oklch(0.42 0.06 var(--h));
    background: oklch(0.9 0.035 var(--h));
  }
  :global([data-theme='night']) .tile {
    color: oklch(0.85 0.05 var(--h));
    background: oklch(0.32 0.04 var(--h));
  }
</style>
