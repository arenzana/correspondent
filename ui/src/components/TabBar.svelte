<script lang="ts">
  // Phone navigation: always-visible tabs at the bottom, clear of the home
  // indicator. Hidden while an article is open (it has its own toolbar).
  import { app } from '../lib/state.svelte'
  import Icon, { type IconName } from './Icon.svelte'

  const isView = (v: string) => app.page === 'reader' && app.pane === 'list' && app.source.kind === 'view' && app.source.view === v

  const tabs: { id: string; label: string; icon: IconName; active: () => boolean; run: () => void; badge?: () => number }[] = [
    { id: 'feeds', label: 'Feeds', icon: 'menu', active: () => app.pane === 'nav', run: () => (app.pane = 'nav') },
    {
      id: 'unread', label: 'Unread', icon: 'inbox', active: () => isView('unread'),
      run: () => app.go({ kind: 'view', view: 'unread' }), badge: () => app.counts.unread,
    },
    { id: 'starred', label: 'Starred', icon: 'star', active: () => isView('starred'), run: () => app.go({ kind: 'view', view: 'starred' }) },
    { id: 'search', label: 'Search', icon: 'search', active: () => app.paletteOpen, run: () => (app.paletteOpen = true) },
    { id: 'settings', label: 'Settings', icon: 'settings', active: () => app.settingsOpen, run: () => (app.settingsOpen = true) },
  ]
</script>

<nav class="tabbar" aria-label="Main">
  {#each tabs as t (t.id)}
    <button class="tab" aria-current={t.active() ? 'page' : undefined} onclick={t.run}>
      <span class="ic">
        <Icon name={t.icon} size={22} />
        {#if t.badge && t.badge() > 0}<span class="badge">{t.badge() > 999 ? '999+' : t.badge()}</span>{/if}
      </span>
      <span class="label">{t.label}</span>
    </button>
  {/each}
</nav>

<style>
  .tabbar {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 20;
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    height: calc(56px + env(safe-area-inset-bottom));
    padding: 0 env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left);
    background: color-mix(in oklab, var(--sheet) 92%, transparent);
    backdrop-filter: blur(12px);
    border-top: 1px solid var(--rule);
  }
  .tab {
    display: grid;
    justify-items: center;
    align-content: center;
    gap: 2px;
    min-height: 48px; /* comfortable touch target */
    color: var(--slate);
    font-size: 10.5px;
    font-weight: 500;
  }
  .tab[aria-current='page'] {
    color: var(--accent);
  }
  .ic {
    position: relative;
    display: grid;
  }
  .badge {
    position: absolute;
    top: -5px;
    left: 14px;
    min-width: 17px;
    height: 17px;
    padding: 0 4px;
    border-radius: 9px;
    background: var(--accent);
    color: var(--sheet);
    font-size: 10.5px;
    font-weight: 600;
    line-height: 17px;
    text-align: center;
    font-variant-numeric: tabular-nums;
  }
</style>
