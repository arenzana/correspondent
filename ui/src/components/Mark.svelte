<script lang="ts">
  // The Correspondent app icon, as shown beside the wordmark: the refined
  // "wire" artwork (brand/exports/wire/wire-light.svg) on its blue tile,
  // with the same continuous-curvature corners as the installed icon.
  let { size = 28 }: { size?: number } = $props()
</script>

<svg class="mark" viewBox="0 0 1024 1024" width={size} height={size} aria-hidden="true">
  <path
    fill="#2742c7"
    d="M366.51 0 L657.49 0 C785.78 0 849.926 0 898.926 24.967 C942.028 46.929 977.071 81.972 999.033 125.074 C1024 174.074 1024 238.22 1024 366.51 L1024 657.49 C1024 785.78 1024 849.926 999.033 898.926 C977.071 942.028 942.028 977.071 898.926 999.033 C849.926 1024 785.78 1024 657.49 1024 L366.51 1024 C238.22 1024 174.074 1024 125.074 999.033 C81.972 977.071 46.929 942.028 24.967 898.926 C0 849.926 0 785.78 0 657.49 L0 366.51 C0 238.22 0 174.074 24.967 125.074 C46.929 81.972 81.972 46.929 125.074 24.967 C174.074 0 238.22 0 366.51 0 Z"
  />
  <path d="M328 160 L328 864" stroke="#8291df" stroke-width="28" stroke-linecap="round" />
  <circle cx="328" cy="360" r="96" fill="#ffffff" />
  <circle cx="328" cy="664" r="72" fill="#2742c7" stroke="#ffffff" stroke-width="56" />
  <path d="M528 360 L792 360 M528 664 L704 664" stroke="#ffffff" stroke-width="56" stroke-linecap="round" />
</svg>

<style>
  .mark {
    flex: none;
    display: block;
  }
</style>
