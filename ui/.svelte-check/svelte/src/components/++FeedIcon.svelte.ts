///<reference types="svelte" />
;
;type $$ComponentProps =  { url: string; title: string; size?: number };function $$render() {

  // Favicon with a lettered fallback tile. The tile hue is derived from the
  // feed title so each feed keeps a stable, muted colour.
  let { url, title, size = 16 }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()
  let failed = $state(false)

  const letter = $derived((title.match(/[\p{L}\p{N}]/u)?.[0] ?? '·').toUpperCase())
  const hue = $derived([...title].reduce((h, c) => (h * 31 + c.charCodeAt(0)) % 360, 7))
;
async () => {

if(url && !failed){
   { svelteHTML.createElement("img", {                 "class":`icon`,"src":url,"alt":"","width":size,"height":size,"loading":`lazy`,"referrerpolicy":`no-referrer`,"onerror":() => (failed = true),});__sveltets_2_ensureType(String, Number, `${size} `);}
}else{
   { svelteHTML.createElement("span", {     "class":`tile`,"aria-hidden":`true`,});__sveltets_2_ensureType(String, Number, `${size} `);__sveltets_2_ensureType(String, Number, hue);letter; }
}


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const FeedIcon__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type FeedIcon__SvelteComponent_ = ReturnType<typeof FeedIcon__SvelteComponent_>;
/*Ωignore_endΩ*/export default FeedIcon__SvelteComponent_;