<script lang="ts">
  // The logo row, with the arrow that folds the sidebar away and back. It
  // sits on the sidebar, or on the middle column while the sidebar is folded.
  import { app } from '../lib/state.svelte'
  import { receive, send } from '../lib/dock.svelte'
  import Icon from './Icon.svelte'
  import Mark from './Mark.svelte'
</script>

<header class="brand" in:receive={{ key: 'brand' }} out:send={{ key: 'brand' }}>
  <Mark size={28} />
  <span class="wordmark">Correspondent</span>
  <button
    class="icon-btn fold"
    aria-label={app.prefs.sidebar ? 'Hide feeds' : 'Show feeds'}
    aria-expanded={app.prefs.sidebar}
    title="{app.prefs.sidebar ? 'Hide' : 'Show'} feeds ([)"
    onclick={() => app.setPrefs({ sidebar: !app.prefs.sidebar })}
  >
    <Icon name={app.prefs.sidebar ? 'back' : 'chevron'} size={18} />
  </button>
</header>

<style>
  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: calc(18px + env(safe-area-inset-top)) 12px 14px 18px;
  }
  .wordmark {
    flex: 1;
    min-width: 0;
    font-family: var(--font-read);
    font-size: calc(21px * var(--ui-scale));
    font-weight: 560;
    font-variation-settings: 'opsz' 48;
    letter-spacing: -0.01em;
    white-space: nowrap;
  }
  .fold {
    color: var(--slate);
  }
  /* Narrower, the sidebar is a drawer: nothing to fold. */
  @media (max-width: 1099px) {
    .fold {
      display: none;
    }
  }
</style>
