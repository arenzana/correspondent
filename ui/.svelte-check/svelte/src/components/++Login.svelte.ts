///<reference types="svelte" />
;
import { app } from '../lib/state.svelte';
function $$render() {

  

  let username = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      await app.signIn(username, password)
    } catch (err) {
      error = (err as Error).message
      busy = false
    }
  }
;
async () => {

 { svelteHTML.createElement("main", { "class":`login`,});
   { svelteHTML.createElement("form", {   "onsubmit":submit,"aria-labelledby":`login-title`,});
     { svelteHTML.createElement("div", {   "class":`wire`,"aria-hidden":`true`,}); { svelteHTML.createElement("i", {}); } { svelteHTML.createElement("b", {}); } { svelteHTML.createElement("b", {}); } }
     { svelteHTML.createElement("h1", { "id":`login-title`,});  }
     { svelteHTML.createElement("label", {});
       { svelteHTML.createElement("span", {});  }
      
        { svelteHTML.createElement("input", {      "class":`field`,"bind:value":username,"autocomplete":`username`,"autofocus":true,"required":true,});/*Ωignore_startΩ*/() => username = __sveltets_2_any(null);/*Ωignore_endΩ*/}
     }
     { svelteHTML.createElement("label", {});
       { svelteHTML.createElement("span", {});  }
        { svelteHTML.createElement("input", {        "class":`field`,"type":`password`,"bind:value":password,"autocomplete":`current-password`,"required":true,});/*Ωignore_startΩ*/() => password = __sveltets_2_any(null);/*Ωignore_endΩ*/}
     }
    if(error){ { svelteHTML.createElement("p", {   "class":`error`,"role":`alert`,});error; }}
     { svelteHTML.createElement("button", {   "class":`btn primary`,"disabled":busy,});busy ? 'Signing in…' : 'Sign in'; }
   }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Login__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Login__SvelteComponent_ = ReturnType<typeof Login__SvelteComponent_>;
/*Ωignore_endΩ*/export default Login__SvelteComponent_;