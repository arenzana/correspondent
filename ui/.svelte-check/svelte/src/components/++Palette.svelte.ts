///<reference types="svelte" />
;// ⌘K: one place to jump to any feed or folder, search article text, or run
// an action. Items are ranked by a simple subsequence score.

import { app, type Source } from '../lib/state.svelte'
import Modal from './Modal.svelte'
import Icon, { type IconName } from './Icon.svelte'
import FeedIcon from './FeedIcon.svelte';
function $$render() {

  
  
  
  
  
  

  type Item = { id: string; label: string; hint?: string; icon?: IconName; feedIcon?: string; group: string; run: () => void }

  let q = $state('')
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
      ...app.folders.map((f) => ({
        id: 'f' + f.id, label: f.name, icon: 'folder' as IconName, group: 'Folders',
        hint: f.unread ? `${f.unread} unread` : undefined, run: go({ kind: 'folder', id: f.id }),
      })),
      ...app.feeds.map((f) => ({
        id: 'd' + f.id, label: f.title || f.url, feedIcon: f.iconUrl, group: 'Feeds',
        hint: f.unread ? `${f.unread} unread` : undefined, run: go({ kind: 'feed', id: f.id }),
      })),
      { id: 'a-mark', label: `Mark all as read in ${scopeName}`, icon: 'checkAll', group: 'Actions', hint: '⇧A', run: () => app.markAllRead() },
      { id: 'a-add', label: 'Add feed…', icon: 'plus', group: 'Actions', hint: 'a', run: () => (app.addFeedOpen = true) },
      { id: 'a-focus', label: app.focusMode ? 'Leave focused reading' : 'Focused reading', icon: 'focus', group: 'Actions', hint: 'f', run: () => (app.focusMode = !app.focusMode) },
      { id: 'a-day', label: 'Switch to Day', icon: 'sun', group: 'Actions', run: () => app.setPrefs({ theme: 'day' }) },
      { id: 'a-night', label: 'Switch to Night desk', icon: 'sun', group: 'Actions', run: () => app.setPrefs({ theme: 'night' }) },
      { id: 'a-evening', label: 'Switch to Evening', icon: 'sun', group: 'Actions', run: () => app.setPrefs({ theme: 'evening' }) },
      { id: 'a-auto', label: 'Match system theme', icon: 'sun', group: 'Actions', run: () => app.setPrefs({ theme: 'auto' }) },
      { id: 'a-keys', label: 'Keyboard shortcuts', icon: 'keyboard', group: 'Actions', hint: '?', run: () => (app.shortcutsOpen = true) },
      { id: 'a-settings', label: 'Settings', icon: 'settings', group: 'Actions', run: () => (app.settingsOpen = true) },
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
    const list = ranked.slice(0, q.trim() ? 12 : 40).map((r) => r.it)
    if (q.trim()) {
      list.push({
        id: 'search', label: `Search articles for “${q.trim()}”`, icon: 'search', group: 'Search',
        run: () => { app.query = q.trim(); app.go({ kind: 'view', view: 'all' }) },
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
;
async () => {

 { const $$_ladoM0C = __sveltets_2_ensureComponent(Modal); new $$_ladoM0C({ target: __sveltets_2_any(), props: {       children:() => { return __sveltets_2_any(0); },"label":`Command palette`,"onclose":close,"width":600,"align":`top`,}});
   { svelteHTML.createElement("div", { "class":`field-row`,});
     { const $$_nocI2C = __sveltets_2_ensureComponent(Icon); new $$_nocI2C({ target: __sveltets_2_any(), props: {  "name":`search`,}});}
     { const $$_input2 = svelteHTML.createElement("input", {                    "bind:value":q,"onkeydown":onkey,"placeholder":`Jump to a feed, search articles, or run a command`,"role":`combobox`,"aria-expanded":`true`,"aria-controls":`palette-list`,"aria-activedescendant":results[active] ? 'pal-' + active : undefined,"autocomplete":`off`,"spellcheck":`false`,});input = $$_input2;/*Ωignore_startΩ*/() => q = __sveltets_2_any(null);/*Ωignore_endΩ*/}
     { svelteHTML.createElement("kbd", {});  }
   }
   { svelteHTML.createElement("ul", {     "id":`palette-list`,"role":`listbox`,"aria-label":`Results`,});
        for(let it of __sveltets_2_ensureArray(results)){let i = 1;it.id;
      if(i === 0 || results[i - 1].group !== it.group){
         { svelteHTML.createElement("li", {   "class":`group`,"role":`presentation`,});it.group; }
      }
       { svelteHTML.createElement("li", {               "id":'pal-' + i,"role":`option`,"aria-selected":i === active,"class":`item`,"onmousemove":() => (active = i),"onclick":() => run(it),"onkeydown":() => {},});
         { svelteHTML.createElement("span", { "class":`ic`,});
          if(it.feedIcon !== undefined){
             { const $$_nocIdeeF4C = __sveltets_2_ensureComponent(FeedIcon); new $$_nocIdeeF4C({ target: __sveltets_2_any(), props: {      "url":it.feedIcon,"title":it.label,"size":16,}});}
          } else if (it.icon){
             { const $$_nocI4C = __sveltets_2_ensureComponent(Icon); new $$_nocI4C({ target: __sveltets_2_any(), props: {    "name":it.icon,"size":16,}});}
          }
         }
         { svelteHTML.createElement("span", { "class":`label`,});it.label; }
        if(it.hint){ { svelteHTML.createElement("span", { "class":`hint`,});it.hint; }}
       }
    }
    if(results.length === 0){
       { svelteHTML.createElement("li", { "class":`none`,});  }
    }
   }
 Modal}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Palette__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Palette__SvelteComponent_ = ReturnType<typeof Palette__SvelteComponent_>;
/*Ωignore_endΩ*/export default Palette__SvelteComponent_;