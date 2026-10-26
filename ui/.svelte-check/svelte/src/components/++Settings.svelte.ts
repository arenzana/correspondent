///<reference types="svelte" />
;
import { app } from '../lib/state.svelte'
import { api } from '../lib/api'
import Modal from './Modal.svelte';
function $$render() {

  
  
  

  let apiPassword = $state('')
  let apiMessage = $state('')
  let importMessage = $state('')
  let fileInput: HTMLInputElement | undefined = $state()

  const readerURL = `${location.origin}/api/greader.php`

  async function saveApiPassword(e: SubmitEvent) {
    e.preventDefault()
    try {
      await api.setApiPassword(apiPassword)
      app.hasApiPassword = true
      apiPassword = ''
      apiMessage = 'Saved. Use it as the password in Reeder.'
    } catch (err) {
      apiMessage = (err as Error).message
    }
  }

  async function importFile() {
    const file = fileInput?.files?.[0]
    if (!file) return
    importMessage = 'Importing…'
    try {
      const { added } = await api.importOpml(file)
      importMessage = `Added ${added} ${added === 1 ? 'feed' : 'feeds'}. They'll fill in over the next minute.`
      await app.refreshTree()
    } catch (err) {
      importMessage = (err as Error).message
    }
  }
;
async () => {

 { const $$_ladoM0C = __sveltets_2_ensureComponent(Modal); new $$_ladoM0C({ target: __sveltets_2_any(), props: {     children:() => { return __sveltets_2_any(0); },"label":`Settings`,"onclose":() => (app.settingsOpen = false),"width":560,}});
   { svelteHTML.createElement("div", { "class":`wrap`,});
     { svelteHTML.createElement("h2", {});  }

     { svelteHTML.createElement("section", {});
       { svelteHTML.createElement("h3", {});  }
       { svelteHTML.createElement("label", { "class":`check`,});
         { svelteHTML.createElement("input", {      "type":`checkbox`,"checked":app.prefs.markOnOpen,"onchange":(e) => app.setPrefs({ markOnOpen: e.currentTarget.checked }),});}
         { svelteHTML.createElement("span", {});       }
       }
     }

     { svelteHTML.createElement("section", {});
       { svelteHTML.createElement("h3", {});    }
       { svelteHTML.createElement("p", {});
               { svelteHTML.createElement("code", {});readerURL; }   { svelteHTML.createElement("strong", {});app.user; }     
              
       }
       { svelteHTML.createElement("form", {   "onsubmit":saveApiPassword,"class":`row`,});
         { svelteHTML.createElement("input", {             "class":`field`,"type":`password`,"autocomplete":`new-password`,"minlength":10,"required":true,"placeholder":app.hasApiPassword ? 'Set a new app password' : 'Choose an app password',"bind:value":apiPassword,});/*Ωignore_startΩ*/() => apiPassword = __sveltets_2_any(null);/*Ωignore_endΩ*/}
         { svelteHTML.createElement("button", { "class":`btn`,});  }
       }
      if(apiMessage){ { svelteHTML.createElement("p", {   "class":`msg`,"role":`status`,});apiMessage; }}
     }

     { svelteHTML.createElement("section", {});
       { svelteHTML.createElement("h3", {});  }
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("button", {   "class":`btn`,"onclick":() => fileInput?.click(),});  }
         { svelteHTML.createElement("a", {    "class":`btn`,"href":`/api/v1/opml`,"download":true,});  }
         { const $$_input4 = svelteHTML.createElement("input", {        "type":`file`,"accept":`.opml,.xml,text/xml,text/x-opml`,"hidden":true,"onchange":importFile,});fileInput = $$_input4;}
       }
      if(importMessage){ { svelteHTML.createElement("p", {   "class":`msg`,"role":`status`,});importMessage; }}
     }

     { svelteHTML.createElement("section", { "class":`last`,});
       { svelteHTML.createElement("button", {   "class":`btn`,"onclick":() => app.signOut(),});  }
     }
   }
 Modal}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Settings__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Settings__SvelteComponent_ = ReturnType<typeof Settings__SvelteComponent_>;
/*Ωignore_endΩ*/export default Settings__SvelteComponent_;