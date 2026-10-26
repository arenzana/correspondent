<script lang="ts">
  // Subscription management: every feed with its health, grouped by folder,
  // with rename, move, fix address, pause, notify and delete, singly or in bulk.
  import { app } from '../lib/state.svelte'
  import type { Feed, Folder } from '../lib/api'
  import { relative } from '../lib/time'
  import Icon from './Icon.svelte'
  import FeedIcon from './FeedIcon.svelte'
  import Pulse from './Pulse.svelte'
  import Modal from './Modal.svelte'

  type Filter = 'all' | 'failing' | 'paused' | 'quiet'
  const QUIET_DAYS = 90

  let query = $state('')
  let filter = $state<Filter>('all')
  let selected = $state<Set<number>>(new Set())
  let editing = $state<number | null>(null)
  let editingFolder = $state<number | null>(null)
  let confirmDelete = $state<number[] | null>(null)
  let addressFor = $state<Feed | null>(null)
  let address = $state('')
  let addressError = $state('')
  let newFolder = $state('')
  let bulkFolder = $state<string | null>(null) // name being typed for "Move to a new folder"

  const now = Date.now() / 1000
  const quiet = (f: Feed) => !f.latestAt || now - f.latestAt > QUIET_DAYS * 86_400
  const failing = (f: Feed) => !f.paused && f.errorCount > 0
  const counts = $derived({
    all: app.feeds.length,
    failing: app.feeds.filter(failing).length,
    paused: app.feeds.filter((f) => f.paused).length,
    quiet: app.feeds.filter(quiet).length,
  })

  const visible = $derived(
    app.feeds.filter((f) => {
      if (filter === 'failing' && !failing(f)) return false
      if (filter === 'paused' && !f.paused) return false
      if (filter === 'quiet' && !quiet(f)) return false
      const q = query.trim().toLowerCase()
      return !q || `${f.title} ${f.url} ${f.siteUrl}`.toLowerCase().includes(q)
    }),
  )

  // Folders in sidebar order, then feeds outside any folder.
  const groups = $derived.by(() => {
    const out: { folder: Folder | null; feeds: Feed[] }[] = app.folders.map((folder) => ({
      folder,
      feeds: visible.filter((f) => f.folderId === folder.id),
    }))
    out.push({ folder: null, feeds: visible.filter((f) => f.folderId === null) })
    // While searching or filtering, hide groups with nothing to show.
    return filter === 'all' && !query.trim() ? out.filter((g) => g.folder || g.feeds.length) : out.filter((g) => g.feeds.length)
  })

  const perWeek = (f: Feed) => {
    const n = f.cadence.reduce((a, b) => a + b, 0) / 2
    return n === 0 ? 'none in 2 weeks' : n < 1 ? 'about 1 a fortnight' : `${Math.round(n)} a week`
  }

  function status(f: Feed): { text: string; tone: 'ok' | 'muted' | 'bad' } {
    if (f.paused) return { text: 'Paused', tone: 'muted' }
    if (f.errorCount > 0) return { text: `Failing: ${f.lastError}`, tone: 'bad' }
    if (!f.lastFetchAt) return { text: 'Not checked yet', tone: 'muted' }
    return { text: `Checked ${relative(f.lastFetchAt)}`, tone: 'ok' }
  }

  function toggle(id: number) {
    const s = new Set(selected)
    if (s.has(id)) s.delete(id)
    else s.add(id)
    selected = s
  }
  function selectAll(feeds: Feed[], on: boolean) {
    const s = new Set(selected)
    for (const f of feeds) on ? s.add(f.id) : s.delete(f.id)
    selected = s
  }
  const selectedIDs = $derived([...selected].filter((id) => app.feeds.some((f) => f.id === id)))

  const deleteSummary = $derived.by(() => {
    if (!confirmDelete) return null
    const feeds = app.feeds.filter((f) => confirmDelete!.includes(f.id))
    return {
      title: feeds.length === 1 ? `Delete ${feeds[0].title}?` : `Delete ${feeds.length} feeds?`,
      articles: feeds.reduce((a, f) => a + f.total, 0),
      starred: feeds.reduce((a, f) => a + f.starred, 0),
    }
  })

  async function doDelete() {
    const ids = confirmDelete!
    confirmDelete = null
    await app.deleteFeeds(ids)
    selected = new Set([...selected].filter((id) => !ids.includes(id)))
  }

  async function saveAddress(e: SubmitEvent) {
    e.preventDefault()
    addressError = await app.setFeedURL(addressFor!.id, address)
    if (!addressError) addressFor = null
  }

  function moveSelected(value: string) {
    if (!value) return
    if (value === 'new') {
      bulkFolder = ''
      return
    }
    void app.moveFeeds(selectedIDs, value === 'none' ? null : Number(value))
  }

  function onRenameKey(e: KeyboardEvent, save: (v: string) => void) {
    if (e.key === 'Enter') save((e.currentTarget as HTMLInputElement).value)
    if (e.key === 'Escape') {
      editing = null
      editingFolder = null
    }
  }
</script>

<section class="manage" aria-labelledby="manage-title">
  <header class="head">
    <button class="icon-btn only-narrow" aria-label="Show feeds" onclick={() => app.back()}>
      <Icon name="back" />
    </button>
    <div class="titles">
      <h1 id="manage-title">Subscriptions</h1>
      <p class="sub">
        {app.feeds.length} feeds in {app.folders.length} folders{#if counts.failing}, {counts.failing} failing{/if}
      </p>
    </div>
    <button class="btn" onclick={() => (app.addFeedOpen = true)}><Icon name="plus" size={16} />Add feed</button>
  </header>

  <div class="toolbar">
    <label class="search">
      <Icon name="search" size={15} />
      <input class="field" bind:value={query} placeholder="Find a feed by name or address" aria-label="Find a feed" />
    </label>
    <div class="seg" role="group" aria-label="Show">
      {#each [['all', 'All'], ['failing', 'Failing'], ['paused', 'Paused'], ['quiet', 'Quiet']] as [id, label] (id)}
        <button aria-pressed={filter === id} onclick={() => (filter = id as Filter)}
          title={id === 'quiet' ? `Nothing published in ${QUIET_DAYS} days` : undefined}>
          {label}<span class="n">{counts[id as Filter]}</span>
        </button>
      {/each}
    </div>
    <form class="new-folder" onsubmit={(e) => { e.preventDefault(); app.createFolder(newFolder); newFolder = '' }}>
      <input class="field" bind:value={newFolder} placeholder="New folder" aria-label="New folder name" />
      <button class="btn" disabled={!newFolder.trim()}>Create</button>
    </form>
  </div>

  <div class="scroll">
    {#each groups as g (g.folder?.id ?? 'none')}
      <div class="group">
        <div class="group-head">
          <input type="checkbox" aria-label="Select all in {g.folder?.name ?? 'No folder'}"
            checked={g.feeds.length > 0 && g.feeds.every((f) => selected.has(f.id))}
            disabled={!g.feeds.length}
            onchange={(e) => selectAll(g.feeds, e.currentTarget.checked)} />
          {#if g.folder && editingFolder === g.folder.id}
            <!-- svelte-ignore a11y_autofocus -->
            <input class="field rename" value={g.folder.name} autofocus aria-label="Folder name"
              onkeydown={(e) => onRenameKey(e, (v) => { app.renameFolder(g.folder!.id, v); editingFolder = null })}
              onblur={(e) => { app.renameFolder(g.folder!.id, e.currentTarget.value); editingFolder = null }} />
          {:else}
            <h2>{g.folder?.name ?? 'No folder'}</h2>
            <span class="count">{g.feeds.length}</span>
          {/if}
          {#if g.folder}
            <span class="spacer"></span>
            <button class="icon-btn" aria-label="Rename {g.folder.name}" title="Rename folder" onclick={() => (editingFolder = g.folder!.id)}>
              <Icon name="pencil" size={16} />
            </button>
            <button class="icon-btn" aria-label="Delete folder {g.folder.name}" title="Delete folder (its feeds are kept)"
              onclick={() => app.deleteFolder(g.folder!.id)}>
              <Icon name="trash" size={16} />
            </button>
          {/if}
        </div>

        {#if g.feeds.length === 0}
          <p class="empty">No feeds in this folder.</p>
        {/if}
        <ul>
          {#each g.feeds as f (f.id)}
            {@const st = status(f)}
            <li class="row" class:selected={selected.has(f.id)} class:paused={f.paused}>
              <input type="checkbox" aria-label="Select {f.title}" checked={selected.has(f.id)} onchange={() => toggle(f.id)} />
              <FeedIcon url={f.iconUrl} title={f.title || f.url} size={20} />
              <div class="who">
                {#if editing === f.id}
                  <!-- svelte-ignore a11y_autofocus -->
                  <input class="field rename" value={f.title} autofocus aria-label="Feed name"
                    onkeydown={(e) => onRenameKey(e, (v) => { app.renameFeed(f.id, v); editing = null })}
                    onblur={(e) => { app.renameFeed(f.id, e.currentTarget.value); editing = null }} />
                {:else}
                  <button class="name" title="Rename" onclick={() => (editing = f.id)}>{f.title || f.url}</button>
                {/if}
                <span class="url" title={f.url}>{f.url.replace(/^https?:\/\//, '')}</span>
              </div>
              <div class="health">
                <Pulse days={f.cadence} label={f.title} />
                <span class="rate">{perWeek(f)}</span>
                <span class="status {st.tone}" title={st.text}>{st.text}</span>
              </div>
              <div class="nums" title="{f.unread} unread of {f.total}">
                <strong>{f.unread}</strong><span>/ {f.total}</span>
              </div>
              <div class="actions">
                <button class="icon-btn" aria-pressed={f.notify} title={f.notify ? 'Notifications on' : 'Notify me about new articles'}
                  aria-label={f.notify ? `Turn off notifications for ${f.title}` : `Notify me about ${f.title}`}
                  onclick={() => app.setFeedNotify(f.id, !f.notify)}>
                  <Icon name={f.notify ? 'bell' : 'bellOff'} size={16} />
                </button>
                <button class="icon-btn" aria-pressed={f.fullText}
                  title={f.fullText ? 'Fetching full articles' : 'Fetch full articles from their pages'}
                  aria-label={f.fullText ? `Stop fetching full articles for ${f.title}` : `Fetch full articles for ${f.title}`}
                  onclick={() => app.setFeedFullText(f.id, !f.fullText)}>
                  <Icon name="article" size={16} />
                </button>
                <button class="icon-btn" title="Update now" aria-label="Update {f.title} now" onclick={() => app.refreshFeedNow(f.id)}>
                  <Icon name="refresh" size={16} />
                </button>
                <button class="icon-btn" title={f.paused ? 'Resume updates' : 'Pause updates'}
                  aria-label="{f.paused ? 'Resume' : 'Pause'} {f.title}" onclick={() => app.setPaused([f.id], !f.paused)}>
                  <Icon name={f.paused ? 'play' : 'pause'} size={16} />
                </button>
                <button class="icon-btn" title="Change address" aria-label="Change the address of {f.title}"
                  onclick={() => { addressFor = f; address = f.url; addressError = '' }}>
                  <Icon name="link" size={16} />
                </button>
                <button class="icon-btn danger" title="Delete" aria-label="Delete {f.title}" onclick={() => (confirmDelete = [f.id])}>
                  <Icon name="trash" size={16} />
                </button>
              </div>
            </li>
          {/each}
        </ul>
      </div>
    {/each}
    {#if visible.length === 0}
      <p class="none">
        {#if app.feeds.length === 0}You have no subscriptions yet.{:else}No feeds match.{/if}
      </p>
    {/if}
  </div>

  {#if selectedIDs.length}
    <div class="bulk" role="toolbar" aria-label="Selected feeds">
      <span><strong>{selectedIDs.length}</strong> selected</span>
      {#if bulkFolder !== null}
        <form class="inline" onsubmit={(e) => { e.preventDefault(); if (bulkFolder?.trim()) app.moveFeeds(selectedIDs, bulkFolder.trim()); bulkFolder = null }}>
          <!-- svelte-ignore a11y_autofocus -->
          <input class="field" bind:value={bulkFolder} placeholder="New folder name" aria-label="New folder name" autofocus
            onkeydown={(e) => e.key === 'Escape' && (bulkFolder = null)} />
          <button class="btn primary" disabled={!bulkFolder.trim()}>Move</button>
        </form>
      {:else}
        <select class="field" aria-label="Move selected feeds" onchange={(e) => { moveSelected(e.currentTarget.value); e.currentTarget.value = '' }}>
          <option value="">Move to…</option>
          {#each app.folders as folder (folder.id)}<option value={String(folder.id)}>{folder.name}</option>{/each}
          <option value="none">No folder</option>
          <option value="new">New folder…</option>
        </select>
      {/if}
      <button class="btn" onclick={() => app.setPaused(selectedIDs, true)}>Pause</button>
      <button class="btn" onclick={() => app.setPaused(selectedIDs, false)}>Resume</button>
      <button class="btn danger" onclick={() => (confirmDelete = selectedIDs)}>Delete</button>
      <button class="link" onclick={() => (selected = new Set())}>Clear</button>
    </div>
  {/if}
</section>

{#if confirmDelete && deleteSummary}
  <Modal label={deleteSummary.title} onclose={() => (confirmDelete = null)} width={460}>
    <div class="dialog">
      <h2>{deleteSummary.title}</h2>
      <p>
        This unsubscribes and removes {deleteSummary.articles === 1 ? 'its 1 article' : `${deleteSummary.articles} articles`}{#if deleteSummary.starred},
          including {deleteSummary.starred} starred{/if}. Reeder and other apps lose them too. This can't be undone.
      </p>
      <div class="buttons">
        <button class="btn" onclick={() => (confirmDelete = null)}>Cancel</button>
        <button class="btn danger-solid" onclick={doDelete}>{confirmDelete.length === 1 ? 'Delete feed' : 'Delete feeds'}</button>
      </div>
    </div>
  </Modal>
{/if}

{#if addressFor}
  <Modal label="Change address" onclose={() => (addressFor = null)} width={520}>
    <form class="dialog" onsubmit={saveAddress}>
      <h2>Change the address of {addressFor.title}</h2>
      <p>Use this when a site moves its feed. The feed keeps its articles and settings.</p>
      <input class="field" type="url" bind:value={address} required aria-label="Feed address" />
      {#if addressError}<p class="error" role="alert">{addressError}</p>{/if}
      <div class="buttons">
        <button type="button" class="btn" onclick={() => (addressFor = null)}>Cancel</button>
        <button class="btn primary" disabled={!address.trim() || address.trim() === addressFor.url}>Save address</button>
      </div>
    </form>
  </Modal>
{/if}

<style>
  .manage {
    position: relative;
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--sheet);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: calc(14px + env(safe-area-inset-top)) 18px 12px;
    border-bottom: 1px solid var(--rule);
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  h1 {
    margin: 0;
    font-family: var(--font-read);
    font-size: 23px;
    font-weight: 560;
    font-variation-settings: 'opsz' 36;
  }
  .sub {
    margin: 2px 0 0;
    color: var(--slate);
    font-size: 12.5px;
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding: 12px 18px;
    border-bottom: 1px solid var(--rule-soft);
  }
  .search {
    position: relative;
    flex: 1 1 240px;
    display: flex;
    align-items: center;
    color: var(--slate);
  }
  .search :global(svg) {
    position: absolute;
    left: 10px;
  }
  .search .field {
    padding-left: 32px;
  }
  .seg {
    display: inline-flex;
    padding: 2px;
    border-radius: var(--radius-s);
    background: var(--fog);
  }
  .seg button {
    height: 30px;
    padding: 0 10px;
    border-radius: 4px;
    font-size: 12.5px;
    font-weight: 500;
    color: var(--slate);
  }
  .seg button[aria-pressed='true'] {
    background: var(--sheet);
    color: var(--ink);
    box-shadow: 0 0 0 1px var(--rule);
  }
  .seg .n {
    margin-left: 5px;
    font-variant-numeric: tabular-nums;
    opacity: 0.7;
  }
  .new-folder {
    display: flex;
    gap: 6px;
  }
  .new-folder .field {
    width: 150px;
  }
  .new-folder .btn,
  .head .btn {
    height: 36px;
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 4px 0 calc(80px + env(safe-area-inset-bottom));
  }
  .group-head {
    position: sticky;
    top: 0;
    z-index: 1;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 18px 6px;
    background: linear-gradient(var(--sheet) 75%, transparent);
  }
  .group-head h2 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
  }
  .count {
    font-size: 12px;
    color: var(--slate);
  }
  .spacer {
    flex: 1;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .row {
    display: grid;
    grid-template-columns: auto auto minmax(0, 1.4fr) minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 12px;
    padding: 9px 18px;
    border-top: 1px solid var(--rule-soft);
  }
  .row:hover {
    background: var(--hover);
  }
  .row.selected {
    background: var(--accent-wash);
  }
  .row.paused .name {
    color: var(--slate);
  }
  input[type='checkbox'] {
    accent-color: var(--accent);
    width: 16px;
    height: 16px;
  }
  .who {
    display: grid;
    min-width: 0;
  }
  .name {
    justify-self: start;
    max-width: 100%;
    font-family: var(--font-read);
    font-size: 16px;
    font-weight: 560;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    border-radius: 3px;
  }
  .name:hover {
    text-decoration: underline;
    text-decoration-color: var(--rule);
    text-underline-offset: 3px;
  }
  .url {
    font-size: 12px;
    color: var(--slate);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rename {
    height: 30px;
    max-width: 360px;
  }
  .health {
    display: grid;
    grid-template-columns: auto 1fr;
    align-items: center;
    gap: 2px 10px;
    min-width: 0;
    font-size: 12px;
    color: var(--slate);
  }
  .status {
    grid-column: 1 / -1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .status.bad {
    color: var(--danger);
  }
  .nums {
    font-size: 12.5px;
    color: var(--slate);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .nums strong {
    color: var(--ink);
    margin-right: 3px;
  }
  .actions {
    display: flex;
    gap: 0;
  }
  .icon-btn.danger:hover {
    color: var(--danger);
  }
  .empty,
  .none {
    margin: 4px 18px 10px 44px;
    color: var(--slate);
    font-size: 13px;
  }
  .none {
    margin: 40px 18px;
    text-align: center;
  }

  .bulk {
    position: absolute;
    left: 50%;
    bottom: calc(16px + env(safe-area-inset-bottom));
    transform: translateX(-50%);
    z-index: 5;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    max-width: calc(100% - 32px);
    padding: 8px 10px 8px 14px;
    border: 1px solid var(--rule);
    border-radius: var(--radius-l);
    background: var(--sheet);
    box-shadow: var(--overlay-shadow);
    font-size: 13px;
  }
  .inline {
    display: flex;
    gap: 6px;
  }
  .inline .field {
    width: 160px;
    height: 32px;
  }
  .bulk select {
    width: auto;
    height: 32px;
  }
  .link {
    color: var(--accent);
    font-weight: 500;
    padding: 0 6px;
  }
  .btn.danger {
    color: var(--danger);
  }
  .btn.danger-solid {
    background: var(--danger);
    border-color: var(--danger);
    color: var(--sheet);
  }

  .dialog {
    padding: 22px 24px 20px;
    display: grid;
    gap: 12px;
  }
  .dialog h2 {
    margin: 0;
    font-family: var(--font-read);
    font-size: 21px;
    font-weight: 540;
  }
  .dialog p {
    margin: 0;
    color: var(--slate);
  }
  .error {
    color: var(--danger) !important;
  }
  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 6px;
  }
  .only-narrow {
    display: none;
  }

  @media (max-width: 899px) {
    .row {
      grid-template-columns: auto auto minmax(0, 1fr) auto;
      grid-template-areas:
        'check icon who nums'
        '. . health health'
        '. . actions actions';
      row-gap: 6px;
    }
    .row > input {
      grid-area: check;
    }
    .row > :global(.icon),
    .row > :global(.tile) {
      grid-area: icon;
    }
    .who {
      grid-area: who;
    }
    .nums {
      grid-area: nums;
    }
    .health {
      grid-area: health;
    }
    .actions {
      grid-area: actions;
      margin-left: -8px;
    }
  }
  @media (max-width: 699px) {
    .only-narrow {
      display: inline-grid;
    }
    .head {
      padding-left: 8px;
    }
    .new-folder {
      flex: 1 1 100%;
    }
    .new-folder .field {
      flex: 1;
    }
  }
</style>
