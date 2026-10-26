///<reference types="svelte" />
;
import { app, type Source, type View } from '../lib/state.svelte'
import type { Feed } from '../lib/api'
import Icon, { type IconName } from './Icon.svelte'
import FeedIcon from './FeedIcon.svelte'
import Pulse from './Pulse.svelte';
function $$render() {
 const feedRow/*Ωignore_positionΩ*/ = (feed: Feed)/*Ωignore_startΩ*/: ReturnType<import('svelte').Snippet>/*Ωignore_endΩ*/ => { async ()/*Ωignore_positionΩ*/ => {
  const s: Source = { kind: 'feed', id: feed.id };
   { svelteHTML.createElement("li", {});
     { svelteHTML.createElement("button", {          "class":`row feed`,"aria-current":active(s) ? 'page' : undefined,"onclick":() => app.go(s),"title":feed.errorCount > 0 ? `Last update failed: ${feed.lastError}` : feed.title,});feed.errorCount > 0;
       { const $$_nocIdeeF2C = __sveltets_2_ensureComponent(FeedIcon); new $$_nocIdeeF2C({ target: __sveltets_2_any(), props: {    "url":feed.iconUrl,"title":feed.title || feed.url,}});}
       { svelteHTML.createElement("span", { "class":`name`,});feed.title || feed.url; }
      if(feed.errorCount > 0){
         { svelteHTML.createElement("span", {   "class":`warn`,"aria-label":`Last update failed`,}); { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {    "name":`alert`,"size":13,}});} }
      }
      if(feed.unread){ { svelteHTML.createElement("span", { "class":`count`,});feed.unread; }}
       { svelteHTML.createElement("span", { "class":`pulse-slot`,}); { const $$_esluP3C = __sveltets_2_ensureComponent(Pulse); new $$_esluP3C({ target: __sveltets_2_any(), props: {    "days":feed.cadence,"label":feed.title,}});} }
     }
   }
};return __sveltets_2_any(0)};
  
  
  
  
  

  const views: { view: View; label: string; icon: IconName }[] = [
    { view: 'unread', label: 'Unread', icon: 'inbox' },
    { view: 'today', label: 'Today', icon: 'sun' },
    { view: 'starred', label: 'Starred', icon: 'star' },
    { view: 'all', label: 'All articles', icon: 'layers' },
  ]

  let collapsed = $state<Record<number, boolean>>(loadCollapsed())

  function loadCollapsed(): Record<number, boolean> {
    try {
      return JSON.parse(localStorage.getItem('correspondent:collapsed') ?? '{}')
    } catch {
      return {}
    }
  }
  function toggle(id: number) {
    collapsed = { ...collapsed, [id]: !collapsed[id] }
    try {
      localStorage.setItem('correspondent:collapsed', JSON.stringify(collapsed))
    } catch {
      /* not essential */
    }
  }

  const grouped = $derived.by(() => {
    const byFolder = new Map<number | null, Feed[]>()
    for (const f of app.feeds) {
      const k = f.folderId ?? null
      if (!byFolder.has(k)) byFolder.set(k, [])
      byFolder.get(k)!.push(f)
    }
    return byFolder
  })

  function viewCount(v: View): number | null {
    if (v === 'unread') return app.counts.unread
    if (v === 'today') return app.counts.today
    if (v === 'starred') return app.counts.starred
    return null
  }

  function active(s: Source): boolean {
    return app.sourceKey(s) === app.sourceKey()
  }
;
async () => {

 { svelteHTML.createElement("nav", {   "class":`sidebar`,"aria-label":`Feeds`,});
   { svelteHTML.createElement("header", { "class":`brand`,});
     { svelteHTML.createElement("span", {   "class":`mark`,"aria-hidden":`true`,}); { svelteHTML.createElement("i", {}); } { svelteHTML.createElement("b", {}); } }
     { svelteHTML.createElement("span", { "class":`wordmark`,});  }
   }

   { svelteHTML.createElement("button", {   "class":`search`,"onclick":() => (app.paletteOpen = true),});
     { const $$_nocI2C = __sveltets_2_ensureComponent(Icon); new $$_nocI2C({ target: __sveltets_2_any(), props: {    "name":`search`,"size":15,}});}
     { svelteHTML.createElement("span", {});    }
     { svelteHTML.createElement("kbd", {});  }
   }

   { svelteHTML.createElement("div", { "class":`scroll`,});
     { svelteHTML.createElement("ul", { "class":`views`,});
         for(let v of __sveltets_2_ensureArray(views)){v.view;
        const s: Source = { kind: 'view', view: v.view };
        const n = viewCount(v.view);
         { svelteHTML.createElement("li", {});
           { svelteHTML.createElement("button", {     "class":`row`,"aria-current":active(s) ? 'page' : undefined,"onclick":() => app.go(s),});
             { svelteHTML.createElement("span", { "class":`glyph`,}); { const $$_nocI6C = __sveltets_2_ensureComponent(Icon); new $$_nocI6C({ target: __sveltets_2_any(), props: {    "name":v.icon,"size":16,}});} }
             { svelteHTML.createElement("span", { "class":`name`,});v.label; }
            if(n){ { svelteHTML.createElement("span", { "class":`count`,});n; }}
           }
         }
      }
     }

       for(let folder of __sveltets_2_ensureArray(app.folders)){folder.id;
      const feeds = grouped.get(folder.id) ?? [];
      const s: Source = { kind: 'folder', id: folder.id };
       { svelteHTML.createElement("section", { "class":`folder`,});
         { svelteHTML.createElement("div", { "class":`folder-head`,});
           { svelteHTML.createElement("button", {         "class":`disclosure`,"aria-expanded":!collapsed[folder.id],"aria-label":`${collapsed[folder.id] ? 'Expand' : 'Collapse'} ${folder.name}`,"onclick":() => toggle(folder.id),});
             { svelteHTML.createElement("span", {  "class":`chev`,});!collapsed[folder.id]; { const $$_nocI6C = __sveltets_2_ensureComponent(Icon); new $$_nocI6C({ target: __sveltets_2_any(), props: {    "name":`chevron`,"size":13,}});} }
           }
           { svelteHTML.createElement("button", {     "class":`row folder-row`,"aria-current":active(s) ? 'page' : undefined,"onclick":() => app.go(s),});
             { svelteHTML.createElement("span", { "class":`name`,});folder.name; }
            if(folder.unread){ { svelteHTML.createElement("span", { "class":`count`,});folder.unread; }}
           }
         }
        if(!collapsed[folder.id]){
           { svelteHTML.createElement("ul", {});
               for(let feed of __sveltets_2_ensureArray(feeds)){feed.id;
              ;__sveltets_2_ensureSnippet(feedRow(feed));
            }
           }
        }
       }
    }

    if(grouped.get(null)?.length){
       { svelteHTML.createElement("section", { "class":`folder`,});
         { svelteHTML.createElement("ul", {});
             for(let feed of __sveltets_2_ensureArray(grouped.get(null) ?? [])){feed.id;
            ;__sveltets_2_ensureSnippet(feedRow(feed));
          }
         }
       }
    }

    if(app.feeds.length === 0){
       { svelteHTML.createElement("div", { "class":`empty`,});
         { svelteHTML.createElement("p", {});       }
         { svelteHTML.createElement("button", {   "class":`btn primary`,"onclick":() => (app.addFeedOpen = true),});   }
       }
    }
   }

   { svelteHTML.createElement("footer", { "class":`foot`,});
     { svelteHTML.createElement("button", {       "class":`icon-btn`,"aria-label":`Add feed`,"title":`Add feed (a)`,"onclick":() => (app.addFeedOpen = true),});
       { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {  "name":`plus`,}});}
     }
     { svelteHTML.createElement("button", {       "class":`icon-btn`,"aria-label":`Keyboard shortcuts`,"title":`Keyboard shortcuts (?)`,"onclick":() => (app.shortcutsOpen = true),});
       { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {  "name":`keyboard`,}});}
     }
     { svelteHTML.createElement("span", { "class":`spacer`,}); }
     { svelteHTML.createElement("button", {       "class":`icon-btn`,"aria-label":`Settings`,"title":`Settings`,"onclick":() => (app.settingsOpen = true),});
       { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {  "name":`settings`,}});}
     }
   }
 }




};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Sidebar__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Sidebar__SvelteComponent_ = ReturnType<typeof Sidebar__SvelteComponent_>;
/*Ωignore_endΩ*/export default Sidebar__SvelteComponent_;