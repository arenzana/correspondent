<script lang="ts">
  import { app } from '../lib/state.svelte'
  import { longDate, readingMinutes } from '../lib/time'
  import Icon from './Icon.svelte'
  import FeedIcon from './FeedIcon.svelte'
  import ReadingSettings from './ReadingSettings.svelte'

  let scroller: HTMLElement | undefined = $state()
  let progress = $state(0)

  const a = $derived(app.article)
  const feed = $derived(a ? app.feedById.get(a.entry.feedId) : undefined)
  const minutes = $derived(a?.entry.content ? readingMinutes(a.entry.content) : 0)
  const position = $derived(app.selectedIndex >= 0 ? `${app.selectedIndex + 1} of ${app.entries.length}${app.cursor ? '+' : ''}` : '')
  const audio = $derived(a?.enclosures.filter((e) => e.type.startsWith('audio/')) ?? [])
  const video = $derived(a?.enclosures.filter((e) => e.type.startsWith('video/')) ?? [])

  // Start each article at the top.
  $effect(() => {
    void app.selectedId
    scroller?.scrollTo({ top: 0 })
    progress = 0
  })

  function onScroll() {
    if (!scroller) return
    const max = scroller.scrollHeight - scroller.clientHeight
    progress = max > 0 ? scroller.scrollTop / max : 1
  }

  /** Space: page down, or advance to the next article at the end. Exposed to the keyboard handler. */
  export function pageDown(reverse = false) {
    if (!scroller) return
    const max = scroller.scrollHeight - scroller.clientHeight
    if (!reverse && scroller.scrollTop >= max - 4) {
      app.move(1)
      return
    }
    scroller.scrollBy({ top: (reverse ? -1 : 1) * scroller.clientHeight * 0.85, behavior: 'smooth' })
  }
</script>

<article class="pane" aria-label="Article">
  {#if a}
    <div class="bar">
      <button class="icon-btn only-narrow" aria-label="Back to articles" onclick={() => app.back()}>
        <Icon name="back" />
      </button>
      <button
        class="icon-btn"
        aria-label={app.focusMode ? 'Leave focused reading' : 'Focused reading'}
        aria-pressed={app.focusMode}
        title="Focused reading (f)"
        onclick={() => (app.focusMode = !app.focusMode)}
      >
        <Icon name="focus" />
      </button>
      <span class="position">{position}</span>
      <span class="spacer"></span>
      <button
        class="icon-btn star"
        aria-label={a.entry.starred ? 'Unstar' : 'Star'}
        aria-pressed={a.entry.starred}
        title="Star (s)"
        onclick={() => app.toggleStar()}
      >
        <Icon name="star" fill={a.entry.starred} />
      </button>
      <button
        class="icon-btn"
        aria-label={a.entry.read ? 'Mark unread' : 'Mark read'}
        title="Toggle read (m)"
        onclick={() => app.toggleRead()}
      >
        <Icon name={a.entry.read ? 'circle' : 'dot'} />
      </button>
      {#if a.entry.url}
        <a class="icon-btn" href={a.entry.url} target="_blank" rel="noopener noreferrer" aria-label="Open original" title="Open original (v)">
          <Icon name="external" />
        </a>
      {/if}
      <div class="aa">
        <button
          class="icon-btn"
          aria-label="Reading settings"
          aria-expanded={app.readingOpen}
          title="Reading settings"
          onclick={() => (app.readingOpen = !app.readingOpen)}
        >
          <Icon name="type" />
        </button>
        {#if app.readingOpen}<ReadingSettings />{/if}
      </div>
    </div>
    <div class="progress" style:--p={progress} aria-hidden="true"></div>

    <div class="scroll" bind:this={scroller} onscroll={onScroll} tabindex="-1">
      <div class="column">
        <header>
          <p class="byline">
            {#if feed}<FeedIcon url={feed.iconUrl} title={feed.title} size={16} />{/if}
            <span class="source">{feed?.title ?? ''}</span>
            {#if a.entry.author && a.entry.author !== feed?.title}<span>by {a.entry.author}</span>{/if}
          </p>
          <h1>
            {#if a.entry.url}
              <a href={a.entry.url} target="_blank" rel="noopener noreferrer">{a.entry.title || 'Untitled'}</a>
            {:else}
              {a.entry.title || 'Untitled'}
            {/if}
          </h1>
          <p class="dateline">
            <time datetime={new Date(a.entry.publishedAt * 1000).toISOString()}>{longDate(a.entry.publishedAt)}</time>
            {#if minutes}<span>{minutes} min read</span>{/if}
          </p>
        </header>

        {#each audio as en (en.url)}
          <audio class="media" controls preload="none" src={en.url}></audio>
        {/each}
        {#each video as en (en.url)}
          <!-- svelte-ignore a11y_media_has_caption -->
          <video class="media" controls preload="none" src={en.url}></video>
        {/each}

        {#if app.articleError}
          <p class="note">Couldn't load this article. {app.articleError}</p>
        {:else if a.entry.content === undefined}
          <div class="skeleton" aria-label="Loading article">
            <span></span><span></span><span></span><span class="short"></span>
          </div>
        {:else if a.entry.content}
          <!-- Content is sanitized server-side by an allowlist before storage. -->
          <div class="prose">{@html a.entry.content}</div>
        {:else}
          <p class="note">This feed publishes only titles. <a href={a.entry.url} target="_blank" rel="noopener noreferrer">Read it on the site</a>.</p>
        {/if}

        <footer class="end">
          <span class="end-mark" aria-hidden="true"></span>
          {#if app.selectedIndex >= 0 && app.selectedIndex < app.entries.length - 1}
            {@const next = app.entries[app.selectedIndex + 1]}
            <button class="next" onclick={() => app.move(1)}>
              <span class="next-label">Next <kbd>j</kbd></span>
              <span class="next-title">{next.title}</span>
            </button>
          {/if}
        </footer>
      </div>
    </div>
  {:else}
    <div class="blank">
      <div class="blank-inner">
        <span class="blank-mark" aria-hidden="true"><i></i><b></b><b></b></span>
        <p>Select an article to read it.</p>
        <p class="hint"><kbd>j</kbd> <kbd>k</kbd> to move, <kbd>Space</kbd> to read on, <kbd>?</kbd> for all shortcuts.</p>
      </div>
    </div>
  {/if}
</article>

<style>
  .pane {
    position: relative;
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--sheet);
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 2px;
    height: 52px;
    padding: 0 10px;
    border-bottom: 1px solid var(--rule);
  }
  .position {
    margin-left: 6px;
    font-size: 12.5px;
    color: var(--slate);
    font-variant-numeric: tabular-nums;
  }
  .spacer {
    flex: 1;
  }
  .aa {
    position: relative;
  }
  .progress {
    height: 2px;
    margin-top: -1px;
    background: linear-gradient(var(--accent), var(--accent)) no-repeat 0 0 / calc(var(--p) * 100%) 100%;
    position: relative;
    z-index: 1;
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    scrollbar-gutter: stable;
    outline: none;
  }
  .column {
    max-width: calc(var(--read-measure) + 64px);
    margin: 0 auto;
    padding: 44px 32px 96px;
  }
  .byline {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0 0 14px;
    font-size: 13px;
    color: var(--slate);
  }
  .source {
    font-weight: 600;
    color: var(--ink);
  }
  h1 {
    margin: 0;
    font-family: var(--font-read);
    font-size: clamp(30px, 3.4vw, 42px);
    font-weight: 500;
    font-variation-settings: 'opsz' 72;
    line-height: 1.08;
    letter-spacing: -0.018em;
    text-wrap: balance;
  }
  h1 a {
    color: inherit;
    text-decoration: none;
  }
  h1 a:hover {
    text-decoration: underline;
    text-decoration-thickness: 1px;
    text-underline-offset: 4px;
  }
  .dateline {
    display: flex;
    gap: 14px;
    margin: 14px 0 34px;
    padding-bottom: 22px;
    border-bottom: 1px solid var(--rule);
    font-size: 13px;
    color: var(--slate);
  }
  .media {
    width: 100%;
    margin: 0 0 24px;
  }

  .prose {
    font-family: var(--read-family, var(--font-read));
    font-size: var(--read-size);
    line-height: 1.66;
    font-variation-settings: 'opsz' 16;
    color: var(--ink);
    overflow-wrap: break-word;
    hanging-punctuation: first;
  }
  .prose :global(p),
  .prose :global(ul),
  .prose :global(ol),
  .prose :global(pre),
  .prose :global(table),
  .prose :global(figure),
  .prose :global(blockquote) {
    margin: 0 0 1.05em;
  }
  .prose :global(h1),
  .prose :global(h2),
  .prose :global(h3),
  .prose :global(h4) {
    font-family: var(--font-read);
    font-weight: 560;
    line-height: 1.2;
    letter-spacing: -0.01em;
    margin: 1.7em 0 0.55em;
    text-wrap: balance;
  }
  .prose :global(h1),
  .prose :global(h2) {
    font-size: 1.32em;
  }
  .prose :global(h3) {
    font-size: 1.13em;
  }
  .prose :global(a) {
    color: var(--accent);
    text-decoration-thickness: 1px;
    text-underline-offset: 3px;
  }
  .prose :global(img),
  .prose :global(video),
  .prose :global(picture) {
    display: block;
    max-width: 100%;
    height: auto;
    border-radius: 4px;
  }
  .prose :global(p > img:only-child) {
    margin: 1.4em auto;
  }
  .prose :global(figure) {
    margin: 1.6em 0;
  }
  .prose :global(figcaption) {
    margin-top: 8px;
    font-family: var(--font-ui);
    font-size: 13px;
    line-height: 1.45;
    color: var(--slate);
  }
  .prose :global(blockquote) {
    margin-left: 0;
    padding-left: 1em;
    border-left: 2px solid var(--accent);
    color: color-mix(in oklab, var(--ink) 82%, var(--sheet));
    font-style: italic;
  }
  .prose :global(code) {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 0.82em;
    padding: 0.1em 0.3em;
    border-radius: 4px;
    background: var(--fog);
  }
  .prose :global(pre) {
    padding: 14px 16px;
    overflow-x: auto;
    border-radius: var(--radius-s);
    background: var(--fog);
    line-height: 1.5;
  }
  .prose :global(pre code) {
    padding: 0;
    background: none;
  }
  .prose :global(hr) {
    border: 0;
    height: 1px;
    background: var(--rule);
    margin: 2em 0;
  }
  .prose :global(table) {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85em;
    display: block;
    overflow-x: auto;
  }
  .prose :global(th),
  .prose :global(td) {
    padding: 6px 10px;
    border-bottom: 1px solid var(--rule);
    text-align: left;
  }

  .note {
    color: var(--slate);
  }
  .skeleton {
    display: grid;
    gap: 12px;
  }
  .skeleton span {
    height: 14px;
    border-radius: 3px;
    background: var(--fog);
  }
  .skeleton .short {
    width: 60%;
  }

  .end {
    margin-top: 48px;
  }
  .end-mark {
    display: block;
    width: 28px;
    height: 1.5px;
    margin-bottom: 28px;
    background: var(--ink);
    opacity: 0.4;
  }
  .next {
    display: flex;
    flex-direction: column;
    gap: 4px;
    width: 100%;
    padding: 14px 16px;
    text-align: left;
    border-radius: var(--radius-s);
    border: 1px solid var(--rule);
  }
  .next:hover {
    background: var(--hover);
  }
  .next-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
    color: var(--slate);
  }
  .next-title {
    font-family: var(--font-read);
    font-size: 18px;
    font-weight: 560;
  }

  .blank {
    flex: 1;
    display: grid;
    place-items: center;
    color: var(--slate);
    padding: 24px;
  }
  .blank-inner {
    text-align: center;
  }
  .blank-inner p {
    margin: 6px 0;
  }
  .hint {
    font-size: 12.5px;
  }
  .blank-mark {
    position: relative;
    display: inline-block;
    width: 12px;
    height: 54px;
    margin-bottom: 14px;
  }
  .blank-mark::before {
    content: '';
    position: absolute;
    left: 5.25px;
    top: 0;
    bottom: 0;
    width: 1.5px;
    background: var(--rule);
  }
  .blank-mark i,
  .blank-mark b {
    position: absolute;
    left: 1.5px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
  }
  .blank-mark i {
    top: 4px;
    background: var(--accent);
  }
  .blank-mark b {
    top: 22px;
    border: 1.5px solid var(--ring);
    background: var(--sheet);
  }
  .blank-mark b + b {
    top: 40px;
  }

  .only-narrow {
    display: none;
  }
  @media (max-width: 699px) {
    .only-narrow {
      display: inline-grid;
    }
    .column {
      padding: 28px 20px 72px;
    }
    .dateline {
      margin-bottom: 26px;
    }
  }
</style>
