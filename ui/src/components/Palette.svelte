<script lang="ts">
  // ⌘K: one place to jump to any feed or folder, find articles by their
  // text, or run an action. Feeds and actions are ranked by a simple
  // subsequence score; articles come from the server's full-text search.
  import { app, type Source } from '../lib/state.svelte'
  import { api, type Entry } from '../lib/api'
  import Modal from './Modal.svelte'
  import Icon, { type IconName } from './Icon.svelte'
  import FeedIcon from './FeedIcon.svelte'

  type Item = { id: string; label: string; hint?: string; icon?: IconName; feedIcon?: string; group: string; run: () => void }

  let q = $state('')
  /** Articles whose title or text match, as you type. */
  let found = $state<Entry[]>([])
  $effect(() => {
    const term = q.trim()
    if (term.length < 2 || !app.online) {
      found = []
      return
    }
    const t = setTimeout(async () => {
      try {
        const r = await api.entries({ view: 'all', q: term, limit: 8 })
        if (q.trim() === term) found = r.entries
      } catch {
        found = []
      }
    }, 180)
    return () => clearTimeout(t)
  })
  let active = $state(0)
  let input: HTMLInputElement | undefined = $state()

  const close = () => (app.paletteOpen = false)
  const go = (s: Source) => () => app.go(s)

  const items = $derived.by((): Item[] => {
    const scopeName = app.title
    const out: Item[] = [
      { id: 'v-unread', label: 'Unread', icon: 'inbox', group: 'Go to', hint: 'g u', run: go({ kind: 'view', view: 'unread' }) },
      { id: 'v-today', label: 'Today', icon: 'sun', group: 'Go to', hint: 'g t', run: go({ kind: 'view', view: 'today' }) },
      { id: 'v-starred', label: 'Starred', icon: 'star', group: 'Go to', hint: 'g s', run: go({ kind: 'view', view: 'starred' }) },
      { id: 'v-all', label: 'All articles', icon: 'layers', group: 'Go to', hint: 'g a', run: go({ kind: 'view', view: 'all' }) },
      ...app.searches.map((x) => ({
        id: 's' + x.id, label: x.name, icon: 'search' as IconName, group: 'Searches',
        hint: x.unread ? `${x.unread} unread` : undefined, run: go({ kind: 'search', id: x.id }),
      })),
      ...app.folders.map((f) => ({
        id: 'f' + f.id, label: f.name, icon: 'folder' as IconName, group: 'Folders',
        hint: f.unread ? `${f.unread} unread` : undefined, run: go({ kind: 'folder', id: f.id }),
      })),
      ...app.feeds.map((f) => ({
        id: 'd' + f.id, label: f.title || f.url, feedIcon: f.iconUrl, group: 'Feeds',
        hint: f.unread ? `${f.unread} unread` : undefined, run: go({ kind: 'feed', id: f.id }),
      })),
      { id: 'a-mark', label: `Mark all as read in ${scopeName}`, icon: 'checkAll', group: 'Actions', hint: '⇧A', run: () => app.markAllRead() },
      ...(app.article && (app.ai || app.article.entry.summary)
        ? [{ id: 'a-summary', label: 'Summary of this article', icon: 'summary' as IconName, group: 'Actions', hint: '⇧S', run: () => app.summarize() }]
        : []),
      ...(app.article
        ? [
            ...app.integrations.map((i) => ({ id: `send-${i.id}`, label: `Send to ${i.name}`, icon: 'send' as IconName, group: 'Actions', run: () => app.sendTo(i) })),
            ...(app.prefs.obsidian?.vault
              ? [{ id: 'send-obsidian', label: 'Send to Obsidian', icon: 'send' as IconName, group: 'Actions', run: () => app.sendToObsidian() }]
              : []),
          ]
        : []),
      { id: 'a-listen', label: `Listen to unread with audio in ${scopeName}`, icon: 'headphones', group: 'Actions', hint: '⇧L', run: () => app.listenToList() },
      { id: 'a-manage', label: 'Manage subscriptions', icon: 'rss', group: 'Actions', hint: 'g m', run: () => app.manage() },
      { id: 'a-add', label: 'Add feed…', icon: 'plus', group: 'Actions', hint: 'a', run: () => (app.addFeedOpen = true) },
      { id: 'a-sidebar', label: app.prefs.sidebar ? 'Hide feeds' : 'Show feeds', icon: 'sidebar', group: 'Actions', hint: '[', run: () => app.setPrefs({ sidebar: !app.prefs.sidebar }) },
      ...(app.source.kind === 'feed'
        ? [{
            id: 'a-full', icon: 'article' as IconName, group: 'Actions',
            label: app.feedById.get(app.source.id)?.fullText ? `Stop fetching full articles for ${app.title}` : `Fetch full articles for ${app.title}`,
            run: () => app.source.kind === 'feed' && app.setFeedFullText(app.source.id, !app.feedById.get(app.source.id)?.fullText),
          }]
        : []),
      ...(app.source.kind === 'feed'
        ? [{
            id: 'a-notify', icon: 'bell' as IconName, group: 'Actions',
            label: app.feedById.get(app.source.id)?.notify ? `Stop notifications for ${app.title}` : `Notify me about ${app.title}`,
            run: () => app.source.kind === 'feed' && app.setFeedNotify(app.source.id, !app.feedById.get(app.source.id)?.notify),
          }]
        : []),
      { id: 'a-focus', label: app.focusMode ? 'Leave focused reading' : 'Focused reading', icon: 'focus', group: 'Actions', hint: 'f', run: () => (app.focusMode = !app.focusMode) },
      { id: 'a-day', label: 'Switch to Day', icon: 'sun', group: 'Actions', run: () => app.setPrefs({ theme: 'day' }) },
      { id: 'a-night', label: 'Switch to Night desk', icon: 'sun', group: 'Actions', run: () => app.setPrefs({ theme: 'night' }) },
      { id: 'a-evening', label: 'Switch to Evening', icon: 'sun', group: 'Actions', run: () => app.setPrefs({ theme: 'evening' }) },
      { id: 'a-auto', label: 'Match system theme', icon: 'sun', group: 'Actions', run: () => app.setPrefs({ theme: 'auto' }) },
      { id: 'a-sun', label: 'Follow sunrise and sunset', icon: 'sun', group: 'Actions', run: () => app.useSunTheme() },
      { id: 'a-keys', label: 'Keyboard shortcuts', icon: 'keyboard', group: 'Actions', hint: '?', run: () => (app.shortcutsOpen = true) },
      { id: 'a-settings', label: 'Settings', icon: 'settings', group: 'Actions', run: () => app.openSettings() },
      ...(app.role === 'admin'
        ? [{ id: 'a-admin', label: 'Admin: accounts, server, parsers', icon: 'settings' as const, group: 'Actions', run: () => app.openAdmin() }]
        : []),
    ]
    return out
  })

  function score(label: string, query: string): number {
    const l = label.toLowerCase()
    const s = query.toLowerCase().trim()
    if (!s) return 1
    if (l.startsWith(s)) return 100 - l.length / 100
    const idx = l.indexOf(s)
    if (idx >= 0) return 60 - idx
    let i = 0
    let gaps = 0
    for (const ch of l) {
      if (ch === s[i]) i++
      else if (i > 0) gaps++
      if (i === s.length) return 20 - gaps / 10
    }
    return -1
  }

  const results = $derived.by(() => {
    const ranked = items
      .map((it) => ({ it, s: score(it.label, q) }))
      .filter((r) => r.s > 0)
    if (q.trim()) ranked.sort((a, b) => b.s - a.s)
    const term = q.trim()
    const list = ranked.slice(0, term ? (found.length ? 5 : 12) : 40).map((r) => r.it)
    if (term) {
      for (const e of found) {
        const feed = app.feedById.get(e.feedId)
        list.push({
          id: 'e' + e.id, label: e.title || 'Untitled', hint: feed?.title, feedIcon: feed?.iconUrl ?? '', group: 'Articles',
          run: () => app.openFound(term, e.id),
        })
      }
      list.push({
        id: 'search', label: `All articles matching “${term}”`, icon: 'search', group: 'Articles',
        run: () => { app.query = term; app.go({ kind: 'view', view: 'all' }) },
      })
    }
    return list
  })

  $effect(() => {
    void q
    active = 0
  })

  $effect(() => {
    input?.focus()
  })

  function run(it: Item) {
    close()
    it.run()
  }

  function onkey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || (e.ctrlKey && e.key === 'n')) {
      active = Math.min(results.length - 1, active + 1)
      e.preventDefault()
    } else if (e.key === 'ArrowUp' || (e.ctrlKey && e.key === 'p')) {
      active = Math.max(0, active - 1)
      e.preventDefault()
    } else if (e.key === 'Enter' && results[active]) {
      e.preventDefault()
      run(results[active])
    }
  }

  $effect(() => {
    document.getElementById('pal-' + active)?.scrollIntoView({ block: 'nearest' })
  })
</script>

<Modal label="Command palette" onclose={close} width={600} align="top">
  <div class="field-row">
    <Icon name="search" />
    <input
      bind:this={input}
      bind:value={q}
      onkeydown={onkey}
      placeholder="Search articles, jump to a feed, or run a command"
      role="combobox"
      aria-expanded="true"
      aria-controls="palette-list"
      aria-activedescendant={results[active] ? 'pal-' + active : undefined}
      autocomplete="off"
      spellcheck="false"
    />
    <kbd>esc</kbd>
  </div>
  <ul id="palette-list" role="listbox" aria-label="Results">
    {#each results as it, i (it.id)}
      {#if i === 0 || results[i - 1].group !== it.group}
        <li class="group" role="presentation">{it.group}</li>
      {/if}
      <li
        id={'pal-' + i}
        role="option"
        aria-selected={i === active}
        class="item"
        onmousemove={() => (active = i)}
        onclick={() => run(it)}
        onkeydown={() => {}}
      >
        <span class="ic">
          {#if it.feedIcon !== undefined}
            <FeedIcon url={it.feedIcon} title={it.label} size={16} />
          {:else if it.icon}
            <Icon name={it.icon} size={16} />
          {/if}
        </span>
        <span class="label">{it.label}</span>
        {#if it.hint}<span class="hint">{it.hint}</span>{/if}
      </li>
    {/each}
    {#if results.length === 0}
      <li class="none">No matches.</li>
    {/if}
  </ul>
</Modal>

<style>
  .field-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0 14px;
    height: 54px;
    border-bottom: 1px solid var(--rule);
    color: var(--slate);
  }
  input {
    flex: 1;
    height: 100%;
    border: 0;
    background: none;
    font-size: calc(16px * var(--ui-scale));
    color: var(--ink);
    outline: none;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 6px;
    max-height: min(420px, 60dvh);
    overflow-y: auto;
  }
  .group {
    padding: 10px 10px 4px;
    font-size: calc(12px * var(--ui-scale));
    font-weight: 560;
    color: var(--slate);
  }
  .item {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 36px;
    padding: 0 10px;
    border-radius: var(--radius-s);
    cursor: pointer;
  }
  .item[aria-selected='true'] {
    background: var(--accent-wash-strong);
  }
  .ic {
    display: grid;
    place-items: center;
    width: 18px;
    color: var(--slate);
  }
  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hint {
    font-size: calc(12px * var(--ui-scale));
    color: var(--slate);
  }
  .none {
    padding: 18px 10px;
    color: var(--slate);
  }
</style>
