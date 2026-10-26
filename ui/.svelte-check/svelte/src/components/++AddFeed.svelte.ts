///<reference types="svelte" />
;
import { app } from '../lib/state.svelte'
import { api, type Candidate } from '../lib/api'
import { relative } from '../lib/time'
import Modal from './Modal.svelte'
import FeedIcon from './FeedIcon.svelte';
function $$render() {

  
  
  
  
  

  let url = $state('')
  let busy = $state(false)
  let error = $state('')
  let candidates = $state<Candidate[]>([])
  let chosen = $state(0)
  let name = $state('')
  let folder = $state<string>(app.source.kind === 'folder' ? String(app.source.id) : '')
  let newFolder = $state('')

  const close = () => (app.addFeedOpen = false)

  async function find(e: SubmitEvent) {
    e.preventDefault()
    if (!url.trim()) return
    busy = true
    error = ''
    candidates = []
    try {
      candidates = (await api.discover(url.trim())).candidates
      chosen = 0
      name = candidates[0]?.title ?? ''
    } catch (err) {
      error = (err as Error).message
    } finally {
      busy = false
    }
  }

  async function subscribe() {
    const c = candidates[chosen]
    if (!c) return
    busy = true
    error = ''
    try {
      const folderId = folder && folder !== 'new' ? Number(folder) : null
      const { id } = await api.subscribe(c.url, name.trim() || c.title, folderId, folder === 'new' ? newFolder.trim() : undefined)
      await app.refreshTree()
      close()
      app.go({ kind: 'feed', id })
      app.toast(`Subscribed to ${name.trim() || c.title}.`)
    } catch (err) {
      error = (err as Error).message
      busy = false
    }
  }
;
async () => {

 { const $$_ladoM0C = __sveltets_2_ensureComponent(Modal); new $$_ladoM0C({ target: __sveltets_2_any(), props: {     children:() => { return __sveltets_2_any(0); },"label":`Add feed`,"onclose":close,"width":560,}});
   { svelteHTML.createElement("div", { "class":`wrap`,});
     { svelteHTML.createElement("h2", {});   }
     { svelteHTML.createElement("form", { "onsubmit":find,});
       { svelteHTML.createElement("label", { "for":`feed-url`,});    }
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("input", {              "id":`feed-url`,"class":`field`,"type":`url`,"inputmode":`url`,"placeholder":`example.com or https://example.com/feed.xml`,"bind:value":url,"required":true,"autocomplete":`off`,});/*Ωignore_startΩ*/() => url = __sveltets_2_any(null);/*Ωignore_endΩ*/}
         { svelteHTML.createElement("button", {   "class":`btn`,"disabled":busy,});busy && !candidates.length ? 'Looking…' : 'Find feeds'; }
       }
     }

    if(error){ { svelteHTML.createElement("p", {   "class":`error`,"role":`alert`,});error; }}

    if(candidates.length){
       { svelteHTML.createElement("fieldset", { "class":`found`,});
         { svelteHTML.createElement("legend", {});candidates.length === 1 ? 'Found one feed' : `Found ${candidates.length} feeds`; }
            for(let c of __sveltets_2_ensureArray(candidates)){let i = 1;c.url;
           { svelteHTML.createElement("label", {  "class":`cand`,});chosen === i;
             { svelteHTML.createElement("input", {         "type":`radio`,"name":`cand`,"value":i,"onchange":() => (name = c.title),});chosen = __sveltets_2_any(null);}
             { const $$_nocIdeeF4C = __sveltets_2_ensureComponent(FeedIcon); new $$_nocIdeeF4C({ target: __sveltets_2_any(), props: {      "url":c.iconUrl,"title":c.title,"size":20,}});}
             { svelteHTML.createElement("span", { "class":`cand-body`,});
               { svelteHTML.createElement("span", { "class":`cand-title`,});c.title || c.url; }
              if(c.description){ { svelteHTML.createElement("span", { "class":`cand-desc`,});c.description; }}
               { svelteHTML.createElement("span", { "class":`cand-meta`,});
                c.itemCount;  c.itemCount === 1 ? 'article' : 'articles';if(c.latestAt){  relative(c.latestAt);}
               }
             }
           }
        }
       }

       { svelteHTML.createElement("div", { "class":`grid`,});
         { svelteHTML.createElement("label", {});
           { svelteHTML.createElement("span", {});  }
           { svelteHTML.createElement("input", {    "class":`field`,"bind:value":name,});/*Ωignore_startΩ*/() => name = __sveltets_2_any(null);/*Ωignore_endΩ*/}
         }
         { svelteHTML.createElement("label", {});
           { svelteHTML.createElement("span", {});  }
           { svelteHTML.createElement("select", {   "class":`field`,"bind:value":folder,});/*Ωignore_startΩ*/() => folder = __sveltets_2_any(null);/*Ωignore_endΩ*/
             { svelteHTML.createElement("option", {"value":"",});  }
               for(let f of __sveltets_2_ensureArray(app.folders)){f.id; { svelteHTML.createElement("option", { "value":String(f.id),});f.name; }}
             { svelteHTML.createElement("option", { "value":`new`,});  }
           }
         }
        if(folder === 'new'){
           { svelteHTML.createElement("label", { "class":`wide`,});
             { svelteHTML.createElement("span", {});   }
             { svelteHTML.createElement("input", {    "class":`field`,"bind:value":newFolder,});/*Ωignore_startΩ*/() => newFolder = __sveltets_2_any(null);/*Ωignore_endΩ*/}
           }
        }
       }
    }

     { svelteHTML.createElement("div", { "class":`actions`,});
       { svelteHTML.createElement("button", {   "class":`btn`,"onclick":close,});  }
       { svelteHTML.createElement("button", {     "class":`btn primary`,"disabled":!candidates.length || busy || (folder === 'new' && !newFolder.trim()),"onclick":subscribe,});
        busy && candidates.length ? 'Subscribing…' : 'Subscribe';
       }
     }
   }
 Modal}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const AddFeed__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type AddFeed__SvelteComponent_ = ReturnType<typeof AddFeed__SvelteComponent_>;
/*Ωignore_endΩ*/export default AddFeed__SvelteComponent_;