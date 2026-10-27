<script lang="ts">
  // The wire: articles hang off a vertical time spine. State lives on the
  // spine itself: a filled dot is unread, a hollow ring is read, and a seal
  // in the gutter marks a star. Titles also change weight, so state never
  // relies on colour alone.
  import { app } from '../lib/state.svelte'
  import { clock, dayKey, dayLabel } from '../lib/time'
  import { api, type Entry } from '../lib/api'
  import { swipe } from '../lib/swipe'
  import Icon from './Icon.svelte'
  import FeedIcon from './FeedIcon.svelte'

  let scroller: HTMLElement | undefined = $state()
  let sentinel: HTMLElement | undefined = $state()

  type Group = { key: number; label: string; entries: Entry[] }
  const groups = $derived.by(() => {
    const out: Group[] = []
    const now = new Date()
    // Grouped by the day each article was published, the order it is listed in.
    for (const e of app.entries) {
      const d = new Date(e.publishedAt * 1000)
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

  // A search typed in the search box (not yet saved), and a saved one.
  const adHoc = $derived(app.query.trim() && app.source.kind !== 'search' ? app.query.trim() : '')
  const saved = $derived(app.source.kind === 'search' ? app.searches.find((x) => app.source.kind === 'search' && x.id === app.source.id) : undefined)
  let renaming = $state(false)
  let draft = $state('')
  function startRename() {
    draft = saved?.name ?? ''
    renaming = true
  }
  function finishRename(keep: boolean) {
    if (keep && saved) void app.renameSearch(saved.id, draft)
    renaming = false
  }
  function focusSelect(el: HTMLInputElement) {
    el.focus()
    el.select()
  }

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

  // The list tells the app whether it sits at the top, where new articles
  // may slide in; further down it never moves under the reader.
  function onScroll() {
    const top = (scroller?.scrollTop ?? 0) < 24
    if (top !== app.atTop) app.atTop = top
  }

  // A touch on the list holds back arrivals briefly (see app.listBusyUntil).
  $effect(() => {
    if (!scroller) return
    const hold = () => (app.listBusyUntil = Date.now() + 1500)
    scroller.addEventListener('pointerdown', hold, { passive: true })
    return () => scroller?.removeEventListener('pointerdown', hold)
  })

  const reduced = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches

  // Rows that arrived live grow in from nothing, pushing the list down.
  function arrive(node: HTMLElement, id: string) {
    if (reduced || !app.arriving.has(id)) return
    const h = node.getBoundingClientRect().height
    node.animate(
      [
        { height: '0px', opacity: 0 },
        { height: `${h}px`, opacity: 0, offset: 0.55 },
        { height: `${h}px`, opacity: 1 },
      ],
      { duration: 620, easing: 'cubic-bezier(0.2, 0.7, 0.2, 1)' },
    )
  }

  // The selection cursor: one wash with its stretch of accent spine, moved
  // between rows rather than redrawn on each. Its leading edge travels
  // first and the trailing edge follows, so it stretches and settles.
  let days: HTMLElement | undefined = $state()
  let cursor = $state<{ top: number; bottom: number; dir: 'up' | 'down' | 'none' } | null>(null)
  function placeCursor() {
    const id = app.selectedId
    const row = id && days ? days.querySelector<HTMLElement>(`.row[data-id="${id}"]`) : null
    if (!row || !days) {
      cursor = null
      return
    }
    const base = days.getBoundingClientRect().top
    const r = row.getBoundingClientRect()
    const next = { top: r.top - base, bottom: r.bottom - base }
    const dir = !cursor ? 'none' : next.top > cursor.top ? 'down' : next.top < cursor.top ? 'up' : cursor.dir
    if (cursor && next.top !== cursor.top && dir !== 'none') wave(dir)
    cursor = { ...next, dir }
  }

  // As the cursor travels, its spine ripples like a plucked string: a sine
  // wave running the way it moves, pinned at both ends, swelling and then
  // settling straight as the row lands. Drawn in a 12-unit-wide box whose
  // height stretches with the cursor (the stroke keeps its width).
  const WAVE_MS = 900
  let spine = $state(spinePath(0, 0))
  let waveRaf = 0
  function spinePath(amp: number, phase: number): string {
    let d = ''
    for (let y = 0; y <= 100; y += 2.5) {
      const pin = Math.sin((Math.PI * y) / 100) // 0 at both ends
      const x = 6 + amp * pin * Math.sin((2 * Math.PI * y) / 55 + phase)
      d += `${y ? 'L' : 'M'}${x.toFixed(2)} ${y}`
    }
    return d
  }
  function wave(dir: 'up' | 'down') {
    cancelAnimationFrame(waveRaf)
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return
    const start = performance.now()
    const way = dir === 'down' ? -1 : 1 // the crests travel with the cursor
    const step = (now: number) => {
      const t = Math.min(1, (now - start) / WAVE_MS)
      // Swells fast, then settles: sin(πt) eased toward the end.
      const amp = 2.6 * Math.sin(Math.PI * t) * (1 - t * t * 0.5)
      spine = spinePath(t < 1 ? amp : 0, way * t * 2.5 * Math.PI)
      if (t < 1) waveRaf = requestAnimationFrame(step)
    }
    waveRaf = requestAnimationFrame(step)
  }
  $effect(() => () => cancelAnimationFrame(waveRaf))
  $effect(() => {
    void app.selectedId
    void app.entries
    if (!days) return
    const raf = requestAnimationFrame(placeCursor)
    return () => cancelAnimationFrame(raf)
  })
  $effect(() => {
    if (!days) return
    // Rows change height as images load or text wraps differently.
    const ro = new ResizeObserver(() => placeCursor())
    ro.observe(days)
    return () => ro.disconnect()
  })

  // Keep the selected row in view during keyboard navigation.
  $effect(() => {
    const id = app.selectedId
    if (!id || !scroller) return
    scroller.querySelector<HTMLElement>(`[data-id="${id}"]`)?.scrollIntoView({ block: 'nearest' })
  })

  async function resume(id: number) {
    try {
      await api.updateFeed(id, { paused: false })
      await api.refreshFeed(id)
      await app.refreshTree()
      await app.load()
      app.toast('Updates resumed.')
    } catch (e) {
      app.toast(`Couldn't resume updates. ${(e as Error).message}`)
    }
  }

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
      {#if renaming}
        <!-- svelte-ignore a11y_autofocus -->
        <input class="rename" aria-label="Search name" bind:value={draft} use:focusSelect
          onkeydown={(e) => {
            if (e.key === 'Enter') finishRename(true)
            else if (e.key === 'Escape') { e.stopPropagation(); finishRename(false) }
          }}
          onblur={() => finishRename(true)} />
      {:else if adHoc}
        <h1 title="Searching {app.title}">“{adHoc}”</h1>
      {:else}
        <h1>{app.title}</h1>
      {/if}
      <p class="sub">
        {#if !app.online}
          <span class="chip" title={app.syncedAt ? `Offline copy from ${new Date(app.syncedAt).toLocaleString()}` : 'No offline copy yet'}>Offline</span>
        {/if}
        {#if app.outboxCount > 0}
          <span class="chip">{app.outboxCount} {app.outboxCount === 1 ? 'change' : 'changes'} waiting</span>
        {/if}
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
      {#if adHoc}
        <button class="btn small" onclick={() => app.saveSearch()} title="Keep this search in the sidebar">Save search</button>
        <button class="icon-btn" aria-label="Clear search" title="Clear search" onclick={() => { app.query = ''; app.load() }}>
          <Icon name="x" />
        </button>
      {/if}
      {#if saved && !renaming}
        <button class="icon-btn" aria-label="Rename search" title="Rename search" onclick={startRename}><Icon name="pencil" /></button>
        <button class="icon-btn" aria-label="Remove search" title="Remove search" onclick={() => app.deleteSearch(saved.id)}><Icon name="trash" /></button>
      {/if}
      {#if currentFeed}
        <button
          class="icon-btn"
          aria-pressed={currentFeed.fullText}
          aria-label={currentFeed.fullText ? 'Stop fetching full articles for this feed' : 'Fetch full articles for this feed'}
          title={currentFeed.fullText ? 'Fetching full articles' : 'Fetch full articles from their pages'}
          onclick={() => app.setFeedFullText(currentFeed.id, !currentFeed.fullText)}
        >
          <Icon name="article" />
        </button>
        <button
          class="icon-btn"
          aria-pressed={currentFeed.notify}
          aria-label={currentFeed.notify ? 'Turn off notifications for this feed' : 'Notify me about new articles in this feed'}
          title={currentFeed.notify ? 'Notifications on for this feed' : 'Notify me about new articles'}
          onclick={() => app.setFeedNotify(currentFeed.id, !currentFeed.notify)}
        >
          <Icon name={currentFeed.notify ? 'bell' : 'bellOff'} />
        </button>
      {/if}
      {#if showUnreadToggle}
        <div class="seg" role="group" aria-label="Show">
          <button aria-pressed={app.prefs.unreadOnly} onclick={() => { app.setPrefs({ unreadOnly: true }); app.load() }}>Unread</button>
          <button aria-pressed={!app.prefs.unreadOnly} onclick={() => { app.setPrefs({ unreadOnly: false }); app.load() }}>All</button>
        </div>
      {/if}
      {#if app.entries.some((e) => e.audio && !e.read)}
        <button class="icon-btn" aria-label="Listen to the unread articles that have audio" title="Listen to the unread articles that have audio (Shift+L)" onclick={() => app.listenToList()}>
          <Icon name="headphones" />
        </button>
      {/if}
      {#if canMarkAll}
        <button class="icon-btn" aria-label="Mark all read" title="Mark all read (Shift+A)" onclick={() => app.markAllRead()}>
          <Icon name="checkAll" />
        </button>
      {/if}
    </div>
  </header>

  {#if currentFeed?.paused}
    <div class="notice" role="status">
      <Icon name="pause" size={16} />
      <div>
        <p>Updates for this feed are paused.</p>
        <button class="link" onclick={() => resume(currentFeed.id)}>Resume updates</button>
      </div>
    </div>
  {:else if currentFeed && currentFeed.errorCount > 0}
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

  <div class="wire-scroll" bind:this={scroller} onscroll={onScroll}>
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
      <ol class="days" bind:this={days}>
        {#if cursor}
          <li class="cursor" aria-hidden="true" data-dir={cursor.dir}
            style:--cur-top="{cursor.top}px" style:--cur-bot="{cursor.bottom}px">
            <svg class="spine" viewBox="0 0 12 100" preserveAspectRatio="none"><path d={spine} /></svg>
          </li>
        {/if}
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
                  use:arrive={e.id}
                  use:swipe={{ onCommit: (a) => (a === 'read' ? app.setRead([e.id], !e.read) : app.setStarred(e.id, !e.starred)) }}
                >
                  <span class="reveal" aria-hidden="true">
                    <span class="reveal-read"><Icon name={e.read ? 'dot' : 'check'} size={18} />{e.read ? 'Unread' : 'Read'}</span>
                    <span class="reveal-star"><Icon name="star" size={18} fill={!e.starred} />{e.starred ? 'Unstar' : 'Star'}</span>
                  </span>
                  <button
                    class="hit"
                    aria-current={selected ? 'true' : undefined}
                    onclick={() => app.select(e.id)}
                  >
                    <span class="gutter">
                      <time class="clock" datetime={new Date(e.publishedAt * 1000).toISOString()}>{clock(new Date(e.publishedAt * 1000))}</time>
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
    padding: calc(14px + env(safe-area-inset-top)) 12px 12px 18px;
    border-bottom: 1px solid var(--rule);
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  h1 {
    margin: 0;
    font-family: var(--font-read);
    font-size: calc(23px * var(--ui-scale));
    font-weight: 560;
    font-variation-settings: 'opsz' 36;
    letter-spacing: -0.012em;
    line-height: 1.15;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rename {
    width: 100%;
    margin: 0;
    padding: 0 6px;
    border: 1px solid var(--accent);
    border-radius: var(--radius-s);
    background: var(--sheet);
    color: var(--ink);
    font-family: var(--font-read);
    font-size: calc(21px * var(--ui-scale));
    font-weight: 560;
  }
  .btn.small {
    height: 30px;
    padding: 0 10px;
    font-size: calc(13px * var(--ui-scale));
    white-space: nowrap;
  }
  .sub {
    margin: 2px 0 0;
    color: var(--slate);
    font-size: calc(12.5px * var(--ui-scale));
  }
  .chip {
    display: inline-block;
    margin-right: 6px;
    padding: 0 7px;
    border-radius: 9px;
    font-size: calc(11.5px * var(--ui-scale));
    font-weight: 560;
    line-height: 18px;
    color: var(--ink);
    background: var(--fog);
    box-shadow: inset 0 0 0 1px var(--rule);
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
    font-size: calc(12.5px * var(--ui-scale));
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
    font-size: calc(13px * var(--ui-scale));
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
    font-size: calc(12.5px * var(--ui-scale));
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
    font-size: calc(12.5px * var(--ui-scale));
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
    overflow: hidden;
    touch-action: pan-y; /* vertical scrolling stays native; horizontal is ours */
  }
  /* Rows are transparent so the spine shows through; a row becomes opaque
     only while it is being swiped, to cover the action revealed behind it. */
  .row .hit {
    position: relative;
    z-index: 1;
    transform: translateX(var(--swipe-dx, 0));
    transition: transform 220ms var(--ease), background var(--fast) var(--ease);
  }
  .row:global(.swiping) .hit {
    transition: none;
    background: var(--sheet);
  }
  /* What a swipe will do, revealed behind the row. */
  .reveal {
    position: absolute;
    inset: 0;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0 22px;
    font-weight: 600;
    font-size: calc(13px * var(--ui-scale));
    color: var(--sheet);
    background: var(--ring);
    opacity: 0;
  }
  .row:global(.swiping) .reveal {
    opacity: 1;
  }
  .reveal span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .row:global([data-swipe^='read']) .reveal-star,
  .row:global([data-swipe^='star']) .reveal-read {
    visibility: hidden;
  }
  .row:global([data-swipe='read']) .reveal {
    background: var(--accent);
  }
  .row:global([data-swipe='star']) .reveal {
    background: var(--seal);
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
  /* The selection cursor sits under the rows (which are transparent): a
     wash across the row and its stretch of spine in accent. Its edges are
     registered properties (app.css) so each can move at its own pace. */
  .days {
    position: relative;
  }
  .cursor {
    position: absolute;
    left: 0;
    right: 0;
    top: var(--cur-top);
    height: calc(var(--cur-bot) - var(--cur-top));
    background: var(--accent-wash);
    pointer-events: none;
    transition:
      --cur-top 300ms var(--ease),
      --cur-bot 300ms var(--ease);
  }
  .spine {
    position: absolute;
    /* Centred where the 3px bar was: on the spine. */
    left: calc(var(--spine-x) + 0.75px - 6px);
    top: 0;
    width: 12px;
    height: 100%;
    overflow: visible;
  }
  .spine path {
    fill: none;
    stroke: var(--accent);
    stroke-width: 3px;
    stroke-linecap: round;
    stroke-linejoin: round;
    vector-effect: non-scaling-stroke;
  }
  /* Moving down, the bottom edge leads and the top follows; moving up, the
     reverse. */
  .cursor[data-dir='down'] {
    transition:
      --cur-top 420ms cubic-bezier(0.3, 0, 0.1, 1) 40ms,
      --cur-bot 240ms cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .cursor[data-dir='up'] {
    transition:
      --cur-top 240ms cubic-bezier(0.2, 0.8, 0.2, 1),
      --cur-bot 420ms cubic-bezier(0.3, 0, 0.1, 1) 40ms;
  }
  .cursor[data-dir='none'] {
    transition: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .cursor,
    .cursor[data-dir] {
      transition: none;
    }
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
    font-size: calc(11.5px * var(--ui-scale));
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
    font-size: calc(12.5px * var(--ui-scale));
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
    font-size: calc(17px * var(--ui-scale));
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
    font-size: calc(13px * var(--ui-scale));
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

  /* The home-indicator inset belongs to the opaque ends, never to the
     scroller, or the spine would show through below them. */
  .tail {
    min-height: calc(64px + env(safe-area-inset-bottom));
    padding-bottom: env(safe-area-inset-bottom);
    display: flex;
    align-items: center;
    padding-left: calc(var(--spine-x) - 5px);
    color: var(--slate);
    font-size: calc(12.5px * var(--ui-scale));
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
    padding: 56px 24px calc(24px + env(safe-area-inset-bottom)) calc(var(--spine-x) + 26px);
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
    font-size: calc(22px * var(--ui-scale));
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
