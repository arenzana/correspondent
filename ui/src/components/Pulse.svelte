<script lang="ts">
  // Fourteen-day publishing cadence, oldest first. Quiet by design: it is a
  // texture that tells you which feeds are alive, not a chart to read.
  let { days, label }: { days: number[]; label: string } = $props()
  const peak = $derived(Math.max(1, ...days))
  const total = $derived(days.reduce((a, b) => a + b, 0))
</script>

<span class="pulse" role="img" aria-label="{label}: {total} articles in the last 14 days">
  {#each days as n, i (i)}
    <span class="bar" class:zero={n === 0} style:--v={n / peak}></span>
  {/each}
</span>

<style>
  .pulse {
    display: inline-flex;
    align-items: flex-end;
    gap: 1.5px;
    height: 10px;
  }
  .bar {
    width: 2.5px;
    height: calc(2px + var(--v) * 8px);
    border-radius: 1px;
    background: var(--accent);
    opacity: 0.55;
  }
  .bar.zero {
    background: var(--ring);
    opacity: 0.6;
  }
</style>
