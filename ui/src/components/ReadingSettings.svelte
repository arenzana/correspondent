<script lang="ts">
  import { app } from '../lib/state.svelte'
  import ThemePicker from './ThemePicker.svelte'
  import TextControls from './TextControls.svelte'

  let el: HTMLElement | undefined = $state()

  $effect(() => {
    const close = (e: MouseEvent) => {
      if (el && !el.parentElement?.contains(e.target as Node)) app.readingOpen = false
    }
    // Escape closes only this: stopped here, it never reaches the window's
    // shortcuts, which would close the article too.
    const esc = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      app.readingOpen = false
      e.stopPropagation()
    }
    setTimeout(() => document.addEventListener('click', close))
    document.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('click', close)
      document.removeEventListener('keydown', esc)
    }
  })
</script>

<div class="pop" bind:this={el} role="dialog" aria-label="Reading settings">
  <ThemePicker />

  <TextControls />

  <fieldset>
    <legend>List density</legend>
    <div class="seg">
      <button aria-pressed={app.prefs.density === 'comfortable'} onclick={() => app.setPrefs({ density: 'comfortable' })}>Comfortable</button>
      <button aria-pressed={app.prefs.density === 'compact'} onclick={() => app.setPrefs({ density: 'compact' })}>Compact</button>
    </div>
  </fieldset>
</div>

<style>
  .pop {
    position: absolute;
    right: 0;
    top: 40px;
    z-index: 20;
    width: 340px;
    padding: 16px;
    display: grid;
    gap: 16px;
    background: var(--sheet);
    border: 1px solid var(--rule);
    border-radius: var(--radius-l);
    box-shadow: var(--overlay-shadow);
    animation: pop 140ms var(--ease);
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
  fieldset {
    border: 0;
    margin: 0;
    padding: 0;
  }
  legend {
    display: flex;
    justify-content: space-between;
    width: 100%;
    margin-bottom: 8px;
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 560;
    color: var(--ink);
  }
  .seg {
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: 1fr;
    padding: 2px;
    border-radius: var(--radius-s);
    background: var(--fog);
  }
  .seg button {
    height: 28px;
    border-radius: 4px;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  .seg button[aria-pressed='true'] {
    background: var(--sheet);
    color: var(--ink);
    box-shadow: 0 0 0 1px var(--rule);
  }
</style>
