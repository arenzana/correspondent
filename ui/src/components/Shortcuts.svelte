<script lang="ts">
  import { app } from '../lib/state.svelte'
  import Modal from './Modal.svelte'

  const groups: { title: string; keys: [string[], string][] }[] = [
    {
      title: 'Reading',
      keys: [
        [['j'], 'Next article'],
        [['k'], 'Previous article'],
        [['n'], 'Next unread'],
        [['p'], 'Previous unread'],
        [['Space'], 'Scroll, then next article'],
        [['⇧', 'Space'], 'Scroll up'],
        [['v'], 'Open original'],
        [['l'], 'Listen, or play and pause'],
        [['⇧', 'L'], 'Listen to unread articles with audio'],
        [['f'], 'Focused reading'],
      ],
    },
    {
      title: 'Articles',
      keys: [
        [['s'], 'Star or unstar'],
        [['m'], 'Mark read or unread'],
        [['⇧', 'A'], 'Mark all read'],
        [['r'], 'Reload list'],
      ],
    },
    {
      title: 'Go to',
      keys: [
        [['g', 'u'], 'Unread'],
        [['g', 't'], 'Today'],
        [['g', 's'], 'Starred'],
        [['g', 'a'], 'All articles'],
        [['g', 'm'], 'Manage subscriptions'],
        [[','], 'Settings'],
        [['⌘', 'K'], 'Jump anywhere'],
        [['/'], 'Search'],
        [['['], 'Show or hide feeds'],
        [['a'], 'Add feed'],
        [['?'], 'This sheet'],
      ],
    },
  ]
</script>

<Modal label="Keyboard shortcuts" onclose={() => (app.shortcutsOpen = false)} width={720}>
  <div class="sheet">
    <h2>Keyboard shortcuts</h2>
    <div class="cols">
      {#each groups as g (g.title)}
        <section>
          <h3>{g.title}</h3>
          <dl>
            {#each g.keys as [keys, what] (what)}
              <div>
                <dt>{#each keys as k (k)}<kbd>{k}</kbd>{/each}</dt>
                <dd>{what}</dd>
              </div>
            {/each}
          </dl>
        </section>
      {/each}
    </div>
    <p class="foot">
      Opening an article marks it read after a moment, so moving quickly with <kbd>j</kbd> doesn't use articles up.
      Read articles stay in the list until you reload it.
    </p>
  </div>
</Modal>

<style>
  .sheet {
    padding: 22px 24px 20px;
    overflow-y: auto;
    max-height: inherit;
  }
  h2 {
    margin: 0 0 18px;
    font-family: var(--font-read);
    font-size: calc(22px * var(--ui-scale));
    font-weight: 540;
  }
  .cols {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
    gap: 20px 28px;
  }
  h3 {
    margin: 0 0 8px;
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 600;
    color: var(--slate);
  }
  dl {
    margin: 0;
    display: grid;
    gap: 6px;
  }
  dl div {
    display: grid;
    grid-template-columns: 64px 1fr;
    align-items: center;
    gap: 10px;
  }
  dt {
    display: flex;
    gap: 3px;
  }
  dd {
    margin: 0;
  }
  .foot {
    margin: 20px 0 0;
    padding-top: 14px;
    border-top: 1px solid var(--rule);
    color: var(--slate);
    font-size: calc(13px * var(--ui-scale));
  }
</style>
