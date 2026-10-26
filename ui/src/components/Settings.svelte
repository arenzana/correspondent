<script lang="ts">
  import { app, APP_ICONS } from '../lib/state.svelte'
  import { api } from '../lib/api'
  import { deviceSubscribed, pushSupport, subscribeDevice, unsubscribeDevice } from '../lib/push'
  import Modal from './Modal.svelte'

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
    font-size: 22px;
    font-weight: 540;
  }
  section {
    padding: 16px 0;
    border-bottom: 1px solid var(--rule);
  }
  section.last {
    border: 0;
    padding-bottom: 0;
  }
  h3 {
    margin: 0 0 8px;
    font-size: 13px;
    font-weight: 600;
  }
  p {
    margin: 0 0 10px;
    color: var(--slate);
  }
  code {
    font-size: 12.5px;
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
    font-size: 12.5px;
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
    font-size: 12.5px;
  }
  .about {
    margin: 16px 0 0;
    font-size: 12.5px;
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
