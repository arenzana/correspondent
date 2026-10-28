<script lang="ts">
  // The article's Send to… menu: the reader's services, Obsidian on this
  // device, and where each one already has this article.
  import { app } from '../lib/state.svelte'
  import { api, type Delivery } from '../lib/api'

  let el: HTMLElement | undefined = $state()
  let sent = $state<Map<number, Delivery>>(new Map())

  $effect(() => {
    const id = app.article?.entry.id
    if (!id) return
    api
      .deliveries(id)
      .then((r) => (sent = new Map(r.deliveries.map((d) => [d.integrationId, d]))))
      .catch(() => {})
  })

  $effect(() => {
    const close = (e: MouseEvent) => {
      if (el && !el.parentElement?.contains(e.target as Node)) app.sendOpen = false
    }
    // Escape closes only this, as in the reading settings.
    const esc = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      app.sendOpen = false
      e.stopPropagation()
    }
    setTimeout(() => document.addEventListener('click', close))
    document.addEventListener('keydown', esc)
    el?.querySelector<HTMLElement>('button')?.focus()
    return () => {
      document.removeEventListener('click', close)
      document.removeEventListener('keydown', esc)
    }
  })

  const status = (d: Delivery | undefined) =>
    !d ? '' : d.state === 'sent' ? 'Sent' : d.state === 'pending' ? 'Sending' : 'Failed'
</script>

<div class="pop" bind:this={el} role="menu" aria-label="Send to">
  {#each app.integrations as i (i.id)}
    {@const d = sent.get(i.id)}
    <button role="menuitem" onclick={() => app.sendTo(i)} title={d?.error ?? ''}>
      <span>{i.name}</span>
      {#if d}<small class={d.state}>{status(d)}</small>{/if}
    </button>
  {/each}
  {#if app.prefs.obsidian?.vault}
    <button role="menuitem" onclick={() => app.sendToObsidian()}>
      <span>Obsidian</span><small>{app.prefs.obsidian.vault}</small>
    </button>
  {/if}
  {#if app.integrations.length || app.prefs.obsidian?.vault}<hr />{/if}
  <button role="menuitem" class="quiet" onclick={() => ((app.sendOpen = false), app.openSettings('integrations'))}>
    {app.integrations.length || app.prefs.obsidian?.vault ? 'Manage services…' : 'Connect a service…'}
  </button>
</div>

<style>
  .pop {
    position: absolute;
    right: 0;
    top: 40px;
    z-index: 20;
    min-width: 240px;
    padding: 6px;
    display: grid;
    background: var(--sheet);
    border: 1px solid var(--rule);
    border-radius: var(--radius-l);
    box-shadow: var(--overlay-shadow);
    animation: pop 140ms var(--ease);
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pop {
      animation: none;
    }
  }
  button {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 16px;
    padding: 9px 10px;
    border: 0;
    border-radius: var(--radius-s);
    background: none;
    color: var(--ink);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  button:hover,
  button:focus-visible {
    background: var(--fog);
  }
  .quiet {
    color: var(--slate);
  }
  small {
    color: var(--slate);
    font-size: 0.8em;
  }
  small.sent {
    color: var(--accent);
  }
  small.failed {
    color: var(--seal);
  }
  hr {
    border: 0;
    border-top: 1px solid var(--rule);
    margin: 4px 2px;
  }
</style>
