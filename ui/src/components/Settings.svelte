<script lang="ts">
  import { app } from '../lib/state.svelte'
  import { api } from '../lib/api'
  import Modal from './Modal.svelte'

  let apiPassword = $state('')
  let apiMessage = $state('')
  let importMessage = $state('')
  let fileInput: HTMLInputElement | undefined = $state()

  const readerURL = `${location.origin}/api/greader.php`

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
