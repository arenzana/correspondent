///<reference types="svelte" />
;// Accessible modal built on <dialog>: focus is trapped by the browser,
// Escape closes it, and focus returns to where it was.

import type { Snippet } from 'svelte';

;type $$ComponentProps =  { label: string; onclose: () => void; width?: number; align?: 'center' | 'top'; children: Snippet };function $$render() {

  
  
  

  let {
    label,
    onclose,
    width = 560,
    align = 'center',
    children,
  }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()

  let dialog: HTMLDialogElement | undefined = $state()

  $effect(() => {
    const opener = document.activeElement as HTMLElement | null
    dialog?.showModal()
    return () => opener?.focus?.()
  })
;
async () => {

  { const $$_dialog0 = svelteHTML.createElement("dialog", {         "aria-label":label,"oncancel":(e) => {
    e.preventDefault()
    onclose()
  },"onclick":(e) => e.target === dialog && onclose(),});dialog = $$_dialog0;align === 'top';__sveltets_2_ensureType(String, Number, `${width} `);
  ;__sveltets_2_ensureSnippet(children());
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Modal__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Modal__SvelteComponent_ = ReturnType<typeof Modal__SvelteComponent_>;
/*Ωignore_endΩ*/export default Modal__SvelteComponent_;