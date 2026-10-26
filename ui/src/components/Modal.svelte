<script lang="ts">
  // Accessible modal built on <dialog>: focus is trapped by the browser,
  // Escape closes it, and focus returns to where it was.
  import type { Snippet } from 'svelte'

  let {
    label,
    onclose,
    width = 560,
    align = 'center',
    children,
  }: { label: string; onclose: () => void; width?: number; align?: 'center' | 'top'; children: Snippet } = $props()

  let dialog: HTMLDialogElement | undefined = $state()

  $effect(() => {
    const opener = document.activeElement as HTMLElement | null
    dialog?.showModal()
    return () => opener?.focus?.()
  })
</script>

<dialog
  bind:this={dialog}
  aria-label={label}
  class:top={align === 'top'}
  style:--w="{width}px"
  oncancel={(e) => {
    e.preventDefault()
    onclose()
  }}
  onclick={(e) => e.target === dialog && onclose()}
>
  {@render children()}
</dialog>

<style>
  dialog {
    width: min(var(--w), calc(100vw - 32px));
    max-height: min(680px, calc(100dvh - 48px));
    padding: 0;
    border: 1px solid var(--rule);
    border-radius: var(--radius-l);
    background: var(--sheet);
    color: var(--ink);
    box-shadow: var(--overlay-shadow);
    overflow: hidden;
    animation: rise 160ms var(--ease);
  }
  dialog.top {
    margin-top: min(14vh, 120px);
  }
  dialog::backdrop {
    background: var(--scrim);
    animation: fade 160ms var(--ease);
  }
  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.99);
    }
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
</style>
