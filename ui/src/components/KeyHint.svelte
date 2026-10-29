<script lang="ts">
  // The key a button answers to, on a small keycap, with Show shortcuts on.
  // Inside an .icon-btn it sits on the corner; inline, beside a row's count.
  import { app } from '../lib/state.svelte'

  let { keys, inline = false }: { keys: string; inline?: boolean } = $props()
</script>

{#if app.prefs.keyHints}<span class="keycap" class:inline aria-hidden="true">{keys}</span>{/if}
