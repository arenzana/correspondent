<script lang="ts">
  import { app } from '../lib/state.svelte'
</script>

<div class="toasts" role="status" aria-live="polite">
  {#each app.toasts as t (t.id)}
    <div class="toast">
      <span>{t.message}</span>
      {#if t.action}
        <button onclick={() => { t.action?.run(); app.dismiss(t.id) }}>{t.action.label}</button>
      {/if}
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    left: 50%;
    bottom: max(20px, env(safe-area-inset-bottom));
    z-index: 50;
    transform: translateX(-50%);
    display: grid;
    gap: 8px;
    justify-items: center;
    pointer-events: none;
  }
  .toast {
    pointer-events: auto;
    display: flex;
    align-items: center;
    gap: 16px;
    max-width: min(480px, calc(100vw - 32px));
    padding: 10px 14px;
    border-radius: var(--radius-l);
    background: var(--ink);
    color: var(--sheet);
    box-shadow: var(--overlay-shadow);
    animation: up 180ms var(--ease);
  }
  .toast button {
    font-weight: 600;
    color: color-mix(in oklab, var(--accent) 45%, var(--sheet));
  }
  @keyframes up {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
  }
</style>
