///<reference types="svelte" />
;
import { app, overlayOpen } from './lib/state.svelte'
import Sidebar from './components/Sidebar.svelte'
import Wire from './components/Wire.svelte'
import Article from './components/Article.svelte'
import Palette from './components/Palette.svelte'
import Shortcuts from './components/Shortcuts.svelte'
import AddFeed from './components/AddFeed.svelte'
import Settings from './components/Settings.svelte'
import Login from './components/Login.svelte'
import Toasts from './components/Toasts.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  

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
      if (v) {
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
;
async () => {

 { svelteHTML.createElement("svelte:window", {   onkeydown,"onpopstate":() => app.onPopState(),});}
 { svelteHTML.createElement("svelte:document", {   "onvisibilitychange":() => {
    if (document.visibilityState === 'visible' && app.user) app.refreshTree()
  },});}

if(app.booting){
   { svelteHTML.createElement("div", {   "class":`boot`,"aria-label":`Loading`,}); }
} else if (!app.user){
   { const $$_nigoL0C = __sveltets_2_ensureComponent(Login); new $$_nigoL0C({ target: __sveltets_2_any(), props: {}});}
}else{
   { svelteHTML.createElement("div", {        "class":`shell`,...__sveltets_2_empty({"data-pane":app.pane}),});!app.prefs.sidebar;app.focusMode;app.navOpen;
     { svelteHTML.createElement("a", {   "class":`skip`,"href":`#reader`,});   }
     { svelteHTML.createElement("div", { "class":`nav`,}); { const $$_rabediS2C = __sveltets_2_ensureComponent(Sidebar); new $$_rabediS2C({ target: __sveltets_2_any(), props: {}});} }
    if(app.navOpen){
       { svelteHTML.createElement("button", {     "class":`scrim`,"aria-label":`Close feeds`,"onclick":() => (app.navOpen = false),}); }
    }
     { svelteHTML.createElement("div", { "class":`list`,}); { const $$_eriW2C = __sveltets_2_ensureComponent(Wire); new $$_eriW2C({ target: __sveltets_2_any(), props: {}});} }
     { svelteHTML.createElement("div", {   "class":`read`,"id":`reader`,}); { const $$_elcitrA2C = __sveltets_2_ensureComponent(Article); const $$_elcitrA2 = new $$_elcitrA2C({ target: __sveltets_2_any(), props: {  }});article = $$_elcitrA2;} }
   }

  if(app.paletteOpen){ { const $$_ettelaP0C = __sveltets_2_ensureComponent(Palette); new $$_ettelaP0C({ target: __sveltets_2_any(), props: {}});}}
  if(app.shortcutsOpen){ { const $$_stuctrohS0C = __sveltets_2_ensureComponent(Shortcuts); new $$_stuctrohS0C({ target: __sveltets_2_any(), props: {}});}}
  if(app.addFeedOpen){ { const $$_deeFddA0C = __sveltets_2_ensureComponent(AddFeed); new $$_deeFddA0C({ target: __sveltets_2_any(), props: {}});}}
  if(app.settingsOpen){ { const $$_sgnitteS0C = __sveltets_2_ensureComponent(Settings); new $$_sgnitteS0C({ target: __sveltets_2_any(), props: {}});}}
}
 { const $$_stsaoT0C = __sveltets_2_ensureComponent(Toasts); new $$_stsaoT0C({ target: __sveltets_2_any(), props: {}});}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const App__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type App__SvelteComponent_ = ReturnType<typeof App__SvelteComponent_>;
/*Ωignore_endΩ*/export default App__SvelteComponent_;