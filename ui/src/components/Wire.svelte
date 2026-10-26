<script lang="ts">
  // The wire: articles hang off a vertical time spine. State lives on the
  // spine itself: a filled dot is unread, a hollow ring is read, and a seal
  // in the gutter marks a star. Titles also change weight, so state never
  // relies on colour alone.
  import { app } from '../lib/state.svelte'
  import { arrivedAt, dayKey, dayLabel, gutterTime } from '../lib/time'
  import { api, type Entry } from '../lib/api'
  import Icon from './Icon.svelte'
  import FeedIcon from './FeedIcon.svelte'

  let scroller: HTMLElement | undefined = $state()
  let sentinel: HTMLElement | undefined = $state()

  type Group = { key: number; label: string; entries: Entry[] }
  const groups = $derived.by(() => {
    const out: Group[] = []
    const now = new Date()
    for (const e of app.entries) {
      const d = arrivedAt(e.id)
      const k = dayKey(d)
      if (!out.length || out[out.length - 1].key !== k) out.push({ key: k, label: dayLabel(d, now), entries: [] })
      out[out.length - 1].entries.push(e)
    }
    return out
  })

  const unreadHere = $derived(app.entries.filter((e) => !e.read).length)
  const feedsHere = $derived(new Set(app.entries.filter((e) => !e.read).map((e) => e.feedId)).size)
  const currentFeed = $derived(app.source.kind === 'feed' ? app.feedById.get(app.source.id) : undefined)
  const canMarkAll = $derived(!(app.source.kind === 'view' && app.source.view === 'starred'))
  const showUnreadToggle = $derived(app.source.kind !== 'view')

  // Infinite scroll: load the next page as the sentinel nears the viewport.
  $effect(() => {
    if (!sentinel || !scroller) return
    const io = new IntersectionObserver((es) => es[0].isIntersecting && app.loadMore(), {
      root: scroller,
      rootMargin: '600px',
    })
    io.observe(sentinel)
    return () => io.disconnect()
  })

  // Keep the selected row in view during keyboard navigation.
  $effect(() => {
    const id = app.selectedId
    if (!id || !scroller) return
    scroller.querySelector<HTMLElement>(`[data-id="${id}"]`)?.scrollIntoView({ block: 'nearest' })
  })

  let retrying = $state(false)
  async function retry(id: number) {
    retrying = true
    try {
      await api.refreshFeed(id)
      await app.refreshTree()
      await app.load()
    } finally {
      retrying = false
    }
  }

  function feedTitle(e: Entry) {
    return app.feedById.get(e.feedId)?.title ?? ''
  }
</script>

<section class="wire" aria-label="Articles">
  <header class="head">
    <button class="icon-btn only-narrow" aria-label="Show feeds" onclick={() => app.back()}>
      <Icon name="back" />
    </button>
    <button class="icon-btn only-medium" aria-label="Show feeds" onclick={() => (app.navOpen = true)}>
      <Icon name="menu" />
    </button>
    <div class="titles">
      <h1>{app.title}</h1>
      <p class="sub">
        {#if app.loading && !app.entries.length}
          Loading…
        {:else if app.unreadFilter || app.source.kind === 'view'}
          {unreadHere} unread{#if feedsHere > 1}&nbsp;across {feedsHere} feeds{/if}{#if app.cursor}&nbsp;so far{/if}
        {:else}
          {app.entries.length}{app.cursor ? '+' : ''} articles
        {/if}
      </p>
    </div>
    <div class="tools">
      {#if showUnreadToggle}
        <div class="seg" role="group" aria-label="Show">
          <button aria-pressed={app.prefs.unreadOnly} onclick={() => { app.setPrefs({ unreadOnly: true }); app.load() }}>Unread</button>
          <button aria-pressed={!app.prefs.unreadOnly} onclick={() => { app.setPrefs({ unreadOnly: false }); app.load() }}>All</button>
        </div>
      {/if}
      {#if canMarkAll}
        <button class="icon-btn" aria-label="Mark all read" title="Mark all read (Shift+A)" onclick={() => app.markAllRead()}>
          <Icon name="checkAll" />
        </button>
      {/if}
    </div>
  </header>

  {#if currentFeed && currentFeed.errorCount > 0}
    <div class="notice" role="status">
      <Icon name="alert" size={16} />
      <div>
        <p>Couldn't update this feed. {currentFeed.lastError}</p>
        <button class="link" onclick={() => retry(currentFeed.id)}>
          {retrying ? 'Retrying…' : 'Retry now'}
        </button>
      </div>
    </div>
  {/if}

  {#if app.pending > 0}
    <button class="pending" onclick={() => app.showPending()}>
      {app.pending} new {app.pending === 1 ? 'article' : 'articles'}
    </button>
  {/if}

  <div class="wire-scroll" bind:this={scroller}>
    {#if app.loadError}
      <div class="state">
        <p>Couldn't load articles. {app.loadError}</p>
        <button class="btn" onclick={() => app.load()}>Try again</button>
      </div>
    {:else if !app.loading && app.entries.length === 0}
      <div class="state">
        <span class="spine-end" aria-hidden="true"></span>
        {#if app.query}
          <h2>No articles match “{app.query}”.</h2>
          <button class="btn" onclick={() => { app.query = ''; app.load() }}>Clear search</button>
        {:else if app.unreadFilter}
          <h2>All caught up.</h2>
          <p>New articles will appear here as your feeds update.</p>
          <button class="btn" onclick={() => app.go({ kind: 'view', view: 'today' })}>Show today's articles</button>
        {:else if app.source.kind === 'view' && app.source.view === 'starred'}
          <h2>Nothing starred yet.</h2>
          <p>Press <kbd>s</kbd> on an article to keep it here.</p>
        {:else}
          <h2>No articles yet.</h2>
          <p>This feed hasn't published anything Correspondent has seen.</p>
        {/if}
      </div>
    {:else}
      <ol class="days">
        {#each groups as g (g.key)}
          <li class="day">
            <h2 class="day-label"><span>{g.label}</span></h2>
            <ol class="rows">
              {#each g.entries as e (e.id)}
                {@const selected = e.id === app.selectedId}
                <li
                  class="row"
                  class:read={e.read}
                  class:selected
                  data-id={e.id}
                >
                  <button
                    class="hit"
                    aria-current={selected ? 'true' : undefined}
                    onclick={() => app.select(e.id)}
                  >
                    <span class="gutter">
                      <time class="clock" datetime={new Date(e.publishedAt * 1000).toISOString()}>{gutterTime(e.publishedAt, arrivedAt(e.id))}</time>
                      {#if e.starred}<span class="seal" aria-label="Starred"></span>{/if}
                    </span>
                    <span class="dot" aria-hidden="true"></span>
                    <span class="body">
                      <span class="meta">
                        <FeedIcon url={app.feedById.get(e.feedId)?.iconUrl ?? ''} title={feedTitle(e)} size={14} />
                        <span class="feed">{feedTitle(e)}</span>
                        <span class="visually-hidden">{e.read ? 'Read' : 'Unread'}</span>
                      </span>
                      <span class="title">{e.title || 'Untitled'}</span>
                      {#if e.excerpt}<span class="excerpt">{e.excerpt}</span>{/if}
                    </span>
                    {#if e.imageUrl}
                      <img class="thumb" src={e.imageUrl} alt="" loading="lazy" referrerpolicy="no-referrer"
                        onerror={(ev) => ((ev.currentTarget as HTMLElement).style.display = 'none')} />
                    {/if}
                  </button>
                </li>
              {/each}
            </ol>
          </li>
        {/each}
      </ol>
      <div class="tail" bind:this={sentinel}>
        {#if app.loading}Loading…{:else if !app.cursor}<span class="spine-end" aria-hidden="true"></span>{/if}
      </div>
    {/if}
  </div>
</section>

<style>
  .wire {
    --gutter: 52px;
    --spine-x: calc(var(--gutter) + 9px);
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--sheet);
    position: relative;
  }

  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 14px 12px 12px 18px;
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
    letter-spacing: -0.012em;
    line-height: 1.15;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    margin: 2px 0 0;
    color: var(--slate);
    font-size: 12.5px;
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .seg {
    display: inline-flex;
    padding: 2px;
    border-radius: var(--radius-s);
    background: var(--fog);
  }
  .seg button {
    height: 26px;
    padding: 0 9px;
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

  .notice {
    display: flex;
    gap: 10px;
    margin: 12px 14px 0;
    padding: 10px 12px;
    border-radius: var(--radius-s);
    background: var(--fog);
    color: var(--slate);
    font-size: 13px;
  }
  .notice p {
    margin: 0 0 4px;
    color: var(--ink);
  }
  .link {
    color: var(--accent);
    font-weight: 500;
  }

  .pending {
    position: absolute;
    top: 76px;
    left: 50%;
    z-index: 3;
    transform: translateX(-50%);
    height: 30px;
    padding: 0 14px;
    border-radius: 15px;
    background: var(--accent);
    color: var(--sheet);
    font-weight: 560;
    font-size: 12.5px;
    box-shadow: var(--overlay-shadow);
    animation: drop 220ms var(--ease);
  }
  @keyframes drop {
    from {
      transform: translate(-50%, -8px);
      opacity: 0;
    }
  }

  .wire-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    /* The spine runs the full height of the list. */
    background:
      linear-gradient(var(--rule), var(--rule)) no-repeat var(--spine-x) 0 / 1.5px 100%;
  }
  ol {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .day-label {
    position: sticky;
    top: 0;
    z-index: 2;
    margin: 0;
    padding: 14px 16px 6px calc(var(--spine-x) - 5px);
    font-size: 12.5px;
    font-weight: 600;
    color: var(--ink);
    background: linear-gradient(var(--sheet) 70%, transparent);
    display: flex;
    align-items: center;
    gap: 12px;
  }
  /* A crossbar on the spine marks each day. */
  .day-label::before {
    content: '';
    width: 12px;
    height: 1.5px;
    background: var(--ink);
    flex: none;
  }

  .row {
    position: relative;
    content-visibility: auto;
    contain-intrinsic-size: auto 104px;
  }
  .hit {
    display: grid;
    grid-template-columns: var(--gutter) 18px minmax(0, 1fr) auto;
    align-items: start;
    width: 100%;
    padding: var(--row-pad) 16px var(--row-pad) 0;
    text-align: left;
    border-radius: 0;
    transition: background var(--fast) var(--ease);
  }
  .hit:hover {
    background: var(--hover);
  }
  .hit:focus-visible {
    outline-offset: -2px;
  }
  .selected .hit {
    background: var(--accent-wash);
  }
  /* The selected row's stretch of spine turns accent. */
  .selected::before {
    content: '';
    position: absolute;
    left: calc(var(--spine-x) - 0.75px);
    top: 0;
    bottom: 0;
    width: 3px;
    background: var(--accent);
    border-radius: 2px;
  }

  .gutter {
    position: relative;
    display: flex;
    justify-content: flex-end;
    padding: 3px 10px 0 4px;
    text-align: right;
    line-height: 1.2;
  }
  .clock {
    font-size: 11.5px;
    color: var(--slate);
    white-space: nowrap;
  }
  .seal {
    position: absolute;
    right: -4px;
    top: 20px;
    width: 7px;
    height: 7px;
    background: var(--seal);
    transform: rotate(45deg);
    border-radius: 1px;
  }

  .dot {
    position: relative;
    z-index: 1;
    justify-self: center;
    margin-top: 6px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 0 3px var(--sheet);
    transition: background var(--fast) var(--ease);
  }
  .read .dot {
    background: var(--sheet);
    border: 1.5px solid var(--ring);
  }
  .selected .dot {
    box-shadow: 0 0 0 3px color-mix(in oklab, var(--accent) 8%, var(--sheet));
  }
  .selected.read .dot {
    border-color: var(--accent);
  }

  .body {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    padding-left: 8px;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
    color: var(--slate);
    min-width: 0;
  }
  .feed {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .title {
    font-family: var(--font-read);
    font-size: 17px;
    line-height: 1.3;
    font-weight: 620;
    font-variation-settings: 'opsz' 18;
    letter-spacing: -0.003em;
    color: var(--ink);
    overflow-wrap: anywhere;
  }
  .read .title {
    font-weight: 420;
    color: color-mix(in oklab, var(--ink) 78%, var(--sheet));
  }
  .excerpt {
    font-size: 13px;
    line-height: 1.45;
    color: var(--slate);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  :global([data-density='compact']) .excerpt {
    -webkit-line-clamp: 1;
    line-clamp: 1;
  }
  .thumb {
    width: 64px;
    height: 64px;
    margin: 3px 0 0 12px;
    object-fit: cover;
    border-radius: var(--radius-s);
    background: var(--fog);
  }
  :global([data-density='compact']) .thumb {
    width: 44px;
    height: 44px;
  }
  .read .thumb {
    opacity: 0.8;
  }

  .tail {
    min-height: 64px;
    display: flex;
    align-items: center;
    padding-left: calc(var(--spine-x) - 5px);
    color: var(--slate);
    font-size: 12.5px;
    background: var(--sheet);
  }
  /* Where the list ends, the spine ends in a short terminal bar. */
  .spine-end {
    display: block;
    width: 12px;
    height: 1.5px;
    background: var(--ring);
  }

  .state {
    position: relative;
    padding: 56px 24px 24px calc(var(--spine-x) + 26px);
    background: var(--sheet);
    min-height: 100%;
  }
  .state .spine-end {
    position: absolute;
    left: calc(var(--spine-x) - 5px);
    top: 66px;
  }
  .state h2 {
    margin: 0 0 6px;
    font-family: var(--font-read);
    font-size: 22px;
    font-weight: 520;
  }
  .state p {
    margin: 0 0 16px;
    color: var(--slate);
    max-width: 34ch;
  }

  .only-narrow,
  .only-medium {
    display: none;
  }
  @media (max-width: 1099px) and (min-width: 700px) {
    .only-medium {
      display: inline-grid;
    }
  }
  @media (max-width: 699px) {
    .only-narrow {
      display: inline-grid;
    }
    .wire {
      --gutter: 50px;
    }
    .head {
      padding-left: 8px;
    }
    .thumb {
      width: 56px;
      height: 56px;
    }
  }
</style>
