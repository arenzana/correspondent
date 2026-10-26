///<reference types="svelte" />
;
import { app, type Theme } from '../lib/state.svelte'
import { clock } from '../lib/time';
function $$render() {

  
  

  // Swatches show each theme's own sheet and ink, independent of the current theme.
  const themes: { id: Theme; label: string; bg: string; fg: string }[] = [
    { id: 'auto', label: 'System', bg: 'linear-gradient(135deg, #ffffff 50%, #161d2a 50%)', fg: '#8d97ab' },
    { id: 'sun', label: 'Sun', bg: 'linear-gradient(135deg, #ffffff 50%, #f4ecdc 50%)', fg: '#6e604b' },
    { id: 'day', label: 'Day', bg: '#ffffff', fg: '#18213a' },
    { id: 'night', label: 'Night desk', bg: '#161d2a', fg: '#e3e8f2' },
    { id: 'evening', label: 'Evening', bg: '#f4ecdc', fg: '#2e2416' },
  ]

  let el: HTMLElement | undefined = $state()

  $effect(() => {
    const close = (e: MouseEvent) => {
      if (el && !el.parentElement?.contains(e.target as Node)) app.readingOpen = false
    }
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && (app.readingOpen = false)
    setTimeout(() => document.addEventListener('click', close))
    document.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('click', close)
      document.removeEventListener('keydown', esc)
    }
  })
;
async () => {

 { const $$_div0 = svelteHTML.createElement("div", {      "class":`pop`,"role":`dialog`,"aria-label":`Reading settings`,});el = $$_div0;
   { svelteHTML.createElement("fieldset", {});
     { svelteHTML.createElement("legend", {});  }
     { svelteHTML.createElement("div", { "class":`themes`,});
         for(let t of __sveltets_2_ensureArray(themes)){t.id;
         { svelteHTML.createElement("button", {     "class":`theme`,"aria-pressed":app.prefs.theme === t.id,"onclick":() => (t.id === 'sun' ? app.useSunTheme() : app.setPrefs({ theme: t.id })),});
           { svelteHTML.createElement("span", {   "class":`swatch`,});__sveltets_2_ensureType(String, Number, t.bg);__sveltets_2_ensureType(String, Number, t.fg);  }
          t.label;
         }
      }
     }
    if(app.prefs.theme === 'sun' && app.sunNext){
       { svelteHTML.createElement("p", { "class":`sun-note`,});
        app.sunNext.theme === 'evening' ? 'Evening' : 'Day';  clock(app.sunNext.at);if(app.prefs.place?.estimated){
              }
       }
    }
   }

   { svelteHTML.createElement("fieldset", {});
     { svelteHTML.createElement("legend", {});  }
     { svelteHTML.createElement("div", { "class":`seg`,});
       { svelteHTML.createElement("button", {     "aria-pressed":app.prefs.readFont === 'serif',"class":`serif`,"onclick":() => app.setPrefs({ readFont: 'serif' }),});  }
       { svelteHTML.createElement("button", {   "aria-pressed":app.prefs.readFont === 'sans',"onclick":() => app.setPrefs({ readFont: 'sans' }),});  }
     }
   }

   { svelteHTML.createElement("label", { "class":`slider`,});
     { svelteHTML.createElement("span", {});   { svelteHTML.createElement("output", {});app.prefs.readSize;  } }
     { svelteHTML.createElement("input", {            "type":`range`,"min":`15`,"max":`24`,"step":`1`,"value":app.prefs.readSize,"oninput":(e) => app.setPrefs({ readSize: Number(e.currentTarget.value) }),});}
   }

   { svelteHTML.createElement("label", { "class":`slider`,});
     { svelteHTML.createElement("span", {});   { svelteHTML.createElement("output", {});app.prefs.measure;  } }
     { svelteHTML.createElement("input", {            "type":`range`,"min":`54`,"max":`84`,"step":`2`,"value":app.prefs.measure,"oninput":(e) => app.setPrefs({ measure: Number(e.currentTarget.value) }),});}
   }

   { svelteHTML.createElement("fieldset", {});
     { svelteHTML.createElement("legend", {});  }
     { svelteHTML.createElement("div", { "class":`seg`,});
       { svelteHTML.createElement("button", {   "aria-pressed":app.prefs.density === 'comfortable',"onclick":() => app.setPrefs({ density: 'comfortable' }),});  }
       { svelteHTML.createElement("button", {   "aria-pressed":app.prefs.density === 'compact',"onclick":() => app.setPrefs({ density: 'compact' }),});  }
     }
   }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const ReadingSettings__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type ReadingSettings__SvelteComponent_ = ReturnType<typeof ReadingSettings__SvelteComponent_>;
/*Ωignore_endΩ*/export default ReadingSettings__SvelteComponent_;