///<reference types="svelte" />
;
import { app } from '../lib/state.svelte'
import Modal from './Modal.svelte';
function $$render() {

  
  

  const groups: { title: string; keys: [string[], string][] }[] = [
    {
      title: 'Reading',
      keys: [
        [['j'], 'Next article'],
        [['k'], 'Previous article'],
        [['n'], 'Next unread'],
        [['p'], 'Previous unread'],
        [['Space'], 'Scroll, then next article'],
        [['⇧', 'Space'], 'Scroll up'],
        [['v'], 'Open original'],
        [['f'], 'Focused reading'],
      ],
    },
    {
      title: 'Articles',
      keys: [
        [['s'], 'Star or unstar'],
        [['m'], 'Mark read or unread'],
        [['⇧', 'A'], 'Mark all read'],
        [['r'], 'Reload list'],
      ],
    },
    {
      title: 'Go to',
      keys: [
        [['g', 'u'], 'Unread'],
        [['g', 't'], 'Today'],
        [['g', 's'], 'Starred'],
        [['g', 'a'], 'All articles'],
        [['⌘', 'K'], 'Jump anywhere'],
        [['/'], 'Search'],
        [['['], 'Toggle sidebar'],
        [['a'], 'Add feed'],
        [['?'], 'This sheet'],
      ],
    },
  ]
;
async () => {

 { const $$_ladoM0C = __sveltets_2_ensureComponent(Modal); new $$_ladoM0C({ target: __sveltets_2_any(), props: {     children:() => { return __sveltets_2_any(0); },"label":`Keyboard shortcuts`,"onclose":() => (app.shortcutsOpen = false),"width":720,}});
   { svelteHTML.createElement("div", { "class":`sheet`,});
     { svelteHTML.createElement("h2", {});  }
     { svelteHTML.createElement("div", { "class":`cols`,});
         for(let g of __sveltets_2_ensureArray(groups)){g.title;
         { svelteHTML.createElement("section", {});
           { svelteHTML.createElement("h3", {});g.title; }
           { svelteHTML.createElement("dl", {});
               for(let [keys, what] of __sveltets_2_ensureArray(g.keys)){what;
               { svelteHTML.createElement("div", {});
                 { svelteHTML.createElement("dt", {});   for(let k of __sveltets_2_ensureArray(keys)){k; { svelteHTML.createElement("kbd", {});k; }} }
                 { svelteHTML.createElement("dd", {});what; }
               }
            }
           }
         }
      }
     }
     { svelteHTML.createElement("p", { "class":`foot`,});
                    { svelteHTML.createElement("kbd", {});  }    
               
     }
   }
 Modal}


};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const Shortcuts__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type Shortcuts__SvelteComponent_ = InstanceType<typeof Shortcuts__SvelteComponent_>;
/*Ωignore_endΩ*/export default Shortcuts__SvelteComponent_;