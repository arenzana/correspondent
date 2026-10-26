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
    try {
      candidates = (await api.discover(url.trim())).candidates
      chosen = 0
      name = candidates[0]?.title ?? ''
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }

  async function subscribe() {
    const c = candidates[chosen]
    if (!c) return
    busy = true
    error = ''
    try {
      const folderId = folder && folder !== 'new' ? Number(folder) : null
      const { id } = await api.subscribe(c.url, name.trim() || c.title, folderId, folder === 'new' ? newFolder.trim() : undefined)
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
            </span>
          </label>
        {/each}
      </fieldset>

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
  .wrap {
    padding: 22px 24px 20px;
    overflow-y: auto;
    max-height: inherit;
  }
  h2 {
    margin: 0 0 16px;
    font-family: var(--font-read);
    font-size: 22px;
    font-weight: 540;
  }
  label > span,
  form > label,
  legend {
    display: block;
    margin-bottom: 6px;
    font-size: 12.5px;
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
    font-size: 17px;
    font-weight: 560;
  }
  .cand-desc {
    color: var(--slate);
    font-size: 13px;
  }
  .cand-meta {
    color: var(--slate);
    font-size: 12.5px;
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
