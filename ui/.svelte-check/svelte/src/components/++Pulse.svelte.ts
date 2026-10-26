///<reference types="svelte" />
;
;type $$ComponentProps =  { days: number[]; label: string };function $$render() {

  // Fourteen-day publishing cadence, oldest first. Quiet by design: it is a
  // texture that tells you which feeds are alive, not a chart to read.
  let { days, label }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()
  const peak = $derived(Math.max(1, ...days))
  const total = $derived(days.reduce((a, b) => a + b, 0))
;
async () => {

 { svelteHTML.createElement("span", {     "class":`pulse`,"role":`img`,"aria-label":`${label}: ${total} articles in the last 14 days`,});
      for(let n of __sveltets_2_ensureArray(days)){let i = 1;i;
     { svelteHTML.createElement("span", {   "class":`bar`,});n === 0;__sveltets_2_ensureType(String, Number, n / peak); }
  }
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Pulse__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Pulse__SvelteComponent_ = ReturnType<typeof Pulse__SvelteComponent_>;
/*Ωignore_endΩ*/export default Pulse__SvelteComponent_;