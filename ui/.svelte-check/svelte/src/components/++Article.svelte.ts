///<reference types="svelte" />
;
import { app } from '../lib/state.svelte'
import { longDate, readingMinutes } from '../lib/time'
import Icon from './Icon.svelte'
import FeedIcon from './FeedIcon.svelte'
import ReadingSettings from './ReadingSettings.svelte';
function $$render() {

  
  
  
  
  

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
   function pageDown(reverse = false) {
    if (!scroller) return
    const max = scroller.scrollHeight - scroller.clientHeight
    if (!reverse && scroller.scrollTop >= max - 4) {
      app.move(1)
      return
    }
    scroller.scrollBy({ top: (reverse ? -1 : 1) * scroller.clientHeight * 0.85, behavior: 'smooth' })
  }
;
async () => {

 { svelteHTML.createElement("article", {   "class":`pane`,"aria-label":`Article`,});
  if(a){
     { svelteHTML.createElement("div", { "class":`bar`,});
       { svelteHTML.createElement("button", {     "class":`icon-btn only-narrow`,"aria-label":`Back to articles`,"onclick":() => app.back(),});
         { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {  "name":`back`,}});}
       }
       { svelteHTML.createElement("button", {           "class":`icon-btn`,"aria-label":app.focusMode ? 'Leave focused reading' : 'Focused reading',"aria-pressed":app.focusMode,"title":`Focused reading (f)`,"onclick":() => (app.focusMode = !app.focusMode),});
         { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {  "name":`focus`,}});}
       }
       { svelteHTML.createElement("span", { "class":`position`,});position; }
       { svelteHTML.createElement("span", { "class":`spacer`,}); }
       { svelteHTML.createElement("button", {           "class":`icon-btn star`,"aria-label":a.entry.starred ? 'Unstar' : 'Star',"aria-pressed":a.entry.starred,"title":`Star (s)`,"onclick":() => app.toggleStar(),});
         { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {    "name":`star`,"fill":a.entry.starred,}});}
       }
       { svelteHTML.createElement("button", {         "class":`icon-btn`,"aria-label":a.entry.read ? 'Mark unread' : 'Mark read',"title":`Toggle read (m)`,"onclick":() => app.toggleRead(),});
         { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {  "name":a.entry.read ? 'circle' : 'dot',}});}
       }
      if(a.entry.url){
         { svelteHTML.createElement("a", {           "class":`icon-btn`,"href":a.entry.url,"target":`_blank`,"rel":`noopener noreferrer`,"aria-label":`Open original`,"title":`Open original (v)`,});
           { const $$_nocI3C = __sveltets_2_ensureComponent(Icon); new $$_nocI3C({ target: __sveltets_2_any(), props: {  "name":`external`,}});}
         }
      }
       { svelteHTML.createElement("div", { "class":`aa`,});
         { svelteHTML.createElement("button", {           "class":`icon-btn`,"aria-label":`Reading settings`,"aria-expanded":app.readingOpen,"title":`Reading settings`,"onclick":() => (app.readingOpen = !app.readingOpen),});
           { const $$_nocI4C = __sveltets_2_ensureComponent(Icon); new $$_nocI4C({ target: __sveltets_2_any(), props: {  "name":`type`,}});}
         }
        if(app.readingOpen){ { const $$_sgnitteSgnidaeR3C = __sveltets_2_ensureComponent(ReadingSettings); new $$_sgnitteSgnidaeR3C({ target: __sveltets_2_any(), props: {}});}}
       }
     }
     { svelteHTML.createElement("div", {    "class":`progress`,"aria-hidden":`true`,});__sveltets_2_ensureType(String, Number, progress); }

     { const $$_div1 = svelteHTML.createElement("div", {      "class":`scroll`,"onscroll":onScroll,"tabindex":-1,});scroller = $$_div1;
       { svelteHTML.createElement("div", { "class":`column`,});
         { svelteHTML.createElement("header", {});
           { svelteHTML.createElement("p", { "class":`byline`,});
            if(feed){ { const $$_nocIdeeF5C = __sveltets_2_ensureComponent(FeedIcon); new $$_nocIdeeF5C({ target: __sveltets_2_any(), props: {      "url":feed.iconUrl,"title":feed.title,"size":16,}});}}
             { svelteHTML.createElement("span", { "class":`source`,});feed?.title ?? ''; }
            if(a.entry.author && a.entry.author !== feed?.title){ { svelteHTML.createElement("span", {}); a.entry.author; }}
           }
           { svelteHTML.createElement("h1", {});
            if(a.entry.url){
               { svelteHTML.createElement("a", {     "href":a.entry.url,"target":`_blank`,"rel":`noopener noreferrer`,});a.entry.title || 'Untitled'; }
            }else{
              a.entry.title || 'Untitled';
            }
           }
           { svelteHTML.createElement("p", { "class":`dateline`,});
             { svelteHTML.createElement("time", { "datetime":new Date(a.entry.publishedAt * 1000).toISOString(),});longDate(a.entry.publishedAt); }
            if(minutes){ { svelteHTML.createElement("span", {});minutes;   }}
           }
         }

           for(let en of __sveltets_2_ensureArray(audio)){en.url;
           { svelteHTML.createElement("audio", {     "class":`media`,"controls":true,"preload":`none`,"src":en.url,}); }
        }
           for(let en of __sveltets_2_ensureArray(video)){en.url;
          
           { svelteHTML.createElement("video", {     "class":`media`,"controls":true,"preload":`none`,"src":en.url,}); }
        }

        if(app.articleError){
           { svelteHTML.createElement("p", { "class":`note`,});    app.articleError; }
        } else if (a.entry.content === undefined){
           { svelteHTML.createElement("div", {   "class":`skeleton`,"aria-label":`Loading article`,});
             { svelteHTML.createElement("span", {}); } { svelteHTML.createElement("span", {}); } { svelteHTML.createElement("span", {}); } { svelteHTML.createElement("span", { "class":`short`,}); }
           }
        } else if (a.entry.content){
          
           { svelteHTML.createElement("div", { "class":`prose`,}); a.entry.content; }
        }else{
           { svelteHTML.createElement("p", { "class":`note`,});      { svelteHTML.createElement("a", {     "href":a.entry.url,"target":`_blank`,"rel":`noopener noreferrer`,});     }  }
        }

         { svelteHTML.createElement("footer", { "class":`end`,});
           { svelteHTML.createElement("span", {   "class":`end-mark`,"aria-hidden":`true`,}); }
          if(app.selectedIndex >= 0 && app.selectedIndex < app.entries.length - 1){
            const next = app.entries[app.selectedIndex + 1];
             { svelteHTML.createElement("button", {   "class":`next`,"onclick":() => app.move(1),});
               { svelteHTML.createElement("span", { "class":`next-label`,});  { svelteHTML.createElement("kbd", {});  } }
               { svelteHTML.createElement("span", { "class":`next-title`,});next.title; }
             }
          }
         }
       }
     }
  }else{
     { svelteHTML.createElement("div", { "class":`blank`,});
       { svelteHTML.createElement("div", { "class":`blank-inner`,});
         { svelteHTML.createElement("span", {   "class":`blank-mark`,"aria-hidden":`true`,}); { svelteHTML.createElement("i", {}); } { svelteHTML.createElement("b", {}); } { svelteHTML.createElement("b", {}); } }
         { svelteHTML.createElement("p", {});      }
         { svelteHTML.createElement("p", { "class":`hint`,}); { svelteHTML.createElement("kbd", {});  }  { svelteHTML.createElement("kbd", {});  }    { svelteHTML.createElement("kbd", {});  }     { svelteHTML.createElement("kbd", {});  }    }
       }
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {} as any as { pageDown: typeof pageDown }, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Article__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Article__SvelteComponent_ = ReturnType<typeof Article__SvelteComponent_>;
/*Ωignore_endΩ*/export default Article__SvelteComponent_;