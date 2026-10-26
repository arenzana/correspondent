///<reference types="svelte" />
;
import { app } from '../lib/state.svelte';
function $$render() {

  
;
async () => {

 { svelteHTML.createElement("div", {     "class":`toasts`,"role":`status`,"aria-live":`polite`,});
     for(let t of __sveltets_2_ensureArray(app.toasts)){t.id;
     { svelteHTML.createElement("div", { "class":`toast`,});
       { svelteHTML.createElement("span", {});t.message; }
      if(t.action){
         { svelteHTML.createElement("button", { "onclick":() => { t.action?.run(); app.dismiss(t.id) },});t.action.label; }
      }
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const Toasts__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type Toasts__SvelteComponent_ = InstanceType<typeof Toasts__SvelteComponent_>;
/*Ωignore_endΩ*/export default Toasts__SvelteComponent_;