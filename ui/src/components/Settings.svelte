<script lang="ts">
  import { app, APP_ICONS } from '../lib/state.svelte'
  import { api, type AIProvider } from '../lib/api'
  import { deviceSubscribed, pushSupport, subscribeDevice, unsubscribeDevice } from '../lib/push'
  import Modal from './Modal.svelte'
  import TextControls from './TextControls.svelte'

  let apiPassword = $state('')
  let apiMessage = $state('')
  let importMessage = $state('')
  let fileInput: HTMLInputElement | undefined = $state()

  const readerURL = `${location.origin}/api/greader.php`

  const support = pushSupport()
  let pushOn = $state(false)
  let pushBusy = $state(false)
  let pushMessage = $state('')
  const notifying = $derived(app.feeds.filter((f) => f.notify))
  $effect(() => {
    deviceSubscribed().then((on) => (pushOn = on)).catch(() => {})
  })

  async function togglePush() {
    pushBusy = true
    pushMessage = ''
    try {
      if (pushOn) {
        await unsubscribeDevice()
        pushOn = false
      } else {
        await subscribeDevice()
        pushOn = true
        if (!notifying.length) pushMessage = 'Now choose feeds: open a feed and use the bell in its header.'
      }
    } catch (e) {
      pushMessage = (e as Error).message
    } finally {
      pushBusy = false
    }
  }

  async function testPush() {
    pushMessage = ''
    try {
      const { sent } = await api.pushTest()
      pushMessage = sent ? 'Sent. It should arrive in a few seconds.' : 'No device is registered for notifications.'
    } catch (e) {
      pushMessage = (e as Error).message
    }
  }
  // Released versions link to their notes; builds between releases to the changelog.
  const releaseURL = $derived(
    /^\d+\.\d+\.\d+$/.test(app.version)
      ? `https://github.com/arenzana/correspondent/releases/tag/v${app.version}`
      : 'https://github.com/arenzana/correspondent/blob/main/CHANGELOG.md',
  )

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

  // AI for sites without feeds: the reader's own provider and key.
  let aiProvider = $state<AIProvider>(app.ai?.provider ?? 'anthropic')
  let aiModel = $state(app.ai?.model ?? '')
  let aiBase = $state(app.ai?.baseUrl ?? '')
  let aiKey = $state('')
  let aiBusy = $state(false)
  let aiMessage = $state('')

  async function saveAI(e: SubmitEvent) {
    e.preventDefault()
    aiBusy = true
    aiMessage = ''
    try {
      const { ai } = await api.setAI({ provider: aiProvider, model: aiModel.trim(), baseUrl: aiBase.trim(), key: aiKey.trim() })
      app.ai = ai
      aiModel = ai.model
      aiKey = ''
      aiMessage = 'Saved and checked with the provider.'
    } catch (err) {
      aiMessage = (err as Error).message
    } finally {
      aiBusy = false
    }
  }

  async function removeAI() {
    aiMessage = ''
    try {
      await api.deleteAI()
      app.ai = null
      aiKey = aiModel = aiBase = ''
      aiMessage = 'Removed. Your key is no longer stored.'
    } catch (err) {
      aiMessage = (err as Error).message
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
</script>

<Modal label="Settings" onclose={() => (app.settingsOpen = false)} width={560}>
  <div class="wrap">
    <h2>Settings</h2>

    <section>
      <h3>Reading</h3>
      <label class="check">
        <input type="checkbox" checked={app.prefs.markOnOpen} onchange={(e) => app.setPrefs({ markOnOpen: e.currentTarget.checked })} />
        <span>Mark articles read when I open them</span>
      </label>
    </section>

    <section class="text">
      <h3>Text</h3>
      <TextControls interfaceText />
      <p class="hint">The article settings, with themes, are also under the T button above any article.</p>
    </section>

    <section>
      <h3>Notifications</h3>
      {#if support === 'unsupported'}
        <p>This browser doesn't support notifications.</p>
      {:else}
        <label class="check">
          <input type="checkbox" checked={pushOn} disabled={pushBusy} onchange={togglePush} />
          <span>Notifications on this device</span>
        </label>
        <p class="hint">
          {#if support === 'needs-install'}
            On iPhone and iPad, add Correspondent to your Home Screen first (Share, then Add to Home Screen), and turn this on there.
          {:else}
            Only feeds you choose notify you: open a feed and use the bell in its header.
          {/if}
        </p>
        {#if notifying.length}
          <p class="hint">Notifying: {notifying.map((f) => f.title).join(', ')}.</p>
        {/if}
        {#if pushOn}
          <button class="btn" onclick={testPush}>Send a test notification</button>
        {/if}
        {#if pushMessage}<p class="msg" role="status">{pushMessage}</p>{/if}
      {/if}
    </section>

    <section>
      <h3>App icon</h3>
      <div class="icons" role="radiogroup" aria-label="App icon">
        {#each APP_ICONS as ic (ic.id)}
          <button class="icon-choice" role="radio" aria-checked={app.prefs.appIcon === ic.id}
            onclick={() => app.setPrefs({ appIcon: ic.id })}>
            <img src="/icons/{ic.id}/icon-192.png" alt="" width="56" height="56" />
            <span>{ic.label}</span>
          </button>
        {/each}
      </div>
      <p class="hint">Changes the browser tab and home-screen icon. An installed app updates its icon when your device next checks; on iPhone, add it to the Home Screen again.</p>
    </section>

    {#if app.aiAvailable}
      <section>
        <h3>AI for sites without feeds</h3>
        <p class="hint">
          When a site you add has no feed and its articles can’t be found on the page, your AI writes a parser for it,
          once. Everyone on this server who follows that page shares it, and checking for new articles never uses AI.
          It also reads sites added from Reeder.
        </p>
        <form class="ai" onsubmit={saveAI}>
          <label>
            <span>Provider</span>
            <select class="field" bind:value={aiProvider}>
              <option value="anthropic">Anthropic</option>
              <option value="openai">OpenAI or compatible</option>
            </select>
          </label>
          <label>
            <span>Model</span>
            <input class="field" bind:value={aiModel} autocomplete="off" spellcheck="false"
              placeholder={aiProvider === 'anthropic' ? 'claude-opus-5-5' : 'Model name'} required={aiProvider === 'openai'} />
          </label>
          <label class="wide">
            <span>Address <small>(optional, for a compatible service)</small></span>
            <input class="field" type="url" bind:value={aiBase} autocomplete="off" spellcheck="false"
              placeholder={aiProvider === 'anthropic' ? 'https://api.anthropic.com' : 'https://api.openai.com/v1'} />
          </label>
          <label class="wide">
            <span>API key</span>
            <input class="field" type="password" bind:value={aiKey} autocomplete="off" spellcheck="false"
              placeholder={app.ai?.keyHint ? `Saved key ending ${app.ai.keyHint}` : 'Paste your API key'} required={!app.ai} />
          </label>
          <div class="ai-actions wide">
            <button class="btn primary" disabled={aiBusy}>{aiBusy ? 'Checking…' : 'Save'}</button>
            {#if app.ai}<button type="button" class="btn" onclick={removeAI}>Remove</button>{/if}
          </div>
        </form>
        <p class="hint">The key is checked with the provider, stored encrypted, and never shown again. Up to 20 AI requests a day.</p>
        {#if aiMessage}<p class="msg" role="status">{aiMessage}</p>{/if}
      </section>
    {/if}

    <section>
      <h3>Reeder and other apps</h3>
      <p>
        Add a FreshRSS account with server <code>{readerURL}</code>, username <strong>{app.user}</strong>, and the app password below.
        Your sign-in password never works for apps.
      </p>
      <form onsubmit={saveApiPassword} class="row">
        <input class="field" type="password" autocomplete="new-password" minlength="10" required
          placeholder={app.hasApiPassword ? 'Set a new app password' : 'Choose an app password'} bind:value={apiPassword} />
        <button class="btn">Save</button>
      </form>
      {#if apiMessage}<p class="msg" role="status">{apiMessage}</p>{/if}
    </section>

    <section>
      <h3>Subscriptions</h3>
      <p><button class="btn" onclick={() => { app.settingsOpen = false; app.manage() }}>Manage subscriptions</button></p>
      <div class="row">
        <button class="btn" onclick={() => fileInput?.click()}>Import OPML…</button>
        <a class="btn" href="/api/v1/opml" download>Export OPML</a>
        <input bind:this={fileInput} type="file" accept=".opml,.xml,text/xml,text/x-opml" hidden onchange={importFile} />
      </div>
      {#if importMessage}<p class="msg" role="status">{importMessage}</p>{/if}
    </section>

    <section class="last">
      <button class="btn" onclick={() => app.signOut()}>Sign out</button>
      <p class="about">
        Correspondent {app.version}{#if app.commit}&nbsp;<span title={app.commit}>({app.commit.slice(0, 7)})</span>{/if}.
        <a href={releaseURL} target="_blank" rel="noopener noreferrer">What's new</a>
      </p>
    </section>
  </div>
</Modal>

<style>
  .wrap {
    padding: 22px 24px 20px;
    overflow-y: auto;
    max-height: inherit;
  }
  h2 {
    margin: 0 0 6px;
    font-family: var(--font-read);
    font-size: calc(22px * var(--ui-scale));
    font-weight: 540;
  }
  section {
    padding: 16px 0;
    border-bottom: 1px solid var(--rule);
  }
  .text {
    display: grid;
    gap: 16px;
  }
  .text h3,
  .text .hint {
    margin: 0;
  }
  section.last {
    border: 0;
    padding-bottom: 0;
  }
  h3 {
    margin: 0 0 8px;
    font-size: calc(13px * var(--ui-scale));
    font-weight: 600;
  }
  p {
    margin: 0 0 10px;
    color: var(--slate);
  }
  code {
    font-size: calc(12.5px * var(--ui-scale));
    padding: 1px 5px;
    border-radius: 4px;
    background: var(--fog);
    color: var(--ink);
    word-break: break-all;
  }
  .row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .row .field {
    flex: 1;
    min-width: 200px;
  }
  .row .btn {
    height: 36px;
    text-decoration: none;
  }
  .msg {
    margin: 8px 0 0;
  }
  .icons {
    display: flex;
    gap: 12px;
  }
  .icon-choice {
    display: grid;
    justify-items: center;
    gap: 6px;
    padding: 8px;
    border-radius: var(--radius-l);
    border: 1px solid transparent;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  .icon-choice[aria-checked='true'] {
    border-color: var(--accent);
    background: var(--accent-wash);
    color: var(--ink);
    font-weight: 560;
  }
  .hint {
    margin: 10px 0 0;
    font-size: calc(12.5px * var(--ui-scale));
  }
  .about {
    margin: 16px 0 0;
    font-size: calc(12.5px * var(--ui-scale));
  }
  .ai {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px 12px;
    margin: 12px 0 0;
  }
  .ai label {
    display: grid;
    gap: 4px;
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 560;
  }
  .ai label small {
    font-weight: 400;
    color: var(--slate);
  }
  .ai .wide {
    grid-column: 1 / -1;
  }
  .ai-actions {
    display: flex;
    gap: 8px;
  }
  @media (max-width: 499px) {
    .ai {
      grid-template-columns: 1fr;
    }
  }
  .check {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .check input {
    accent-color: var(--accent);
    width: 16px;
    height: 16px;
  }
</style>
