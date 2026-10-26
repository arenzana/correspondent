<script lang="ts">
  import { app, overlayOpen } from './lib/state.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Wire from './components/Wire.svelte'
  import Article from './components/Article.svelte'
  import Palette from './components/Palette.svelte'
  import Shortcuts from './components/Shortcuts.svelte'
  import AddFeed from './components/AddFeed.svelte'
  import Settings from './components/Settings.svelte'
  import Login from './components/Login.svelte'
  import Toasts from './components/Toasts.svelte'
  import Resizer from './components/Resizer.svelte'
  import Manage from './components/Manage.svelte'

  const NAV_DEFAULT = 264
  const listDefault = () => Math.round(Math.min(440, Math.max(360, window.innerWidth * 0.3)))
  let navWidth = $derived(app.prefs.navWidth ?? NAV_DEFAULT)
  let listWidth = $derived(app.prefs.listWidth ?? listDefault())
  let resizing = $state(false)
  // While dragging, widths update locally; they are saved once on release.
  let live = $state<{ nav?: number; list?: number }>({})
  let article: ReturnType<typeof Article> | undefined = $state()
  let pendingG = false
  let gTimer: ReturnType<typeof setTimeout> | undefined

  app.boot()

  function typing(t: EventTarget | null): boolean {
    const el = t as HTMLElement | null
    return !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
  }

  function onkeydown(e: KeyboardEvent) {
    if (!app.user) return
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault()
      app.paletteOpen = !app.paletteOpen
      return
    }
    // Shortcuts never fire while typing or while a dialog owns the keyboard.
    if (typing(e.target) || overlayOpen() || e.metaKey || e.ctrlKey || e.altKey) return

    if (pendingG) {
      pendingG = false
      clearTimeout(gTimer)
      const views = { u: 'unread', t: 'today', s: 'starred', a: 'all' } as const
      const v = views[e.key as keyof typeof views]
      if (e.key === 'm') {
        e.preventDefault()
        app.manage()
      } else if (v) {
        e.preventDefault()
        app.go({ kind: 'view', view: v })
      }
      return
    }

    const actions: Record<string, () => void> = {
      j: () => app.move(1),
      ArrowDown: () => app.move(1),
      k: () => app.move(-1),
      ArrowUp: () => app.move(-1),
      n: () => app.move(1, true),
      p: () => app.move(-1, true),
      ' ': () => (app.article ? article?.pageDown(e.shiftKey) : app.move(1)),
      s: () => app.toggleStar(),
      m: () => app.toggleRead(),
      v: () => app.article?.entry.url && window.open(app.article.entry.url, '_blank', 'noopener,noreferrer'),
      A: () => app.markAllRead(),
      r: () => { app.refreshTree(); app.load() },
      f: () => (app.focusMode = !app.focusMode),
      '[': () => app.setPrefs({ sidebar: !app.prefs.sidebar }),
      a: () => (app.addFeedOpen = true),
      '/': () => (app.paletteOpen = true),
      '?': () => (app.shortcutsOpen = true),
      Escape: () => {
        if (app.readingOpen) app.readingOpen = false
        else if (app.focusMode) app.focusMode = false
        else if (app.navOpen) app.navOpen = false
        else if (app.pane === 'article' && window.matchMedia('(max-width: 699px)').matches) app.back()
      },
      g: () => {
        pendingG = true
        gTimer = setTimeout(() => (pendingG = false), 1200)
      },
    }
    const run = actions[e.key]
    if (run) {
      e.preventDefault()
      run()
    }
  }
</script>

<svelte:window {onkeydown} onpopstate={() => app.onPopState()} />
<svelte:document
  onvisibilitychange={() => {
    if (document.visibilityState === 'visible' && app.user) app.refreshTree()
  }}
/>

{#if app.booting}
  <div class="boot" aria-label="Loading"></div>
{:else if !app.user}
  <Login />
{:else}
  <div
    class="shell"
    class:no-sidebar={!app.prefs.sidebar}
    class:focus={app.focusMode}
    class:nav-open={app.navOpen}
    class:resizing
    class:managing={app.page === 'manage'}
    data-pane={app.pane}
    style:--nav-w="{live.nav ?? navWidth}px"
    style:--list-w="{live.list ?? listWidth}px"
  >
    <a class="skip" href="#reader">Skip to article</a>
    <div class="nav">
      <Sidebar />
      <span class="handle nav-handle">
        <Resizer label="Resize feeds" value={live.nav ?? navWidth} min={200} max={420} fallback={NAV_DEFAULT}
          ondragging={(d) => (resizing = d)}
          onresize={(w, done) => (done ? ((live = {}), app.setPrefs({ navWidth: w })) : (live = { ...live, nav: w }))} />
      </span>
    </div>
    {#if app.navOpen}
      <button class="scrim" aria-label="Close feeds" onclick={() => (app.navOpen = false)}></button>
    {/if}
    {#if app.page === 'manage'}
      <div class="list manage-pane"><Manage /></div>
    {:else}
    <div class="list">
      <Wire />
      <span class="handle list-handle">
        <Resizer label="Resize article list" value={live.list ?? listWidth} min={300} max={Math.max(320, Math.round(window.innerWidth * 0.5))}
          fallback={listDefault()}
          ondragging={(d) => (resizing = d)}
          onresize={(w, done) => (done ? ((live = {}), app.setPrefs({ listWidth: w })) : (live = { ...live, list: w }))} />
      </span>
    </div>
    <div class="read" id="reader"><Article bind:this={article} /></div>
    {/if}
  </div>

  {#if app.paletteOpen}<Palette />{/if}
  {#if app.shortcutsOpen}<Shortcuts />{/if}
  {#if app.addFeedOpen}<AddFeed />{/if}
  {#if app.settingsOpen}<Settings />{/if}
{/if}
<Toasts />

<style>
  .boot {
    height: 100%;
  }
  .shell {
    display: grid;
    grid-template-columns: var(--nav-w) var(--list-w) minmax(0, 1fr);
    height: 100dvh;
    overflow: hidden;
    transition: grid-template-columns 200ms var(--ease);
  }
  .nav,
  .list,
  .read {
    position: relative;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
  }
  .handle {
    display: contents;
  }
  /* The resizer sits on the pane edge, which overflow would clip. */
  .nav,
  .list {
    overflow: visible;
  }
  .nav > :global(nav),
  .list > :global(section) {
    overflow: hidden;
  }
  .shell.resizing {
    transition: none;
    cursor: col-resize;
    user-select: none;
  }
  .no-sidebar .nav-handle,
  .focus .handle {
    display: none;
  }
  .list {
    border-left: 1px solid var(--rule);
    border-right: 1px solid var(--rule);
  }
  .shell.no-sidebar {
    grid-template-columns: 0 var(--list-w) minmax(0, 1fr);
  }
  /* Subscription management takes the list and article columns together. */
  .shell.managing {
    grid-template-columns: var(--nav-w) minmax(0, 1fr);
  }
  .shell.managing.no-sidebar {
    grid-template-columns: 0 minmax(0, 1fr);
  }
  .manage-pane {
    border-right: 0;
  }
  .shell.no-sidebar .list {
    border-left: 0;
  }
  /* Focused reading: only the article, centred on the fog. */
  .shell.focus {
    grid-template-columns: 0 0 minmax(0, 1fr);
  }
  .shell.focus .list {
    border: 0;
  }
  .skip {
    position: absolute;
    left: -999px;
    z-index: 100;
  }
  .skip:focus {
    left: 12px;
    top: 12px;
    padding: 8px 12px;
    background: var(--sheet);
    border-radius: var(--radius-s);
  }

  /* Medium: list + article; feeds slide in as a drawer. */
  @media (max-width: 1099px) {
    .shell,
    .shell.no-sidebar {
      grid-template-columns: var(--list-w) minmax(0, 1fr);
    }
    .shell.managing,
    .shell.managing.no-sidebar {
      grid-template-columns: minmax(0, 1fr);
    }
    .shell.focus {
      grid-template-columns: 0 minmax(0, 1fr);
    }
    .nav {
      position: fixed;
      inset: 0 auto 0 0;
      z-index: 30;
      width: min(300px, 86vw);
      transform: translateX(-100%);
      transition: transform 200ms var(--ease);
      box-shadow: var(--overlay-shadow);
    }
    .nav-open .nav {
      transform: none;
    }
    .list {
      border-left: 0;
    }
    .nav-handle {
      display: none;
    }
    .scrim {
      position: fixed;
      inset: 0;
      z-index: 29;
      background: var(--scrim);
    }
  }

  /* Narrow: one pane at a time, feeds → list → article. */
  @media (max-width: 699px) {
    .shell,
    .shell.no-sidebar,
    .shell.focus {
      display: block;
      height: 100dvh;
    }
    .nav,
    .list,
    .read {
      position: fixed;
      inset: 0;
      width: auto;
      transform: none;
      box-shadow: none;
      border: 0;
      display: none;
    }
    .scrim,
    .handle {
      display: none;
    }
    [data-pane='nav'] .nav,
    [data-pane='list'] .list,
    [data-pane='article'] .read {
      display: block;
    }
    .nav-open .nav {
      display: block;
    }
  }
</style>
