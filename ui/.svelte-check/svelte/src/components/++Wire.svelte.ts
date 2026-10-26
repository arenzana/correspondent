///<reference types="svelte" />
;// The wire: articles hang off a vertical time spine. State lives on the
// spine itself: a filled dot is unread, a hollow ring is read, and a seal
// in the gutter marks a star. Titles also change weight, so state never
// relies on colour alone.

import { app } from '../lib/state.svelte'
import { arrivedAt, dayKey, dayLabel, gutterTime } from '../lib/time'
import { api, type Entry } from '../lib/api'
import Icon from './Icon.svelte'
import FeedIcon from './FeedIcon.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  

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
;
async () => {

 { svelteHTML.createElement("section", {   "class":`wire`,"aria-label":`Articles`,});
   { svelteHTML.createElement("header", { "class":`head`,});
     { svelteHTML.createElement("button", {     "class":`icon-btn only-narrow`,"aria-label":`Show feeds`,"onclick":() => app.back(),});
       { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {  "name":`back`,}});}
     }
     { svelteHTML.createElement("button", {     "class":`icon-btn only-medium`,"aria-label":`Show feeds`,"onclick":() => (app.navOpen = true),});
       { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {  "name":`menu`,}});}
     }
     { svelteHTML.createElement("div", { "class":`titles`,});
       { svelteHTML.createElement("h1", {});app.title; }
       { svelteHTML.createElement("p", { "class":`sub`,});
        if(app.loading && !app.entries.length){ } else if (app.unreadFilter || app.source.kind === 'view'){
          unreadHere; if(feedsHere > 1){  feedsHere; }if(app.cursor){  }
        }else{
          app.entries.length;app.cursor ? '+' : ''; }
       }
     }
     { svelteHTML.createElement("div", { "class":`tools`,});
      if(showUnreadToggle){
         { svelteHTML.createElement("div", {     "class":`seg`,"role":`group`,"aria-label":`Show`,});
           { svelteHTML.createElement("button", {   "aria-pressed":app.prefs.unreadOnly,"onclick":() => { app.setPrefs({ unreadOnly: true }); app.load() },});  }
           { svelteHTML.createElement("button", {   "aria-pressed":!app.prefs.unreadOnly,"onclick":() => { app.setPrefs({ unreadOnly: false }); app.load() },});  }
         }
      }
      if(canMarkAll){
         { svelteHTML.createElement("button", {       "class":`icon-btn`,"aria-label":`Mark all read`,"title":`Mark all read (Shift+A)`,"onclick":() => app.markAllRead(),});
           { const $$_nocI4C = __sveltets_2_ensureComponent(Icon); new $$_nocI4C({ target: __sveltets_2_any(), props: {  "name":`checkAll`,}});}
         }
      }
     }
   }

  if(currentFeed && currentFeed.errorCount > 0){
     { svelteHTML.createElement("div", {   "class":`notice`,"role":`status`,});
       { const $$_nocI2C = __sveltets_2_ensureComponent(Icon); new $$_nocI2C({ target: __sveltets_2_any(), props: {    "name":`alert`,"size":16,}});}
       { svelteHTML.createElement("div", {});
         { svelteHTML.createElement("p", {});    currentFeed.lastError; }
         { svelteHTML.createElement("button", {   "class":`link`,"onclick":() => retry(currentFeed.id),});
          retrying ? 'Retrying…' : 'Retry now';
         }
       }
     }
  }

  if(app.pending > 0){
     { svelteHTML.createElement("button", {   "class":`pending`,"onclick":() => app.showPending(),});
      app.pending;  app.pending === 1 ? 'article' : 'articles';
     }
  }

   { const $$_div1 = svelteHTML.createElement("div", {  "class":`wire-scroll`,});scroller = $$_div1;
    if(app.loadError){
       { svelteHTML.createElement("div", { "class":`state`,});
         { svelteHTML.createElement("p", {});   app.loadError; }
         { svelteHTML.createElement("button", {   "class":`btn`,"onclick":() => app.load(),});  }
       }
    } else if (!app.loading && app.entries.length === 0){
       { svelteHTML.createElement("div", { "class":`state`,});
         { svelteHTML.createElement("span", {   "class":`spine-end`,"aria-hidden":`true`,}); }
        if(app.query){
           { svelteHTML.createElement("h2", {});   app.query;  }
           { svelteHTML.createElement("button", {   "class":`btn`,"onclick":() => { app.query = ''; app.load() },});  }
        } else if (app.unreadFilter){
           { svelteHTML.createElement("h2", {});   }
           { svelteHTML.createElement("p", {});         }
           { svelteHTML.createElement("button", {   "class":`btn`,"onclick":() => app.go({ kind: 'view', view: 'today' }),});   }
        } else if (app.source.kind === 'view' && app.source.view === 'starred'){
           { svelteHTML.createElement("h2", {});   }
           { svelteHTML.createElement("p", {});  { svelteHTML.createElement("kbd", {});  }        }
        }else{
           { svelteHTML.createElement("h2", {});   }
           { svelteHTML.createElement("p", {});        }
        }
       }
    }else{
       { svelteHTML.createElement("ol", { "class":`days`,});
           for(let g of __sveltets_2_ensureArray(groups)){g.key;
           { svelteHTML.createElement("li", { "class":`day`,});
             { svelteHTML.createElement("h2", { "class":`day-label`,}); { svelteHTML.createElement("span", {});g.label; } }
             { svelteHTML.createElement("ol", { "class":`rows`,});
                 for(let e of __sveltets_2_ensureArray(g.entries)){e.id;
                const selected = e.id === app.selectedId;
                 { svelteHTML.createElement("li", {       "class":`row`,...__sveltets_2_empty({"data-id":e.id}),});e.read;selected;
                   { svelteHTML.createElement("button", {       "class":`hit`,"aria-current":selected ? 'true' : undefined,"onclick":() => app.select(e.id),});
                     { svelteHTML.createElement("span", { "class":`gutter`,});
                       { svelteHTML.createElement("time", {   "class":`clock`,"datetime":new Date(e.publishedAt * 1000).toISOString(),});gutterTime(e.publishedAt, arrivedAt(e.id)); }
                      if(e.starred){ { svelteHTML.createElement("span", {   "class":`seal`,"aria-label":`Starred`,}); }}
                     }
                     { svelteHTML.createElement("span", {   "class":`dot`,"aria-hidden":`true`,}); }
                     { svelteHTML.createElement("span", { "class":`body`,});
                       { svelteHTML.createElement("span", { "class":`meta`,});
                         { const $$_nocIdeeF9C = __sveltets_2_ensureComponent(FeedIcon); new $$_nocIdeeF9C({ target: __sveltets_2_any(), props: {      "url":app.feedById.get(e.feedId)?.iconUrl ?? '',"title":feedTitle(e),"size":14,}});}
                         { svelteHTML.createElement("span", { "class":`feed`,});feedTitle(e); }
                         { svelteHTML.createElement("span", { "class":`visually-hidden`,});e.read ? 'Read' : 'Unread'; }
                       }
                       { svelteHTML.createElement("span", { "class":`title`,});e.title || 'Untitled'; }
                      if(e.excerpt){ { svelteHTML.createElement("span", { "class":`excerpt`,});e.excerpt; }}
                     }
                    if(e.imageUrl){
                       { svelteHTML.createElement("img", {          "class":`thumb`,"src":e.imageUrl,"alt":"","loading":`lazy`,"referrerpolicy":`no-referrer`,"onerror":(ev) => ((ev.currentTarget as HTMLElement).style.display = 'none'),});}
                    }
                   }
                 }
              }
             }
           }
        }
       }
       { const $$_div2 = svelteHTML.createElement("div", {  "class":`tail`,});sentinel = $$_div2;
        if(app.loading){ } else if (!app.cursor){ { svelteHTML.createElement("span", {   "class":`spine-end`,"aria-hidden":`true`,}); }}
       }
    }
   }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Wire__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Wire__SvelteComponent_ = ReturnType<typeof Wire__SvelteComponent_>;
/*Ωignore_endΩ*/export default Wire__SvelteComponent_;