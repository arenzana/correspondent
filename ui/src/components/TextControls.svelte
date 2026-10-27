<script lang="ts">
  // Typeface and size controls, shared by the article's Aa menu and Settings.
  // With `interfaceText`, the app's own text (lists, menus, buttons) too.
  import { app, UI_SCALES } from '../lib/state.svelte'

  let { interfaceText = false }: { interfaceText?: boolean } = $props()
</script>

{#if interfaceText}
  <fieldset>
    <legend>Interface text size</legend>
    <div class="seg">
      {#each UI_SCALES as s (s.value)}
        <button aria-pressed={app.prefs.uiScale === s.value} onclick={() => app.setPrefs({ uiScale: s.value })}>{s.label}</button>
      {/each}
    </div>
  </fieldset>

  <fieldset>
    <legend>Interface typeface</legend>
    <div class="seg">
      <button aria-pressed={app.prefs.uiFont === 'grotesk'} class="grotesk" onclick={() => app.setPrefs({ uiFont: 'grotesk' })}>Grotesk</button>
      <button aria-pressed={app.prefs.uiFont === 'system'} class="system" onclick={() => app.setPrefs({ uiFont: 'system' })}>System</button>
    </div>
  </fieldset>
{/if}

<fieldset>
  <legend>{interfaceText ? 'Article typeface' : 'Typeface'}</legend>
  <div class="seg">
    <button aria-pressed={app.prefs.readFont === 'serif'} class="serif" onclick={() => app.setPrefs({ readFont: 'serif' })}>Newsreader</button>
    <button aria-pressed={app.prefs.readFont === 'sans'} class="grotesk" onclick={() => app.setPrefs({ readFont: 'sans' })}>Grotesk</button>
  </div>
</fieldset>

<fieldset>
  <legend>Bionic reading</legend>
  <div class="seg">
    <button aria-pressed={!app.prefs.bionic} onclick={() => app.setPrefs({ bionic: false })}>Off</button>
    <button aria-pressed={app.prefs.bionic} onclick={() => app.setPrefs({ bionic: true })}><b>Bi</b>onic</button>
  </div>
</fieldset>

<fieldset>
  <legend>Spotlight</legend>
  <div class="seg">
    <button aria-pressed={app.prefs.spotlight === 'off'} onclick={() => app.setPrefs({ spotlight: 'off' })}>Off</button>
    <button aria-pressed={app.prefs.spotlight === 'paragraph'} onclick={() => app.setPrefs({ spotlight: 'paragraph' })}>Paragraph</button>
    <button aria-pressed={app.prefs.spotlight === 'line'} onclick={() => app.setPrefs({ spotlight: 'line' })}>Line</button>
  </div>
  {#if app.prefs.spotlight !== 'off'}<p class="hint">↓ and ↑ move it; scrolling moves it too.</p>{/if}
</fieldset>

<label class="slider">
  <span>{interfaceText ? 'Article text size' : 'Text size'} <output>{app.prefs.readSize}px</output></span>
  <input type="range" min="15" max="24" step="1" value={app.prefs.readSize}
    oninput={(e) => app.setPrefs({ readSize: Number(e.currentTarget.value) })} />
</label>

<label class="slider">
  <span>Line length <output>{app.prefs.measure} characters</output></span>
  <input type="range" min="54" max="84" step="2" value={app.prefs.measure}
    oninput={(e) => app.setPrefs({ measure: Number(e.currentTarget.value) })} />
</label>

<style>
  fieldset {
    border: 0;
    margin: 0;
    padding: 0;
  }
  legend,
  .slider > span {
    display: flex;
    justify-content: space-between;
    width: 100%;
    margin-bottom: 8px;
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 560;
    color: var(--ink);
  }
  .slider {
    display: block;
  }
  .hint {
    margin: 6px 0 0;
    font-size: calc(12px * var(--ui-scale));
    color: var(--slate);
  }
  output {
    font-weight: 400;
    color: var(--slate);
    font-variant-numeric: tabular-nums;
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
    min-height: 28px;
    padding: 0 4px;
    border-radius: 4px;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  /* Each typeface choice is set in its own face. */
  .seg .serif {
    font-family: var(--font-read);
    font-size: calc(14px * var(--ui-scale));
  }
  .seg .grotesk {
    font-family: var(--font-grotesk);
  }
  .seg .system {
    font-family: var(--font-system);
  }
  .seg button[aria-pressed='true'] {
    background: var(--sheet);
    color: var(--ink);
    box-shadow: 0 0 0 1px var(--rule);
  }
  input[type='range'] {
    width: 100%;
    accent-color: var(--accent);
  }
</style>
