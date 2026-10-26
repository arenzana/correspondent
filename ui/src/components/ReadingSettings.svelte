<script lang="ts">
  import { app, type Theme } from '../lib/state.svelte'
  import { clock } from '../lib/time'
  import TextControls from './TextControls.svelte'

  // Swatches show each theme's own sheet and ink, independent of the current theme.
  const themes: { id: Theme; label: string; bg: string; fg: string }[] = [
    { id: 'auto', label: 'System', bg: 'linear-gradient(135deg, #fcfcfb 50%, #1b2230 50%)', fg: '#8a94a7' },
    { id: 'sun', label: 'Sun', bg: 'linear-gradient(135deg, #fcfcfb 33%, #f3ebdc 33% 66%, #1b2230 66%)', fg: '#6a5c48' },
    { id: 'day', label: 'Day', bg: '#fcfcfb', fg: '#2a3349' },
    { id: 'night', label: 'Night desk', bg: '#1b2230', fg: '#c5ccd8' },
    { id: 'evening', label: 'Evening', bg: '#f3ebdc', fg: '#3b3123' },
  ]

  let el: HTMLElement | undefined = $state()

  $effect(() => {
    const close = (e: MouseEvent) => {
      if (el && !el.parentElement?.contains(e.target as Node)) app.readingOpen = false
    }
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && (app.readingOpen = false)
    setTimeout(() => document.addEventListener('click', close))
    document.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('click', close)
      document.removeEventListener('keydown', esc)
    }
  })
</script>

<div class="pop" bind:this={el} role="dialog" aria-label="Reading settings">
  <fieldset>
    <legend>Theme</legend>
    <div class="themes">
      {#each themes as t (t.id)}
        <button class="theme" aria-pressed={app.prefs.theme === t.id}
          onclick={() => (t.id === 'sun' ? app.useSunTheme() : app.setPrefs({ theme: t.id }))}>
          <span class="swatch" style:background={t.bg} style:color={t.fg}>Aa</span>
          {t.label}
        </button>
      {/each}
    </div>
    {#if app.prefs.theme === 'sun' && app.sunNext}
      <p class="sun-note">
        {{ day: 'Day', evening: 'Evening', night: 'Night desk' }[app.sunNext.theme]} at {clock(app.sunNext.at)}{#if app.prefs.place?.estimated},
          estimated from your time zone{/if}.
      </p>
    {/if}
  </fieldset>

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
  .themes {
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    gap: 6px;
  }
  .theme {
    display: grid;
    justify-items: center;
    gap: 6px;
    font-size: calc(11.5px * var(--ui-scale));
    color: var(--slate);
  }
  .theme[aria-pressed='true'] {
    color: var(--ink);
    font-weight: 560;
  }
  .swatch {
    display: grid;
    place-items: center;
    width: 100%;
    height: 40px;
    border-radius: var(--radius-s);
    border: 1px solid var(--rule);
    font-family: var(--font-read);
    font-size: calc(15px * var(--ui-scale));
  }
  .theme[aria-pressed='true'] .swatch {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-wash-strong);
  }
  .sun-note {
    margin: 10px 0 0;
    font-size: calc(12px * var(--ui-scale));
    color: var(--slate);
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
