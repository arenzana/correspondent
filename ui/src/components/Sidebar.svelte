<script lang="ts">
  import { app, type Source, type View } from '../lib/state.svelte'
  import type { Feed } from '../lib/api'
  import Icon, { type IconName } from './Icon.svelte'
  import FeedIcon from './FeedIcon.svelte'
  import Pulse from './Pulse.svelte'
  import Mark from './Mark.svelte'

  const views: { view: View; label: string; icon: IconName }[] = [
    { view: 'unread', label: 'Unread', icon: 'inbox' },
    { view: 'today', label: 'Today', icon: 'sun' },
    { view: 'starred', label: 'Starred', icon: 'star' },
    { view: 'all', label: 'All articles', icon: 'layers' },
  ]

  let collapsed = $state<Record<number, boolean>>(loadCollapsed())

  function loadCollapsed(): Record<number, boolean> {
    try {
      return JSON.parse(localStorage.getItem('correspondent:collapsed') ?? '{}')
    } catch {
      return {}
    }
  }
  function toggle(id: number) {
    collapsed = { ...collapsed, [id]: !collapsed[id] }
    try {
      localStorage.setItem('correspondent:collapsed', JSON.stringify(collapsed))
    } catch {
      /* not essential */
    }
  }

  const grouped = $derived.by(() => {
    const byFolder = new Map<number | null, Feed[]>()
    for (const f of app.feeds) {
      const k = f.folderId ?? null
      if (!byFolder.has(k)) byFolder.set(k, [])
      byFolder.get(k)!.push(f)
    }
    return byFolder
  })

  function viewCount(v: View): number | null {
    if (v === 'unread') return app.counts.unread
    if (v === 'today') return app.counts.today
    if (v === 'starred') return app.counts.starred
    return null
  }

  function active(s: Source): boolean {
    return app.sourceKey(s) === app.sourceKey()
  }
</script>

<nav class="sidebar" aria-label="Feeds">
  <header class="brand">
    <Mark size={28} />
    <span class="wordmark">Correspondent</span>
  </header>

  <button class="search" onclick={() => (app.paletteOpen = true)}>
    <Icon name="search" size={15} />
    <span>Search or jump to…</span>
    <kbd>⌘K</kbd>
  </button>

  <div class="scroll">
    <ul class="views">
      {#each views as v (v.view)}
        {@const s: Source = { kind: 'view', view: v.view }}
        {@const n = viewCount(v.view)}
        <li>
          <button class="row" aria-current={active(s) ? 'page' : undefined} onclick={() => app.go(s)}>
            <span class="glyph"><Icon name={v.icon} size={16} /></span>
            <span class="name">{v.label}</span>
            {#if n}<span class="count">{n}</span>{/if}
          </button>
        </li>
      {/each}
    </ul>

    {#each app.folders as folder (folder.id)}
      {@const feeds = grouped.get(folder.id) ?? []}
      {@const s: Source = { kind: 'folder', id: folder.id }}
      <section class="folder">
        <div class="folder-head">
          <button
            class="disclosure"
            aria-expanded={!collapsed[folder.id]}
            aria-label="{collapsed[folder.id] ? 'Expand' : 'Collapse'} {folder.name}"
            onclick={() => toggle(folder.id)}
          >
            <span class="chev" class:open={!collapsed[folder.id]}><Icon name="chevron" size={13} /></span>
          </button>
          <button class="row folder-row" aria-current={active(s) ? 'page' : undefined} onclick={() => app.go(s)}>
            <span class="name">{folder.name}</span>
            {#if folder.unread}<span class="count">{folder.unread}</span>{/if}
          </button>
        </div>
        {#if !collapsed[folder.id]}
          <ul>
            {#each feeds as feed (feed.id)}
              {@render feedRow(feed)}
            {/each}
          </ul>
        {/if}
      </section>
    {/each}

    {#if grouped.get(null)?.length}
      <section class="folder">
        <ul>
          {#each grouped.get(null) ?? [] as feed (feed.id)}
            {@render feedRow(feed)}
          {/each}
        </ul>
      </section>
    {/if}

    {#if app.feeds.length === 0}
      <div class="empty">
        <p>Subscribe to a site to start reading.</p>
        <button class="btn primary" onclick={() => (app.addFeedOpen = true)}>Add a feed</button>
      </div>
    {/if}
  </div>

  <footer class="foot">
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
</nav>

{#snippet feedRow(feed: Feed)}
  {@const s: Source = { kind: 'feed', id: feed.id }}
  <li>
    <button
      class="row feed"
      class:stale={feed.errorCount > 0 || feed.paused}
      aria-current={active(s) ? 'page' : undefined}
      onclick={() => app.go(s)}
      title={feed.paused ? `${feed.title}: updates paused` : feed.errorCount > 0 ? `Last update failed: ${feed.lastError}` : feed.title}
    >
      <FeedIcon url={feed.iconUrl} title={feed.title || feed.url} />
      <span class="name">{feed.title || feed.url}</span>
      {#if feed.paused}
        <span class="warn" aria-label="Updates paused"><Icon name="pause" size={12} /></span>
      {:else if feed.errorCount > 0}
        <span class="warn" aria-label="Last update failed"><Icon name="alert" size={13} /></span>
      {/if}
      {#if feed.unread}<span class="count">{feed.unread}</span>{/if}
      <span class="pulse-slot"><Pulse days={feed.cadence} label={feed.title} /></span>
    </button>
  </li>
{/snippet}

<style>
  .sidebar {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--fog);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: calc(18px + env(safe-area-inset-top)) 18px 14px;
  }
  .wordmark {
    font-family: var(--font-read);
    font-size: calc(21px * var(--ui-scale));
    font-weight: 560;
    font-variation-settings: 'opsz' 48;
    letter-spacing: -0.01em;
  }
  .search {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 12px 10px;
    height: 34px;
    padding: 0 8px 0 10px;
    border-radius: var(--radius-s);
    border: 1px solid var(--rule);
    background: var(--sheet);
    color: var(--slate);
    text-align: left;
  }
  .search span {
    flex: 1;
  }
  .search:hover {
    border-color: color-mix(in oklab, var(--rule) 50%, var(--slate));
  }

  .scroll {
    flex: 1;
    overflow-y: auto;
    padding: 2px 8px 16px;
    scrollbar-width: thin;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .views {
    padding-bottom: 10px;
    margin-bottom: 8px;
    border-bottom: 1px solid var(--rule-soft);
  }
  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    min-height: 30px;
    padding: 4px 8px;
    border-radius: var(--radius-s);
    text-align: left;
    color: var(--ink);
  }
  .row:hover {
    background: var(--hover);
  }
  .row[aria-current='page'] {
    background: var(--accent-wash-strong);
  }
  .row[aria-current='page'] .name {
    font-weight: 600;
  }
  .glyph {
    display: grid;
    color: var(--slate);
  }
  .row[aria-current='page'] .glyph {
    color: var(--accent);
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    font-size: calc(12px * var(--ui-scale));
    font-variant-numeric: tabular-nums;
    color: var(--slate);
  }

  .folder {
    margin-top: 6px;
  }
  .folder-head {
    display: flex;
    align-items: center;
  }
  .disclosure {
    display: grid;
    place-items: center;
    width: 20px;
    height: 28px;
    color: var(--slate);
    border-radius: 4px;
  }
  .chev {
    display: grid;
    transition: transform var(--fast) var(--ease);
  }
  .chev.open {
    transform: rotate(90deg);
  }
  .folder-row {
    padding-left: 4px;
    font-weight: 560;
    color: var(--slate);
  }
  .folder-row[aria-current='page'] {
    color: var(--ink);
  }
  .feed {
    padding-left: 24px;
  }
  .stale .name {
    color: var(--slate);
  }
  .warn {
    display: grid;
    color: var(--slate);
  }
  /* The pulse appears for the hovered or selected feed only. */
  .pulse-slot {
    display: none;
  }
  .feed:hover .pulse-slot,
  .feed[aria-current='page'] .pulse-slot {
    display: inline-flex;
  }
  .feed:hover .count,
  .feed[aria-current='page'] .count {
    display: none;
  }

  .empty {
    padding: 24px 12px;
    color: var(--slate);
  }

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
