<script lang="ts">
  import { scrollMotion } from '../lib/motion'
  import { app } from '../lib/state.svelte'
  import { BarTracker, NARROW } from '../lib/bars'
  import { longDate, readingMinutes } from '../lib/time'
  import { looksPartial as isPartial } from '../lib/partial'
  import Icon from './Icon.svelte'
  import FeedIcon from './FeedIcon.svelte'
  import ReadingSettings from './ReadingSettings.svelte'
  import SendMenu from './SendMenu.svelte'
  import { bionicHTML, paragraphSpotlight, READING_LINE } from '../lib/reading'

  let scroller: HTMLElement | undefined = $state()
  let progress = $state(0)

  const a = $derived(app.article)
  const feed = $derived(a ? app.feedById.get(a.entry.feedId) : undefined)
  const minutes = $derived(a?.entry.content ? readingMinutes(a.entry.content) : 0)
  const looksPartial = $derived(
    !!a && !!a.entry.url && !a.entry.fullText && a.entry.content !== undefined && isPartial(a.entry.content),
  )
  const position = $derived(app.selectedIndex >= 0 ? `${app.selectedIndex + 1} of ${app.entries.length}${app.cursor ? '+' : ''}` : '')
  const audio = $derived(a?.enclosures.filter((e) => e.type.startsWith('audio/')) ?? [])
  const video = $derived(a?.enclosures.filter((e) => e.type.startsWith('video/')) ?? [])

  // Reading aids: bionic text, and a spotlight on one paragraph or line.
  const content = $derived(a?.entry.content ? (app.prefs.bionic ? bionicHTML(a.entry.content) : a.entry.content) : '')
  let prose: HTMLElement | undefined = $state()
  let viewH = $state(0)
  const lineH = $derived(app.prefs.readSize * 1.66)
  let paragraphs: ReturnType<typeof paragraphSpotlight> | undefined
  $effect(() => {
    void content
    if (app.prefs.spotlight !== 'paragraph' || !scroller || !prose) return
    const s = paragraphSpotlight(scroller, prose)
    paragraphs = s
    return () => {
      s.stop()
      paragraphs = undefined
    }
  })

  /** ↓/↑ with the spotlight on: the next paragraph, or the next line. */
  export function spotlightStep(by: number) {
    if (app.prefs.spotlight === 'paragraph') paragraphs?.step(by)
    else scroller?.scrollBy({ top: by * lineH, behavior: scrollMotion() })
  }

  // Start each article at the top.
  $effect(() => {
    void app.selectedId
    scroller?.scrollTo({ top: 0 })
    progress = 0
  })

  const bars = new BarTracker()
  let barH = $state(0)
  function onScroll() {
    if (!scroller) return
    const max = scroller.scrollHeight - scroller.clientHeight
    progress = max > 0 ? scroller.scrollTop / max : 1
    if (matchMedia(NARROW).matches) app.barsHidden = bars.update(scroller.scrollTop, max, performance.now())
  }

  /** Space: page down, or advance to the next article at the end. Exposed to the keyboard handler. */
  export function pageDown(reverse = false) {
    if (!scroller) return
    const max = scroller.scrollHeight - scroller.clientHeight
    if (!reverse && scroller.scrollTop >= max - 4) {
      app.move(1)
      return
    }
    scroller.scrollBy({ top: (reverse ? -1 : 1) * scroller.clientHeight * 0.85, behavior: scrollMotion() })
  }
</script>

<article class="pane" aria-label="Article">
  {#if a}
    <div class="bar" class:tucked={app.barsHidden} bind:offsetHeight={barH} style:--bar-h="{barH}px">
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
        <button
          class="icon-btn"
          aria-pressed={!!a.entry.fullText}
          aria-label={a.entry.fullText ? 'Show the feed text' : 'Load the full article'}
          title={a.entry.fullText ? 'Showing the full article. Click for the feed text' : 'Load the full article from its page'}
          disabled={app.fetchingFull}
          onclick={() => (a.entry.fullText ? app.showFeedText() : app.loadFullArticle())}
        >
          <Icon name="article" />
        </button>
        <a class="icon-btn" href={a.entry.url} target="_blank" rel="noopener noreferrer" aria-label="Open original" title="Open original (v)">
          <Icon name="external" />
        </a>
      {/if}
      {#if app.ai || a.entry.summary}
        <button
          class="icon-btn"
          class:busy={app.summarizing === a.entry.id}
          aria-pressed={app.summaryOpen === a.entry.id}
          aria-label={app.summarizing === a.entry.id ? 'Summarizing' : 'Summary'}
          title={app.summarizing === a.entry.id ? 'Summarizing' : a.entry.summary ? 'Summary (Shift+S)' : 'Summarize (Shift+S)'}
          disabled={!!app.summarizing || !a.entry.content}
          onclick={() => app.summarize()}
        >
          <Icon name="summary" />
        </button>
      {/if}
      {#if app.speech || a.entry.audio}
        <button
          class="icon-btn"
          class:busy={app.preparingAudio === a.entry.id}
          aria-pressed={app.listening?.id === a.entry.id}
          aria-label={app.preparingAudio === a.entry.id ? 'Preparing audio' : 'Listen'}
          title={app.preparingAudio === a.entry.id ? 'Preparing audio. Long articles take a minute' : a.entry.audio ? 'Listen (l)' : 'Read aloud (l)'}
          disabled={!!app.preparingAudio || a.entry.content === undefined}
          onclick={() => app.readAloud()}
        >
          <Icon name="headphones" />
        </button>
      {/if}
      <div class="aa">
        <button
          class="icon-btn"
          aria-label="Send to"
          aria-haspopup="menu"
          aria-expanded={app.sendOpen}
          title="Send to… (b)"
          onclick={() => ((app.readingOpen = false), (app.sendOpen = !app.sendOpen))}
        >
          <Icon name="send" />
        </button>
        {#if app.sendOpen}<SendMenu />{/if}
      </div>
      <div class="aa">
        <button
          class="icon-btn"
          aria-label="Reading settings"
          aria-expanded={app.readingOpen}
          title="Reading settings"
          onclick={() => ((app.sendOpen = false), (app.readingOpen = !app.readingOpen))}
        >
          <Icon name="type" />
        </button>
        {#if app.readingOpen}<ReadingSettings />{/if}
      </div>
    </div>
    <div class="progress" style:--p={progress} aria-hidden="true"></div>

    <div class="scroll" class:spotlit={app.prefs.spotlight === 'paragraph'} bind:this={scroller} bind:clientHeight={viewH} onscroll={onScroll} tabindex="-1">
      {#if app.prefs.spotlight === 'line'}
        <!-- A band one line tall stays clear; above and below are veiled. -->
        <div class="veil" aria-hidden="true" style:--top="{viewH * READING_LINE - lineH * 0.65}px" style:--band="{lineH * 1.3}px">
          <span class="above"></span><span class="below"></span>
        </div>
      {/if}
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

        {#if a.entry.sponsored && !app.prefs.skipSponsored && !app.prefs.sponsoredAsked}
          <div class="partial sponsored" role="note">
            <p>We think this is a sponsored post. Would you rather not see posts like it?</p>
            <div class="partial-actions">
              <button class="btn primary" onclick={() => app.answerSponsored(true)}>Skip sponsored posts</button>
              <button class="btn" onclick={() => app.answerSponsored(false)}>Keep showing them</button>
            </div>
          </div>
        {/if}

        {#if app.summaryOpen === a.entry.id && a.entry.summary}
          <aside class="summary" aria-label="Summary">
            <p>{a.entry.summary}</p>
            <small>Summary written by AI.</small>
          </aside>
        {/if}

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
        {:else}
          {#if a.entry.fullText}
            <p class="source-note">Full article from <a href={a.entry.url} target="_blank" rel="noopener noreferrer">{new URL(a.entry.url).hostname}</a>.
              <button class="link" onclick={() => app.showFeedText()}>Show the feed text</button>
              {#if feed && !feed.fullText}
                <button class="link" onclick={() => app.setFeedFullText(feed.id, true)}>Do this for every article from {feed.title}</button>
              {/if}</p>
          {/if}
          {#if a.entry.content}
            <!-- Content is sanitized server-side by an allowlist before storage. -->
            <div class="prose" class:bionic={app.prefs.bionic} class:sans={app.prefs.readFont === 'sans'} bind:this={prose}>{@html content}</div>
          {/if}
          {#if looksPartial}
            <div class="partial">
              <p>{a.entry.content ? 'This feed publishes only part of the article.' : 'This feed publishes only titles.'}
                {#if a.entry.fullError}<span class="err">Last try: {a.entry.fullError}</span>{/if}</p>
              <div class="partial-actions">
                <button class="btn primary" disabled={app.fetchingFull} onclick={() => app.loadFullArticle()}>
                  {app.fetchingFull ? 'Loading…' : 'Load the full article'}
                </button>
                <a class="btn" href={a.entry.url} target="_blank" rel="noopener noreferrer">Read it on the site</a>
              </div>
              {#if feed && !feed.fullText}
                <button class="always" disabled={app.fetchingFull} onclick={() => app.fullTextForFeed(feed.id)}>
                  <Icon name="article" size={16} />
                  <span><span class="t">Always load full articles from {feed.title}</span><small>New articles arrive complete, here and in your apps.</small></span>
                </button>
              {/if}
            </div>
          {/if}
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
  .icon-btn.busy {
    animation: breathe 1.2s ease-in-out infinite;
  }
  .summary {
    margin: 0 0 28px;
    padding: 2px 0 2px 16px;
    border-left: 2px solid var(--accent);
    font-family: var(--font-ui);
    font-size: calc(15.5px * var(--ui-scale));
    line-height: 1.55;
    color: var(--ink);
  }
  .summary p {
    margin: 0 0 6px;
  }
  .summary small {
    color: var(--slate);
    font-size: calc(12px * var(--ui-scale));
  }
  @keyframes breathe {
    50% {
      opacity: 0.35;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .icon-btn.busy {
      animation: none;
    }
  }
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
    min-height: calc(52px + env(safe-area-inset-top));
    padding: env(safe-area-inset-top) 10px 0;
    border-bottom: 1px solid var(--rule);
  }
  /* Phones: the toolbar tucks away while reading on, like the list header. */
  @media (max-width: 699px) {
    .bar {
      transition: margin-top 220ms var(--ease);
    }
    .bar > :global(*) {
      transition: opacity 160ms var(--ease);
    }
    .bar.tucked {
      margin-top: calc(env(safe-area-inset-top) - var(--bar-h));
    }
    .bar.tucked > :global(*) {
      opacity: 0;
      pointer-events: none;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .bar,
    .bar > :global(*) {
      transition: none;
    }
  }
  .position {
    margin-left: 6px;
    font-size: calc(12.5px * var(--ui-scale));
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

  /* The spotlight: the lit paragraph clear, the rest translucent. */
  .spotlit :global(.sl) {
    opacity: 0.28;
    transition: opacity 0.25s ease;
  }
  .spotlit :global(.sl.lit) {
    opacity: 1;
  }
  .veil {
    position: sticky;
    top: 0;
    height: 0;
    z-index: 2;
    pointer-events: none;
  }
  .veil span {
    position: absolute;
    left: 0;
    right: 0;
    background: color-mix(in srgb, var(--sheet) 72%, transparent);
  }
  .veil .above {
    top: 0;
    height: max(0px, var(--top));
  }
  .veil .below {
    top: calc(var(--top) + var(--band));
    height: 100vh;
  }
  /* Semibold, not bold: heavy starts turn words into blobs. The rest of
     each word is a shade lighter, so the eye still lands on the starts. */
  .prose.bionic {
    color: color-mix(in srgb, var(--ink) 72%, var(--slate));
  }
  .prose.bionic :global(b.bionic) {
    font-weight: 580;
    color: var(--ink);
  }
  .prose.bionic.sans :global(b.bionic) {
    font-weight: 600;
  }
  @media (prefers-reduced-motion: reduce) {
    .spotlit :global(.sl) {
      transition: none;
    }
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    /* Reading scrolls only up and down; wide code and tables scroll inside
       themselves. */
    overflow-x: hidden;
    scrollbar-gutter: stable;
    outline: none;
    padding-bottom: env(safe-area-inset-bottom);
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
    font-size: calc(13px * var(--ui-scale));
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
    font-size: calc(13px * var(--ui-scale));
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
    font-size: calc(13px * var(--ui-scale));
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
  .source-note {
    margin: -18px 0 22px;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  .source-note a {
    color: inherit;
  }
  .link {
    color: var(--accent);
    font-weight: 500;
    margin-left: 6px;
  }
  .partial {
    margin: 28px 0 0;
    padding: 16px 18px;
    border-radius: var(--radius-l);
    background: var(--fog);
  }
  .partial.sponsored {
    margin: 0 0 28px;
  }
  .partial p {
    margin: 0 0 12px;
  }
  .partial .err {
    display: block;
    margin-top: 4px;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  .partial-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .partial-actions .btn {
    text-decoration: none;
  }
  .always {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    width: 100%;
    margin-top: 14px;
    padding-top: 14px;
    border-top: 1px solid var(--rule);
    text-align: left;
    color: var(--ink);
    font-weight: 500;
  }
  .always :global(svg) {
    flex: none;
    margin-top: 2px;
    color: var(--accent);
  }
  .always small {
    display: block;
    margin-top: 2px;
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 400;
    color: var(--slate);
  }
  @media (hover: hover) {
    .always:hover .t {
      text-decoration: underline;
      text-decoration-thickness: 1px;
      text-underline-offset: 3px;
    }
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
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  .next-title {
    font-family: var(--font-read);
    font-size: calc(18px * var(--ui-scale));
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
    font-size: calc(12.5px * var(--ui-scale));
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
