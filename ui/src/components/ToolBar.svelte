<script lang="ts">
  // The toolbar: add a feed, shortcuts, subscriptions, settings. It sits at
  // the foot of the sidebar, or of the middle column while the sidebar is folded.
  import { app } from '../lib/state.svelte'
  import { receive, send } from '../lib/dock.svelte'
  import Icon from './Icon.svelte'
</script>

<footer class="foot" in:receive={{ key: 'toolbar' }} out:send={{ key: 'toolbar' }}>
  <button class="icon-btn" aria-label="Add feed" title="Add feed (a)" onclick={() => (app.addFeedOpen = true)}>
    <Icon name="plus" />
  </button>
  <button class="icon-btn" aria-label="Keyboard shortcuts" title="Keyboard shortcuts (?)" onclick={() => (app.shortcutsOpen = true)}>
    <Icon name="keyboard" />
  </button>
  <button class="icon-btn" aria-label="Manage subscriptions" title="Manage subscriptions (g m)"
    aria-pressed={app.page === 'manage'} onclick={() => app.manage()}>
    <Icon name="rss" />
  </button>
  <span class="spacer"></span>
  <button class="icon-btn" aria-label="Settings" title="Settings" onclick={() => app.openSettings()}>
    <Icon name="settings" />
  </button>
</footer>

<style>
  .foot {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 8px 10px calc(8px + env(safe-area-inset-bottom));
    border-top: 1px solid var(--rule-soft);
  }
  .spacer {
    flex: 1;
  }
</style>
