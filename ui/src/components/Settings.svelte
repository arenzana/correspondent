<script lang="ts">
  import { app, APP_ICONS, UI_SCALES } from '../lib/state.svelte'
  import { api, type AIProvider, type SpeechProvider } from '../lib/api'
  import { deviceSubscribed, pushSupport, subscribeDevice, unsubscribeDevice } from '../lib/push'
  import Icon from './Icon.svelte'
  import ThemePicker from './ThemePicker.svelte'
  import { relative } from '../lib/time'
  import { addPasskey, passkeysSupported } from '../lib/passkey'
  import type { APIToken, Passkey, Session } from '../lib/api'
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

  // Read aloud: the reader's own text-to-speech provider and key.
  const voices: Record<SpeechProvider, { model: string; voice: string; voiceHint: string }> = {
    openai: { model: 'gpt-4o-mini-tts', voice: 'alloy', voiceHint: 'alloy, ash, coral, echo, nova, onyx, sage, shimmer…' },
    elevenlabs: { model: 'eleven_multilingual_v2', voice: 'JBFqnCBsd6RMkjVDRZzb', voiceHint: 'A voice ID from your ElevenLabs voice library' },
    deepgram: { model: 'aura-2-thalia-en', voice: '', voiceHint: '' },
  }
  let speechProvider = $state<SpeechProvider>(app.speech?.provider ?? 'openai')
  let speechModel = $state(app.speech?.model ?? '')
  let speechVoice = $state(app.speech?.voice ?? '')
  let speechBase = $state(app.speech?.baseUrl ?? '')
  let speechKey = $state('')
  let speechBusy = $state(false)
  let speechMessage = $state('')

  async function saveSpeech(e: SubmitEvent) {
    e.preventDefault()
    speechBusy = true
    speechMessage = ''
    try {
      const { speech } = await api.setSpeech({
        provider: speechProvider,
        model: speechModel.trim(),
        voice: speechProvider === 'deepgram' ? '' : speechVoice.trim(),
        baseUrl: speechProvider === 'openai' ? speechBase.trim() : '',
        key: speechKey.trim(),
      })
      app.speech = speech
      speechModel = speech.model
      speechVoice = speech.voice ?? ''
      speechKey = ''
      speechMessage = 'Saved and checked with the provider.'
    } catch (err) {
      speechMessage = (err as Error).message
    } finally {
      speechBusy = false
    }
  }

  async function removeSpeech() {
    speechMessage = ''
    try {
      await api.deleteSpeech()
      app.speech = null
      speechKey = speechModel = speechVoice = speechBase = ''
      speechMessage = 'Removed. Your key is no longer stored. Audio already made stays playable.'
    } catch (err) {
      speechMessage = (err as Error).message
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

  const sections = [
    { id: 'account', label: 'Account' },
    { id: 'appearance', label: 'Appearance' },
    { id: 'reading', label: 'Reading' },
    { id: 'feeds', label: 'Feeds' },
    { id: 'apps', label: 'Apps & sync' },
    { id: 'about', label: 'About' },
  ]
  let scroller: HTMLElement | undefined = $state()
  let current = $state('account')

  function show(id: string) {
    current = id
    scroller?.querySelector(`#settings-${id}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  // The rail follows the section being read: the last one whose heading
  // has reached the top of the page (or the last one, at the very end).
  function track() {
    if (!scroller) return
    const top = scroller.getBoundingClientRect().top + 90
    const els = [...scroller.querySelectorAll<HTMLElement>('section[id^="settings-"]')]
    const atEnd = scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 4
    const passed = els.filter((el) => el.getBoundingClientRect().top <= top)
    const el = atEnd ? els[els.length - 1] : passed[passed.length - 1] ?? els[0]
    if (el) current = el.id.replace('settings-', '')
  }

  // Opened at a section (from Add feed, say).
  $effect(() => {
    const id = app.settingsSection
    if (id && scroller) requestAnimationFrame(() => show(id))
  })

  const feedStats = $derived({
    total: app.feeds.length,
    pages: app.feeds.filter((f) => f.page).length,
    failing: app.feeds.filter((f) => f.errorCount > 0 && !f.paused).length,
    paused: app.feeds.filter((f) => f.paused).length,
    full: app.feeds.filter((f) => f.fullText).length,
  })

  let copied = $state(false)
  async function copyServer() {
    try {
      await navigator.clipboard.writeText(readerURL)
      copied = true
      setTimeout(() => (copied = false), 1600)
    } catch {
      /* the address stays visible to copy by hand */
    }
  }

  // Account: password, passkeys, sessions.
  let pwCurrent = $state('')
  let pwNew = $state('')
  let pwMessage = $state('')
  async function changePassword(e: SubmitEvent) {
    e.preventDefault()
    pwMessage = ''
    try {
      await api.changePassword(pwCurrent, pwNew)
      pwCurrent = pwNew = ''
      pwMessage = 'Password changed. Other browsers were signed out.'
      loadAccount()
    } catch (err) {
      pwMessage = (err as Error).message
    }
  }

  let keys = $state<Passkey[]>([])
  let sessionList = $state<Session[]>([])
  let keyMessage = $state('')
  let renaming = $state<number | null>(null)
  const canPasskey = passkeysSupported()
  // Personal API tokens: corr, scripts, and AI tools over MCP.
  let tokens = $state<APIToken[]>([])
  let tokenName = $state('')
  let tokenScope = $state<'read' | 'write'>('read')
  let newToken = $state('')
  let tokenCopied = $state(false)
  const mcpURL = `${location.origin}/mcp`
  async function createToken(e: SubmitEvent) {
    e.preventDefault()
    try {
      const r = await api.createToken(tokenName.trim() || 'Token', tokenScope)
      newToken = r.token
      tokenName = ''
      loadAccount()
    } catch (err) {
      app.toast((err as Error).message)
    }
  }
  async function copyToken() {
    try {
      await navigator.clipboard.writeText(newToken)
      tokenCopied = true
      setTimeout(() => (tokenCopied = false), 1600)
    } catch {
      /* visible to copy by hand */
    }
  }
  async function revoke(t: APIToken) {
    await api.revokeToken(t.id).catch((e) => app.toast((e as Error).message))
    loadAccount()
  }

  function loadAccount() {
    api.sessions().then((r) => (sessionList = r.sessions)).catch(() => {})
    api.tokens().then((r) => (tokens = r.tokens)).catch(() => {})
    if (app.passkeysAvailable) api.passkeys().then((r) => (keys = r.passkeys)).catch(() => {})
  }
  $effect(loadAccount)

  async function newPasskey() {
    keyMessage = ''
    try {
      await addPasskey('')
      keyMessage = 'Passkey added. Use it to sign in on this device.'
      loadAccount()
    } catch (err) {
      const e = err as Error
      keyMessage = e.name === 'NotAllowedError' || e.name === 'AbortError' ? '' : e.name === 'InvalidStateError' ? 'This device already has a passkey for your account.' : e.message
    }
  }
  async function renameKey(id: number, name: string) {
    renaming = null
    await api.renamePasskey(id, name).catch((e) => (keyMessage = (e as Error).message))
    loadAccount()
  }
  async function removeKey(k: Passkey) {
    await api.deletePasskey(k.id).catch((e) => (keyMessage = (e as Error).message))
    loadAccount()
  }
  async function endSession(x: Session) {
    await api.endSession(x.id).catch(() => {})
    loadAccount()
  }

  let syncing = $state(false)
  async function syncNow() {
    syncing = true
    try {
      await app.syncOffline()
    } finally {
      syncing = false
    }
  }
</script>

<section class="settings" aria-labelledby="settings-title">
  <header class="head">
    <button class="icon-btn only-narrow" aria-label="Show feeds" onclick={() => app.back()}>
      <Icon name="back" />
    </button>
    <div class="titles">
      <h1 id="settings-title">Settings</h1>
      <p class="sub">Signed in as {app.user}</p>
    </div>
  </header>

  <nav class="chips" aria-label="Settings sections">
    {#each sections as sec (sec.id)}
      <button aria-current={current === sec.id ? 'true' : undefined} onclick={() => show(sec.id)}>{sec.label}</button>
    {/each}
  </nav>

  <div class="body" bind:this={scroller} onscroll={track}>
    <!-- The rail is a short time spine: a dot per section, the current one filled. -->
    <nav class="rail" aria-label="Settings sections">
      <ol>
        {#each sections as sec (sec.id)}
          <li><button aria-current={current === sec.id ? 'true' : undefined} onclick={() => show(sec.id)}><span class="dot" aria-hidden="true"></span>{sec.label}</button></li>
        {/each}
      </ol>
    </nav>

    <div class="content">
      <section id="settings-account">
        <h2>Account</h2>
        <div class="whoami">
          <span class="avatar" aria-hidden="true">{(app.user ?? '?').slice(0, 1).toUpperCase()}</span>
          <span><strong>{app.user}</strong>{#if app.role === 'admin'}<span class="role">Admin</span>{/if}</span>
          {#if app.role === 'admin'}<button class="btn" onclick={() => app.openAdmin()}>Open admin</button>{/if}
        </div>

        {#if app.passkeysAvailable}
          <div class="card">
            <h3>Passkeys</h3>
            <p class="hint">Sign in with Face ID, Touch ID or your device’s screen lock instead of a password. Each device keeps its own.</p>
            {#if keys.length}
              <ul class="list">
                {#each keys as k (k.id)}
                  <li>
                    {#if renaming === k.id}
                      <!-- svelte-ignore a11y_autofocus -->
                      <input class="field" value={k.name} aria-label="Passkey name" autofocus
                        onkeydown={(e) => { if (e.key === 'Enter') renameKey(k.id, e.currentTarget.value); if (e.key === 'Escape') renaming = null }}
                        onblur={(e) => renameKey(k.id, e.currentTarget.value)} />
                    {:else}
                      <span class="item"><strong>{k.name}</strong>
                        <small>Added {relative(k.createdAt)}{#if k.lastUsedAt}, last used {relative(k.lastUsedAt)}{:else}, not used yet{/if}</small></span>
                      <span class="row-actions">
                        <button class="link" onclick={() => (renaming = k.id)}>Rename</button>
                        <button class="link danger" onclick={() => removeKey(k)}>Remove</button>
                      </span>
                    {/if}
                  </li>
                {/each}
              </ul>
            {/if}
            {#if canPasskey}
              <button class="btn" onclick={newPasskey}>Add a passkey on this device</button>
            {:else}
              <p class="hint">This browser can’t create passkeys.</p>
            {/if}
            {#if keyMessage}<p class="msg" role="status">{keyMessage}</p>{/if}
          </div>
        {/if}

        <div class="card">
          <h3>Password</h3>
          <form class="pw" onsubmit={changePassword}>
            <input type="text" autocomplete="username" value={app.user} hidden readonly />
            <input class="field" type="password" bind:value={pwCurrent} autocomplete="current-password" placeholder="Current password" required />
            <input class="field" type="password" bind:value={pwNew} autocomplete="new-password" placeholder="New password (10 characters or more)" minlength="10" required />
            <button class="btn">Change password</button>
          </form>
          {#if pwMessage}<p class="msg" role="status">{pwMessage}</p>{/if}
        </div>

        <div class="card">
          <h3>Command line and AI tools</h3>
          <p class="hint">
            A token lets <code>corr</code>, scripts and AI tools use your account. Read-only tokens can read but never change
            anything: the safe choice for an AI agent. Tokens can’t change your password, passkeys or other tokens.
          </p>
          <form class="token-form" onsubmit={createToken}>
            <input class="field" bind:value={tokenName} maxlength="60" placeholder="What is it for? (e.g. Claude, laptop)" />
            <select class="field" bind:value={tokenScope} aria-label="Access">
              <option value="read">Read only</option>
              <option value="write">Read and change</option>
            </select>
            <button class="btn">Create token</button>
          </form>
          {#if newToken}
            <div class="fresh" role="status">
              <p>Your new token. Copy it now; it won’t be shown again.</p>
              <div class="linkrow">
                <code class="secret">{newToken}</code>
                <button class="btn primary" onclick={copyToken}>{tokenCopied ? 'Copied' : 'Copy'}</button>
                <button class="btn" onclick={() => (newToken = '')}>Done</button>
              </div>
              <p class="hint">
                AI tools: connect to <code>{mcpURL}</code> with the header <code>Authorization: Bearer</code> and this token.
                In Claude Code: <code>claude mcp add --transport http correspondent {mcpURL} --header "Authorization: Bearer {newToken.slice(0, 9)}…"</code>
              </p>
            </div>
          {/if}
          {#if tokens.length}
            <ul class="list">
              {#each tokens as t (t.id)}
                <li>
                  <span class="item"><strong>{t.name}<span class="role">{t.scope === 'read' ? 'Read only' : 'Read and change'}</span></strong>
                    <small>Created {relative(t.createdAt)}{#if t.lastUsedAt}, used {relative(t.lastUsedAt)}{:else}, not used yet{/if}</small></span>
                  <button class="link danger" onclick={() => revoke(t)}>Revoke</button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>

        <div class="card">
          <h3>Signed in</h3>
          <ul class="list">
            {#each sessionList as x (x.id)}
              <li>
                <span class="item"><strong>{x.kind === 'greader' ? 'Reader app' : x.client || 'Browser'}{#if x.current}<span class="role">This device</span>{/if}</strong>
                  <small>Signed in {relative(x.createdAt)}{#if x.lastSeenAt && !x.current}, active {relative(x.lastSeenAt)}{/if}</small></span>
                {#if !x.current}<button class="link danger" onclick={() => endSession(x)}>Sign out</button>{/if}
              </li>
            {/each}
          </ul>
        </div>
      </section>

      <section id="settings-appearance">
        <h2>Appearance</h2>
        <div class="preview wire-preview" aria-hidden="true">
          <span class="pv-day">Today</span>
          <div class="pv-row pv-selected">
            <span class="pv-time">09:41</span><span class="pv-dot"></span>
            <span class="pv-body"><span class="pv-feed">Field Notes</span><span class="pv-title">The patience of a news desk</span>
              <span class="pv-excerpt">Most of the craft is restraint: deciding which small events deserve a second look.</span></span>
          </div>
          <div class="pv-row pv-read">
            <span class="pv-time">08:15</span><span class="pv-dot"></span>
            <span class="pv-body"><span class="pv-feed">Small Systems</span><span class="pv-title">SQLite is enough for your home server</span></span>
          </div>
        </div>
        <ThemePicker />
        <div class="field-group">
          <span class="label">Interface text size</span>
          <div class="seg">
            {#each UI_SCALES as sc (sc.value)}
              <button aria-pressed={app.prefs.uiScale === sc.value} onclick={() => app.setPrefs({ uiScale: sc.value })}>{sc.label}</button>
            {/each}
          </div>
        </div>
        <div class="pair">
          <div class="field-group">
            <span class="label">Interface typeface</span>
            <div class="seg">
              <button aria-pressed={app.prefs.uiFont === 'grotesk'} class="grotesk" onclick={() => app.setPrefs({ uiFont: 'grotesk' })}>Grotesk</button>
              <button aria-pressed={app.prefs.uiFont === 'system'} class="system" onclick={() => app.setPrefs({ uiFont: 'system' })}>System</button>
            </div>
          </div>
          <div class="field-group">
            <span class="label">List density</span>
            <div class="seg">
              <button aria-pressed={app.prefs.density === 'comfortable'} onclick={() => app.setPrefs({ density: 'comfortable' })}>Comfortable</button>
              <button aria-pressed={app.prefs.density === 'compact'} onclick={() => app.setPrefs({ density: 'compact' })}>Compact</button>
            </div>
          </div>
        </div>
        <div class="field-group">
          <span class="label">App icon</span>
          <div class="icons" role="radiogroup" aria-label="App icon">
            {#each APP_ICONS as ic (ic.id)}
              <button class="icon-choice" role="radio" aria-checked={app.prefs.appIcon === ic.id} onclick={() => app.setPrefs({ appIcon: ic.id })}>
                <img src="/icons/{ic.id}/icon-192.png" alt="" width="52" height="52" />
                <span>{ic.label}</span>
              </button>
            {/each}
          </div>
          <p class="hint">The browser tab and home-screen icon. An installed app picks it up when your device next checks; on iPhone, add it to the Home Screen again.</p>
        </div>
      </section>

      <section id="settings-reading">
        <h2>Reading</h2>
        <article class="preview read-preview" aria-hidden="true">
          <h3>Optical sizes, finally on the web</h3>
          <p>Variable fonts with an optical-size axis let one family serve a twelve-pixel caption and a
            forty-eight-pixel headline without compromise. The letters change shape as they grow.</p>
        </article>
        <div class="field-group">
          <span class="label">Article typeface</span>
          <div class="seg">
            <button aria-pressed={app.prefs.readFont === 'serif'} class="serif" onclick={() => app.setPrefs({ readFont: 'serif' })}>Newsreader</button>
            <button aria-pressed={app.prefs.readFont === 'sans'} class="grotesk" onclick={() => app.setPrefs({ readFont: 'sans' })}>Grotesk</button>
          </div>
        </div>
        <div class="pair">
          <label class="field-group">
            <span class="label">Text size <output>{app.prefs.readSize}px</output></span>
            <input type="range" min="15" max="24" step="1" value={app.prefs.readSize} oninput={(e) => app.setPrefs({ readSize: Number(e.currentTarget.value) })} />
          </label>
          <label class="field-group">
            <span class="label">Line length <output>{app.prefs.measure} characters</output></span>
            <input type="range" min="54" max="84" step="2" value={app.prefs.measure} oninput={(e) => app.setPrefs({ measure: Number(e.currentTarget.value) })} />
          </label>
        </div>
        <label class="toggle">
          <span><strong>Mark articles read when I open them</strong></span>
          <input type="checkbox" role="switch" checked={app.prefs.markOnOpen} onchange={(e) => app.setPrefs({ markOnOpen: e.currentTarget.checked })} />
        </label>
        <label class="toggle">
          <span><strong>Show only unread articles in feeds and folders</strong>
            <small>Unread, Today and Starred always show what they say.</small></span>
          <input type="checkbox" role="switch" checked={app.prefs.unreadOnly} onchange={(e) => { app.setPrefs({ unreadOnly: e.currentTarget.checked }); app.load() }} />
        </label>
        <label class="toggle">
          <span><strong>Skip sponsored posts</strong>
            <small>Posts titled "Sponsor:", "[Sponsor]" and the like, or tagged as sponsored, arrive already read, here and in Reeder, and don't notify you. They stay in All.</small></span>
          <input type="checkbox" role="switch" checked={app.prefs.skipSponsored} onchange={(e) => app.setPrefs({ skipSponsored: e.currentTarget.checked })} />
        </label>

        {#if app.aiAvailable}
          <div class="card">
            <h3>Read aloud</h3>
            <p class="hint">
              Listen to any article in a natural voice, with your own text-to-speech key. Press <kbd>l</kbd> or the
              headphones in an article. Audio is shared: once anyone here has had an article read aloud, everyone who
              follows it can listen without using their key. It also plays in Reeder, as the article’s audio.
            </p>
            <form class="ai" onsubmit={saveSpeech}>
              <label>
                <span>Provider</span>
                <select class="field" bind:value={speechProvider}>
                  <option value="openai">OpenAI or compatible</option>
                  <option value="elevenlabs">ElevenLabs</option>
                  <option value="deepgram">Deepgram</option>
                </select>
              </label>
              <label>
                <span>{speechProvider === 'deepgram' ? 'Voice model' : 'Model'}</span>
                <input class="field" bind:value={speechModel} autocomplete="off" spellcheck="false" placeholder={voices[speechProvider].model} />
              </label>
              {#if speechProvider !== 'deepgram'}
                <label class="wide">
                  <span>Voice</span>
                  <input class="field" bind:value={speechVoice} autocomplete="off" spellcheck="false"
                    placeholder={voices[speechProvider].voice} title={voices[speechProvider].voiceHint} />
                </label>
              {/if}
              {#if speechProvider === 'openai'}
                <label class="wide">
                  <span>Address <small>(optional, for a compatible service)</small></span>
                  <input class="field" type="url" bind:value={speechBase} autocomplete="off" spellcheck="false" placeholder="https://api.openai.com/v1" />
                </label>
              {/if}
              <label class="wide">
                <span>API key</span>
                <input class="field" type="password" bind:value={speechKey} autocomplete="off" spellcheck="false"
                  placeholder={app.speech?.keyHint ? `Saved key ending ${app.speech.keyHint}` : 'Paste your API key'} required={!app.speech} />
              </label>
              <div class="actions-row wide">
                <button class="btn primary" disabled={speechBusy}>{speechBusy ? 'Checking…' : 'Save'}</button>
                {#if app.speech}<button type="button" class="btn" onclick={removeSpeech}>Remove</button>{/if}
              </div>
            </form>
            <p class="hint">
              The key is checked by speaking one word, stored encrypted, and never shown again. English voices; up to
              60 articles a day. Leave model and voice empty for the defaults.
            </p>
            {#if speechMessage}<p class="msg" role="status">{speechMessage}</p>{/if}
          </div>
        {/if}
      </section>

      <section id="settings-feeds">
        <h2>Feeds</h2>
        <div class="stats">
          <button class="stat" onclick={() => app.manage()}><strong>{feedStats.total}</strong><span>feeds</span></button>
          <button class="stat" onclick={() => app.manage()}><strong>{feedStats.full}</strong><span>with full articles</span></button>
          <button class="stat" onclick={() => app.manage()}><strong>{feedStats.pages}</strong><span>read from pages</span></button>
          <button class="stat" class:warn={feedStats.failing > 0} onclick={() => app.manage()}><strong>{feedStats.failing}</strong><span>failing</span></button>
        </div>
        <div class="actions-row">
          <button class="btn primary" onclick={() => app.manage()}>Open Subscriptions</button>
          <button class="btn" onclick={() => (app.addFeedOpen = true)}><Icon name="plus" size={16} />Add feed</button>
          <button class="btn" onclick={() => fileInput?.click()}>Import OPML…</button>
          <a class="btn" href="/api/v1/opml" download>Export OPML</a>
          <input bind:this={fileInput} type="file" accept=".opml,.xml,text/xml,text/x-opml" hidden onchange={importFile} />
        </div>
        {#if importMessage}<p class="msg" role="status">{importMessage}</p>{/if}

        {#if app.aiAvailable}
          <div class="card">
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
              <div class="actions-row wide">
                <button class="btn primary" disabled={aiBusy}>{aiBusy ? 'Checking…' : 'Save'}</button>
                {#if app.ai}<button type="button" class="btn" onclick={removeAI}>Remove</button>{/if}
              </div>
            </form>
            <p class="hint">The key is checked with the provider, stored encrypted, and never shown again. Up to 20 AI requests a day.</p>
            {#if aiMessage}<p class="msg" role="status">{aiMessage}</p>{/if}
          </div>
        {/if}
      </section>

      <section id="settings-apps">
        <h2>Apps &amp; sync</h2>
        <div class="card">
          <h3>Reeder and other apps</h3>
          <p class="hint">Add a FreshRSS account with these details. Your sign-in password never works for apps.</p>
          <dl class="creds">
            <dt>Server</dt>
            <dd><code>{readerURL}</code> <button class="link" onclick={copyServer}>{copied ? 'Copied' : 'Copy'}</button></dd>
            <dt>Username</dt>
            <dd><code>{app.user}</code></dd>
            <dt>Password</dt>
            <dd>
              <form onsubmit={saveApiPassword} class="inline">
                <input class="field" type="password" autocomplete="new-password" minlength="10" required
                  placeholder={app.hasApiPassword ? 'Set a new app password' : 'Choose an app password'} bind:value={apiPassword} />
                <button class="btn">Save</button>
              </form>
            </dd>
          </dl>
          {#if apiMessage}<p class="msg" role="status">{apiMessage}</p>{/if}
        </div>

        <div class="card">
          <h3>Notifications</h3>
          {#if support === 'unsupported'}
            <p class="hint">This browser doesn't support notifications.</p>
          {:else}
            <label class="toggle">
              <span><strong>Notifications on this device</strong>
                <small>
                  {#if support === 'needs-install'}
                    On iPhone and iPad, add Correspondent to your Home Screen first (Share, then Add to Home Screen), and turn this on there.
                  {:else}
                    Only feeds you choose notify you: open a feed and use the bell in its header.
                  {/if}
                </small></span>
              <input type="checkbox" role="switch" checked={pushOn} disabled={pushBusy} onchange={togglePush} />
            </label>
            {#if notifying.length}<p class="hint">Notifying: {notifying.map((f) => f.title).join(', ')}.</p>{/if}
            {#if pushOn}<button class="btn" onclick={testPush}>Send a test notification</button>{/if}
            {#if pushMessage}<p class="msg" role="status">{pushMessage}</p>{/if}
          {/if}
        </div>

        <div class="card">
          <h3>This device</h3>
          <p class="hint">
            Unread and starred articles are kept here for reading offline{#if app.syncedAt}; last saved {relative(Math.floor(app.syncedAt / 1000))}{/if}.
            {#if app.outboxCount}{app.outboxCount} {app.outboxCount === 1 ? 'change is' : 'changes are'} waiting to be sent.{/if}
          </p>
          <button class="btn" disabled={syncing || !app.online} onclick={syncNow}>{syncing ? 'Saving…' : 'Save for offline now'}</button>
        </div>
      </section>

      <section id="settings-about">
        <h2>About</h2>
        <p class="about">
          Correspondent {app.version}{#if app.commit}&nbsp;<span class="commit" title={app.commit}>({app.commit.slice(0, 7)})</span>{/if}.
          <a href={releaseURL} target="_blank" rel="noopener noreferrer">What’s new</a>
          · <a href="https://github.com/arenzana/correspondent" target="_blank" rel="noopener noreferrer">Source</a>
        </p>
        <p class="hint">Signed in as {app.user}. Press <kbd>?</kbd> for keyboard shortcuts.</p>
        <button class="btn" onclick={() => app.signOut()}>Sign out</button>
      </section>
    </div>
  </div>
</section>

<style>
  .settings {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--sheet);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: calc(14px + env(safe-area-inset-top)) 18px 12px;
    border-bottom: 1px solid var(--rule);
  }
  .titles {
    flex: 1;
    min-width: 0;
  }
  h1 {
    margin: 0;
    font-family: var(--font-read);
    font-size: calc(23px * var(--ui-scale));
    font-weight: 560;
    letter-spacing: -0.01em;
  }
  .sub {
    margin: 2px 0 0;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: grid;
    grid-template-columns: 200px minmax(0, 680px);
    gap: 0 36px;
    padding: 0 28px calc(96px + env(safe-area-inset-bottom));
    scroll-padding-top: 20px;
  }

  /* The rail: a short spine with a dot per section. */
  .rail {
    position: sticky;
    top: 0;
    align-self: start;
    padding: 30px 0;
  }
  .rail ol {
    list-style: none;
    margin: 0;
    padding: 0;
    background: linear-gradient(var(--rule), var(--rule)) no-repeat 9px 14px / 1.5px calc(100% - 28px);
  }
  .rail button {
    display: flex;
    align-items: center;
    gap: 14px;
    width: 100%;
    padding: 7px 0;
    text-align: left;
    color: var(--slate);
    font-size: calc(13.5px * var(--ui-scale));
  }
  .rail .dot {
    flex: none;
    width: 9px;
    height: 9px;
    margin-left: 5.5px;
    border-radius: 50%;
    border: 1.5px solid var(--ring);
    background: var(--sheet);
    transition: background var(--fast) var(--ease), border-color var(--fast) var(--ease), transform var(--fast) var(--ease);
  }
  .rail button:hover {
    color: var(--ink);
  }
  .rail button[aria-current='true'] {
    color: var(--ink);
    font-weight: 600;
  }
  .rail button[aria-current='true'] .dot {
    border-color: var(--accent);
    background: var(--accent);
    transform: scale(1.15);
  }

  /* Phones: the sections as a row of chips under the header. */
  .chips {
    display: none;
  }

  .content {
    min-width: 0;
  }
  section[id^='settings-'] {
    display: grid;
    gap: 18px;
    padding: 30px 0 34px;
    border-bottom: 1px solid var(--rule);
  }
  section[id^='settings-']:last-child {
    border-bottom: 0;
  }
  h2 {
    margin: 0;
    font-family: var(--font-read);
    font-size: calc(21px * var(--ui-scale));
    font-weight: 560;
    letter-spacing: -0.01em;
  }
  h3 {
    margin: 0;
    font-size: calc(14px * var(--ui-scale));
    font-weight: 600;
  }

  .field-group {
    display: grid;
    gap: 8px;
  }
  .label {
    display: flex;
    justify-content: space-between;
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 600;
    color: var(--ink);
  }
  output {
    font-weight: 400;
    color: var(--slate);
    font-variant-numeric: tabular-nums;
  }
  .pair {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 18px;
  }
  .seg {
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: 1fr;
    padding: 2px;
    border-radius: var(--radius-s);
    background: var(--fog);
  }
  .seg button {
    min-height: 30px;
    padding: 0 6px;
    border-radius: 4px;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  .seg button[aria-pressed='true'] {
    background: var(--sheet);
    color: var(--ink);
    box-shadow: 0 0 0 1px var(--rule);
  }
  .seg .serif {
    font-family: var(--font-read);
    font-size: calc(14px * var(--ui-scale));
  }
  .seg .grotesk {
    font-family: var(--font-grotesk);
  }
  .seg .system {
    font-family: var(--font-system);
  }
  input[type='range'] {
    width: 100%;
    accent-color: var(--accent);
  }

  /* Previews use the live tokens, so they change as settings do. */
  .preview {
    border: 1px solid var(--rule);
    border-radius: var(--radius-l);
    background: var(--sheet);
    overflow: hidden;
  }
  .wire-preview {
    --pv-spine: 66px;
    padding: 10px 0 6px;
    background:
      linear-gradient(var(--rule), var(--rule)) no-repeat var(--pv-spine) 0 / 1.5px 100%,
      var(--sheet);
  }
  .pv-day {
    display: block;
    padding: 0 0 6px calc(var(--pv-spine) + 18px);
    font-size: calc(12px * var(--ui-scale));
    font-weight: 600;
  }
  .pv-row {
    position: relative;
    display: grid;
    grid-template-columns: 52px 28px 1fr;
    align-items: start;
    padding: var(--row-pad) 16px var(--row-pad) 0;
  }
  .pv-selected {
    background: var(--accent-wash);
  }
  .pv-selected::before {
    content: '';
    position: absolute;
    left: calc(var(--pv-spine) - 0.75px);
    top: 0;
    bottom: 0;
    width: 3px;
    border-radius: 2px;
    background: var(--accent);
  }
  .pv-time {
    padding-top: 2px;
    text-align: right;
    font-size: calc(11.5px * var(--ui-scale));
    color: var(--slate);
    font-variant-numeric: tabular-nums;
  }
  .pv-dot {
    justify-self: center;
    width: 9px;
    height: 9px;
    margin: 5px 0 0 1px;
    border-radius: 50%;
    background: var(--accent);
    position: relative;
  }
  .pv-read .pv-dot {
    background: var(--sheet);
    border: 1.5px solid var(--ring);
  }
  .pv-body {
    display: grid;
    gap: 2px;
    min-width: 0;
  }
  .pv-feed {
    font-size: calc(12px * var(--ui-scale));
    color: var(--slate);
  }
  .pv-title {
    font-family: var(--font-read);
    font-size: calc(16.5px * var(--ui-scale));
    font-weight: 600;
    line-height: 1.25;
  }
  .pv-read .pv-title {
    font-weight: 400;
    color: color-mix(in oklab, var(--ink) 78%, var(--sheet));
  }
  .pv-excerpt {
    font-size: calc(13px * var(--ui-scale));
    color: var(--slate);
    line-height: 1.4;
  }
  :global([data-density='compact']) .pv-excerpt {
    display: none;
  }
  .read-preview {
    padding: 20px 24px;
  }
  .read-preview h3 {
    margin: 0 0 8px;
    font-family: var(--font-read);
    font-size: calc(var(--read-size) * 1.3);
    font-weight: 500;
    line-height: 1.15;
  }
  .read-preview p {
    margin: 0;
    max-width: var(--read-measure);
    font-family: var(--read-family, var(--font-read));
    font-size: var(--read-size);
    line-height: 1.66;
  }

  .icons {
    display: flex;
    gap: 12px;
  }
  .icon-choice {
    display: grid;
    justify-items: center;
    gap: 6px;
    padding: 8px 10px;
    border-radius: var(--radius-l);
    border: 1px solid transparent;
    font-size: calc(12px * var(--ui-scale));
    color: var(--slate);
  }
  .icon-choice img {
    border-radius: 12px;
  }
  .icon-choice[aria-checked='true'] {
    border-color: var(--accent);
    background: var(--accent-wash);
    color: var(--ink);
    font-weight: 600;
  }

  .whoami {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .whoami > span:nth-child(2) {
    flex: 1;
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border-radius: 50%;
    background: var(--accent-wash-strong);
    color: var(--accent);
    font-family: var(--font-read);
    font-size: calc(19px * var(--ui-scale));
    font-weight: 600;
  }
  .role {
    margin-left: 8px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--fog);
    color: var(--slate);
    font-size: calc(11px * var(--ui-scale));
    font-weight: 600;
    vertical-align: 1px;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }
  .list li {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 0;
    border-top: 1px solid var(--rule-soft);
  }
  .list li:first-child {
    border-top: 0;
  }
  .item {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 2px;
  }
  .item strong {
    font-weight: 500;
  }
  .item small {
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  .row-actions {
    display: flex;
    gap: 12px;
  }
  .link.danger {
    color: var(--danger);
  }
  .token-form {
    display: grid;
    grid-template-columns: 1fr auto auto;
    gap: 8px;
  }
  .fresh {
    display: grid;
    gap: 8px;
    padding: 14px 16px;
    border-radius: var(--radius-l);
    background: var(--accent-wash);
    border: 1px solid color-mix(in oklab, var(--accent) 30%, var(--rule));
  }
  .fresh p {
    margin: 0;
  }
  .linkrow {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }
  .secret {
    flex: 1 1 260px;
    padding: 8px 10px;
    background: var(--sheet);
    overflow-wrap: anywhere;
    user-select: all;
  }
  .pw {
    display: grid;
    grid-template-columns: 1fr 1fr auto;
    gap: 8px;
  }
  @media (max-width: 559px) {
    .pw,
    .token-form {
      grid-template-columns: 1fr;
    }
    .pw .btn {
      justify-self: start;
    }
  }

  /* A setting that is on or off: label left, switch right. */
  .toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 18px;
    cursor: pointer;
  }
  .toggle span {
    display: grid;
    gap: 2px;
  }
  .toggle strong {
    font-weight: 500;
  }
  .toggle small {
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  input[role='switch'] {
    appearance: none;
    flex: none;
    position: relative;
    width: 38px;
    height: 22px;
    margin: 0;
    border-radius: 11px;
    background: var(--ring);
    cursor: pointer;
    transition: background var(--fast) var(--ease);
  }
  input[role='switch']::after {
    content: '';
    position: absolute;
    top: 3px;
    left: 3px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--sheet);
    box-shadow: 0 1px 2px rgb(0 0 0 / 0.2);
    transition: transform var(--fast) var(--ease);
  }
  input[role='switch']:checked {
    background: var(--accent);
  }
  input[role='switch']:checked::after {
    transform: translateX(16px);
  }
  input[role='switch']:disabled {
    opacity: 0.5;
  }

  .stats {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 10px;
  }
  .stat {
    display: grid;
    gap: 2px;
    padding: 12px 14px;
    text-align: left;
    border-radius: var(--radius-l);
    background: var(--fog);
  }
  .stat strong {
    font-family: var(--font-read);
    font-size: calc(24px * var(--ui-scale));
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    line-height: 1.1;
  }
  .stat span {
    font-size: calc(12px * var(--ui-scale));
    color: var(--slate);
  }
  .stat.warn strong {
    color: var(--danger);
  }
  .stat:hover {
    background: color-mix(in oklab, var(--fog) 80%, var(--ink));
  }
  /* Buttons keep their own width inside the stacked layouts. */
  section[id^='settings-'] > .btn,
  .card > .btn {
    justify-self: start;
  }
  a.btn {
    text-decoration: none;
  }
  .actions-row {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .card {
    display: grid;
    gap: 12px;
    padding: 18px 20px;
    border: 1px solid var(--rule);
    border-radius: var(--radius-l);
  }
  .hint {
    margin: 0;
    font-size: calc(13px * var(--ui-scale));
    color: var(--slate);
    line-height: 1.5;
  }
  .msg {
    margin: 0;
    font-size: calc(13px * var(--ui-scale));
    color: var(--ink);
  }
  .ai {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px 12px;
  }
  .ai label {
    display: grid;
    gap: 4px;
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 600;
  }
  .ai label small {
    font-weight: 400;
    color: var(--slate);
  }
  .ai .wide {
    grid-column: 1 / -1;
  }
  .creds {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 10px 16px;
    align-items: center;
    margin: 0;
  }
  .creds dt {
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 600;
  }
  .creds dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  code {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 0.86em;
    padding: 2px 6px;
    border-radius: 4px;
    background: var(--fog);
  }
  .inline {
    display: flex;
    gap: 8px;
  }
  .inline .field {
    flex: 1;
    min-width: 0;
  }
  .link {
    color: var(--accent);
    font-weight: 500;
    font-size: calc(12.5px * var(--ui-scale));
  }
  .about {
    margin: 0;
  }
  .about a {
    color: var(--accent);
  }
  .commit {
    color: var(--slate);
  }

  @media (max-width: 899px) {
    .body {
      display: block;
      padding: 0 18px calc(96px + env(safe-area-inset-bottom));
    }
    .rail {
      display: none;
    }
    .chips {
      display: flex;
      gap: 6px;
      padding: 8px 18px;
      overflow-x: auto;
      scrollbar-width: none;
      border-bottom: 1px solid var(--rule);
    }
    .chips button {
      flex: none;
      padding: 6px 12px;
      border-radius: 999px;
      font-size: calc(12.5px * var(--ui-scale));
      color: var(--slate);
      background: var(--fog);
    }
    .chips button[aria-current='true'] {
      color: var(--sheet);
      background: var(--ink);
      font-weight: 600;
    }
  }
  @media (max-width: 559px) {
    .pair,
    .ai {
      grid-template-columns: 1fr;
    }
    .stats {
      grid-template-columns: 1fr 1fr;
    }
    .read-preview {
      padding: 16px 18px;
    }
  }
</style>
