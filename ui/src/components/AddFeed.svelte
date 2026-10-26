<script lang="ts">
  import { app } from '../lib/state.svelte'
  import { api, type Candidate } from '../lib/api'
  import { relative } from '../lib/time'
  import Modal from './Modal.svelte'
  import FeedIcon from './FeedIcon.svelte'

  let url = $state('')
  let busy = $state(false)
  let error = $state('')
  let candidates = $state<Candidate[]>([])
  let searched = $state(false)
  let asking = $state(false)
  let chosen = $state(0)
  let name = $state('')
  let folder = $state<string>(app.source.kind === 'folder' ? String(app.source.id) : '')
  let newFolder = $state('')

  const close = () => (app.addFeedOpen = false)

  async function find(e: SubmitEvent) {
    e.preventDefault()
    if (!url.trim()) return
    busy = true
    error = ''
    candidates = []
    searched = false
    try {
      candidates = (await api.discover(url.trim())).candidates
      chosen = 0
      name = candidates[0]?.title ?? ''
      searched = true
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }

  // Ask the reader's AI to write a parser for the page. rebuild replaces a
  // parser that read the page poorly.
  async function askAI(rebuild = false) {
    busy = asking = true
    error = ''
    try {
      candidates = (await api.discoverAI(url.trim(), rebuild)).candidates
      chosen = 0
      name = candidates[0]?.title ?? ''
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = asking = false
    }
  }

  const methods: Record<string, string> = {
    hfeed: 'from the page’s microformats',
    jsonld: 'from the page’s structured data',
    links: 'from the page’s links',
    css: 'with a parser written by AI',
  }

  async function subscribe() {
    const c = candidates[chosen]
    if (!c) return
    busy = true
    error = ''
    try {
      const folderId = folder && folder !== 'new' ? Number(folder) : null
      const { id } = await api.subscribe(c.url, name.trim() || c.title, folderId, folder === 'new' ? newFolder.trim() : undefined, !!c.page)
      await app.refreshTree()
      close()
      app.go({ kind: 'feed', id })
      app.toast(`Subscribed to ${name.trim() || c.title}.`)
    } catch (err) {
      error = (err as Error).message
      busy = false
    }
  }
</script>

<Modal label="Add feed" onclose={close} width={560}>
  <div class="wrap">
    <h2>Add a feed</h2>
    <form onsubmit={find}>
      <label for="feed-url">Site or feed address</label>
      <div class="row">
        <input id="feed-url" class="field" type="url" inputmode="url" placeholder="example.com or https://example.com/feed.xml"
          bind:value={url} required autocomplete="off" />
        <button class="btn" disabled={busy}>{busy && !candidates.length ? 'Looking…' : 'Find feeds'}</button>
      </div>
    </form>

    {#if error}<p class="error" role="alert">{error}</p>{/if}

    {#if searched && !candidates.length && !asking}
      <div class="nofeed">
        <p><strong>This site has no feed</strong>, and its articles couldn’t be found on the page.</p>
        {#if app.ai}
          <p>Your AI can write a parser for it, once. Everyone on this server who follows the page then shares it.</p>
          <button class="btn primary" disabled={busy} onclick={() => askAI()}>Find the articles with AI</button>
        {:else if app.aiAvailable}
          <p>Set up AI in Settings and it can write a parser for pages like this one.</p>
          <button class="btn" onclick={() => { close(); app.openSettings('feeds') }}>Open Settings</button>
        {/if}
      </div>
    {/if}
    {#if asking}<p class="asking" role="status">Asking the AI to read the page. This can take a minute…</p>{/if}

    {#if candidates.length}
      <fieldset class="found">
        <legend>{candidates.length === 1 ? 'Found one feed' : `Found ${candidates.length} feeds`}</legend>
        {#each candidates as c, i (c.url)}
          <label class="cand" class:on={chosen === i}>
            <input type="radio" name="cand" value={i} bind:group={chosen} onchange={() => (name = c.title)} />
            <FeedIcon url={c.iconUrl} title={c.title} size={20} />
            <span class="cand-body">
              <span class="cand-title">{c.title || c.url}</span>
              {#if c.description}<span class="cand-desc">{c.description}</span>{/if}
              <span class="cand-meta">
                {c.itemCount} recent {c.itemCount === 1 ? 'article' : 'articles'}{#if c.latestAt}, latest {relative(c.latestAt)}{/if}
              </span>
              {#if c.page}
                <span class="cand-page">
                  No feed: read {methods[c.method ?? ''] ?? 'from the page'}{#if c.shared}, already set up on this server{/if}.
                  Full articles are fetched from each link.
                </span>
                {#if c.sample?.length}
                  <ul class="sample">{#each c.sample as t, j (j)}<li>{t}</li>{/each}</ul>
                {/if}
              {/if}
            </span>
          </label>
        {/each}
      </fieldset>

      {#if candidates[chosen]?.page && candidates[chosen]?.method !== 'css' && app.ai}
        <p class="retry">Not the right articles? <button class="link" disabled={busy} onclick={() => askAI(true)}>Ask the AI instead</button></p>
      {/if}

      <div class="grid">
        <label>
          <span>Name</span>
          <input class="field" bind:value={name} />
        </label>
        <label>
          <span>Folder</span>
          <select class="field" bind:value={folder}>
            <option value="">No folder</option>
            {#each app.folders as f (f.id)}<option value={String(f.id)}>{f.name}</option>{/each}
            <option value="new">New folder…</option>
          </select>
        </label>
        {#if folder === 'new'}
          <label class="wide">
            <span>New folder name</span>
            <input class="field" bind:value={newFolder} />
          </label>
        {/if}
      </div>
    {/if}

    <div class="actions">
      <button class="btn" onclick={close}>Cancel</button>
      <button class="btn primary" disabled={!candidates.length || busy || (folder === 'new' && !newFolder.trim())} onclick={subscribe}>
        {busy && candidates.length ? 'Subscribing…' : 'Subscribe'}
      </button>
    </div>
  </div>
</Modal>

<style>
  .nofeed {
    margin: 16px 0 0;
    padding: 14px 16px;
    border-radius: var(--radius-l);
    background: var(--fog);
  }
  .nofeed p {
    margin: 0 0 10px;
  }
  .asking,
  .retry {
    margin: 14px 0 0;
    color: var(--slate);
    font-size: calc(13px * var(--ui-scale));
  }
  .link {
    color: var(--accent);
    font-weight: 500;
  }
  .cand-page {
    display: block;
    margin-top: 4px;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  .sample {
    margin: 6px 0 0;
    padding-left: 18px;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--ink);
  }
  .sample li {
    margin: 2px 0;
  }
  .wrap {
    padding: 22px 24px 20px;
    overflow-y: auto;
    max-height: inherit;
  }
  h2 {
    margin: 0 0 16px;
    font-family: var(--font-read);
    font-size: calc(22px * var(--ui-scale));
    font-weight: 540;
  }
  label > span,
  form > label,
  legend {
    display: block;
    margin-bottom: 6px;
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 560;
  }
  .row {
    display: flex;
    gap: 8px;
  }
  .row .btn {
    height: 36px;
  }
  .error {
    margin: 12px 0 0;
    color: var(--danger);
  }
  .found {
    border: 0;
    padding: 0;
    margin: 20px 0 0;
    display: grid;
    gap: 6px;
  }
  .cand {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 12px;
    border-radius: var(--radius-s);
    border: 1px solid var(--rule);
    cursor: pointer;
  }
  .cand.on {
    border-color: var(--accent);
    background: var(--accent-wash);
  }
  .cand input {
    margin-top: 3px;
    accent-color: var(--accent);
  }
  .cand-body {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .cand-title {
    font-family: var(--font-read);
    font-size: calc(17px * var(--ui-scale));
    font-weight: 560;
  }
  .cand-desc {
    color: var(--slate);
    font-size: calc(13px * var(--ui-scale));
  }
  .cand-meta {
    color: var(--slate);
    font-size: calc(12.5px * var(--ui-scale));
  }
  .grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
    margin-top: 16px;
  }
  .wide {
    grid-column: 1 / -1;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 22px;
  }
  @media (max-width: 520px) {
    .grid {
      grid-template-columns: 1fr;
    }
    .row {
      flex-direction: column;
    }
  }
</style>
