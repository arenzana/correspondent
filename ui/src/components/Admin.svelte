<script lang="ts">
  import { scrollMotion } from '../lib/motion'
  // The admin page: accounts and invitations, the server's health, and the
  // page parsers everyone shares. Every action is checked again by the
  // server; this page only shows it to admins.
  import { app } from '../lib/state.svelte'
  import { api, type Account, type Invite, type ParserInfo, type Role, type ServerInfo } from '../lib/api'
  import { relative } from '../lib/time'
  import Icon from './Icon.svelte'

  const sections = [
    { id: 'accounts', label: 'Accounts' },
    { id: 'server', label: 'Server' },
    { id: 'parsers', label: 'Shared parsers' },
  ]
  let scroller: HTMLElement | undefined = $state()
  let current = $state('accounts')
  function show(id: string) {
    current = id
    scroller?.querySelector(`#admin-${id}`)?.scrollIntoView({ behavior: scrollMotion(), block: 'start' })
  }
  function track() {
    if (!scroller) return
    const top = scroller.getBoundingClientRect().top + 90
    const els = [...scroller.querySelectorAll<HTMLElement>('section[id^="admin-"]')]
    const atEnd = scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 4
    const passed = els.filter((el) => el.getBoundingClientRect().top <= top)
    const el = atEnd ? els[els.length - 1] : passed[passed.length - 1] ?? els[0]
    if (el) current = el.id.replace('admin-', '')
  }

  let accounts = $state<Account[]>([])
  let invites = $state<Invite[]>([])
  let server = $state<ServerInfo | null>(null)
  let parsers = $state<ParserInfo[]>([])
  let message = $state('')

  async function load() {
    try {
      const [a, s, p] = await Promise.all([api.admin.accounts(), api.admin.server(), api.admin.parsers()])
      accounts = a.accounts
      invites = a.invites
      server = s
      parsers = p.parsers
    } catch (e) {
      message = (e as Error).message
    }
  }
  $effect(() => {
    load()
  })

  // Links are shown once, right after they're made; the server keeps only
  // a hash of the token.
  const linkFor = (token: string) => `${location.origin}/#/invite/${token}`
  let fresh = $state<{ label: string; url: string } | null>(null)
  let copied = $state(false)
  async function copy() {
    if (!fresh) return
    try {
      await navigator.clipboard.writeText(fresh.url)
      copied = true
      setTimeout(() => (copied = false), 1600)
    } catch {
      /* the link stays visible to copy by hand */
    }
  }

  let inviteRole = $state<Role>('user')
  let inviteNote = $state('')
  async function createInvite(e: SubmitEvent) {
    e.preventDefault()
    try {
      const { token } = await api.admin.invite(inviteRole, inviteNote.trim())
      fresh = { label: `Invitation${inviteNote.trim() ? ' for ' + inviteNote.trim() : ''}, valid for 7 days, once`, url: linkFor(token) }
      inviteNote = ''
      load()
    } catch (err) {
      message = (err as Error).message
    }
  }

  async function act(fn: () => Promise<unknown>, done?: string) {
    message = ''
    try {
      await fn()
      if (done) app.toast(done)
      await load()
    } catch (e) {
      message = (e as Error).message
    }
  }

  async function resetLink(a: Account) {
    try {
      const { token } = await api.admin.resetLink(a.id)
      fresh = { label: `Password reset for ${a.username}, valid for 48 hours, once`, url: linkFor(token) }
    } catch (e) {
      message = (e as Error).message
    }
  }

  let confirmDelete = $state<number | null>(null)

  let tests = $state<Record<number, string>>({})
  async function test(p: ParserInfo) {
    tests[p.id] = 'Testing…'
    try {
      const r = await api.admin.testParser(p.id)
      tests[p.id] = r.ok ? `Finds ${r.items} articles: ${(r.sample ?? []).slice(0, 3).join(', ')}` : (r.error ?? 'It failed.')
    } catch (e) {
      tests[p.id] = (e as Error).message
    }
  }
  async function rebuild(p: ParserInfo) {
    tests[p.id] = 'Rebuilding…'
    try {
      const r = await api.admin.rebuildParser(p.id)
      tests[p.id] = `Rebuilt (${methodLabel(r.method)}): finds ${r.items} articles.`
      load()
    } catch (e) {
      tests[p.id] = (e as Error).message
    }
  }

  const methodLabel = (m: string) =>
    ({ hfeed: 'microformats', jsonld: 'structured data', links: 'link pattern', css: 'written by AI' })[m] ?? m
  const host = (u: string) => u.replace(/^https?:\/\//, '')
  function bytes(n?: number) {
    if (n === undefined) return '–'
    const units = ['bytes', 'KB', 'MB', 'GB']
    let i = 0
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024
      i++
    }
    return `${n < 10 && i ? n.toFixed(1) : Math.round(n)} ${units[i]}`
  }
  const me = $derived(accounts.find((a) => a.username === app.user))
</script>

<section class="admin" aria-labelledby="admin-title">
  <header class="head">
    <button class="icon-btn" aria-label="Back to articles" title="Back to articles" onclick={() => app.leavePage()}>
      <Icon name="back" />
    </button>
    <div class="titles">
      <h1 id="admin-title">Admin</h1>
      <p class="sub">{accounts.length} {accounts.length === 1 ? 'account' : 'accounts'} on this server</p>
    </div>
  </header>

  <nav class="chips" aria-label="Admin sections">
    {#each sections as sec (sec.id)}
      <button aria-current={current === sec.id ? 'true' : undefined} onclick={() => show(sec.id)}>{sec.label}</button>
    {/each}
  </nav>

  <div class="body" bind:this={scroller} onscroll={track}>
    <nav class="rail" aria-label="Admin sections">
      <ol>
        {#each sections as sec (sec.id)}
          <li><button aria-current={current === sec.id ? 'true' : undefined} onclick={() => show(sec.id)}><span class="dot" aria-hidden="true"></span>{sec.label}</button></li>
        {/each}
      </ol>
    </nav>

    <div class="content">
      {#if message}<p class="error" role="alert">{message}</p>{/if}
      {#if fresh}
        <div class="fresh" role="status">
          <p>{fresh.label}. Send it to them; it won’t be shown again.</p>
          <div class="linkrow">
            <code>{fresh.url}</code>
            <button class="btn primary" onclick={copy}>{copied ? 'Copied' : 'Copy link'}</button>
            <button class="btn" onclick={() => (fresh = null)}>Done</button>
          </div>
        </div>
      {/if}

      <section id="admin-accounts">
        <h2>Accounts</h2>
        <form class="card invite" onsubmit={createInvite}>
          <h3>Invite someone</h3>
          <p class="hint">They get a link to choose their own username and password. Links work once, for 7 days.</p>
          <div class="invite-row">
            <input class="field" bind:value={inviteNote} maxlength="120" placeholder="Who is it for? (optional, for you)" />
            <select class="field" bind:value={inviteRole} aria-label="Role">
              <option value="user">Reader</option>
              <option value="admin">Admin</option>
            </select>
            <button class="btn primary">Create invite link</button>
          </div>
        </form>

        {#if invites.length}
          <div class="card">
            <h3>Waiting to be used</h3>
            <ul class="list">
              {#each invites as i (i.id)}
                <li>
                  <span class="item">
                    <strong>{i.kind === 'reset' ? `Password reset for ${i.username}` : i.note || 'Invitation'}{#if i.kind === 'invite' && i.role === 'admin'}<span class="tag">Admin</span>{/if}</strong>
                    <small>By {i.createdBy || 'the command line'} {relative(i.createdAt)}; expires {new Date(i.expiresAt * 1000).toLocaleDateString()}</small>
                  </span>
                  <button class="link danger" onclick={() => act(() => api.admin.revokeInvite(i.id), 'Link revoked.')}>Revoke</button>
                </li>
              {/each}
            </ul>
          </div>
        {/if}

        <ul class="accounts">
          {#each accounts as a (a.id)}
            {@const self = a.id === me?.id}
            <li class="account" class:disabled={a.disabled}>
              <span class="avatar" aria-hidden="true">{a.username.slice(0, 1).toUpperCase()}</span>
              <span class="item">
                <strong>{a.username}{#if a.role === 'admin'}<span class="tag">Admin</span>{/if}{#if a.disabled}<span class="tag warn">Disabled</span>{/if}{#if self}<span class="tag">You</span>{/if}</strong>
                <small>
                  {a.feeds} {a.feeds === 1 ? 'feed' : 'feeds'}, {a.articles.toLocaleString()} articles;
                  {a.lastSeenAt ? `active ${relative(a.lastSeenAt)}` : 'not signed in yet'}{#if a.passkeys}; {a.passkeys} {a.passkeys === 1 ? 'passkey' : 'passkeys'}{/if}{#if a.hasApiPassword}; uses an app{/if}
                </small>
              </span>
              {#if confirmDelete === a.id}
                <span class="confirm">
                  Delete {a.username} and their {a.feeds} feeds? This can’t be undone.
                  <button class="btn danger" onclick={() => act(() => api.admin.deleteAccount(a.id), `${a.username} was deleted.`).then(() => (confirmDelete = null))}>Delete</button>
                  <button class="btn" onclick={() => (confirmDelete = null)}>Cancel</button>
                </span>
              {:else if !self}
                <span class="row-actions">
                  <button class="link" onclick={() => act(() => api.admin.updateAccount(a.id, { role: a.role === 'admin' ? 'user' : 'admin' }))}>
                    {a.role === 'admin' ? 'Make reader' : 'Make admin'}
                  </button>
                  <button class="link" onclick={() => resetLink(a)}>Reset password</button>
                  <button class="link" onclick={() => act(() => api.admin.updateAccount(a.id, { disabled: !a.disabled }), a.disabled ? `${a.username} can sign in again.` : `${a.username} was signed out and disabled.`)}>
                    {a.disabled ? 'Enable' : 'Disable'}
                  </button>
                  <button class="link danger" onclick={() => (confirmDelete = a.id)}>Delete</button>
                </span>
              {/if}
            </li>
          {/each}
        </ul>
      </section>

      <section id="admin-server">
        <h2>Server</h2>
        {#if server}
          <dl class="facts">
            <div><dt>Version</dt><dd>{server.version}</dd></div>
            <div><dt>Running since</dt><dd>{relative(server.startedAt)}</dd></div>
            <div><dt>Feeds</dt><dd>{server.stats.feeds}<small>{server.stats.failing} failing, {server.stats.paused} paused</small></dd></div>
            <div><dt>Articles</dt><dd>{server.stats.articles.toLocaleString()}</dd></div>
            <div><dt>Database</dt><dd>{bytes(server.databaseBytes)}</dd></div>
            <div><dt>Image cache</dt><dd>{bytes(server.imageCacheBytes)}</dd></div>
            <div><dt>Last backup</dt><dd>{server.backupAt ? relative(server.backupAt) : 'none found'}</dd></div>
            <div><dt>Checked in the last hour</dt><dd>{server.stats.polledLastHour}<small>{server.stats.lastFetchAt ? `last ${relative(server.stats.lastFetchAt)}` : 'nothing yet'}</small></dd></div>
            <div><dt>Passkeys</dt><dd>{server.passkeys ? 'On' : 'Off'}<small>{server.passkeys ? '' : 'set CORRESPONDENT_PUBLIC_URL'}</small></dd></div>
          </dl>
          {#if server.failing.length}
            <div class="card">
              <h3>Failing feeds, across accounts</h3>
              <ul class="list">
                {#each server.failing as f (f.id)}
                  <li>
                    <span class="item"><strong>{f.title || host(f.url)}</strong>
                      <small>{f.owner}; failed {f.errorCount} {f.errorCount === 1 ? 'time' : 'times'}: {f.lastError}</small></span>
                  </li>
                {/each}
              </ul>
            </div>
          {/if}
        {/if}
      </section>

      <section id="admin-parsers">
        <h2>Shared parsers</h2>
        <p class="hint">How the server reads sites without feeds. Everyone who follows a page uses its parser; rebuilding one fixes it for all of them.</p>
        <ul class="list parsers">
          {#each parsers as p (p.id)}
            <li>
              <span class="item">
                <strong>{host(p.url)}{#if p.builtin}<span class="tag">Built in</span>{/if}{#if p.failing}<span class="tag warn">{p.failing} failing</span>{/if}</strong>
                <small>{methodLabel(p.method)}; {p.feeds} {p.feeds === 1 ? 'reader' : 'readers'}{#if p.createdBy}; by {p.createdBy}{/if}; updated {relative(p.updatedAt)}</small>
                {#if tests[p.id]}<small class="result">{tests[p.id]}</small>{/if}
              </span>
              <span class="row-actions">
                <button class="link" onclick={() => test(p)}>Test</button>
                <button class="link" onclick={() => rebuild(p)}>Rebuild</button>
                {#if !p.feeds}<button class="link danger" onclick={() => act(() => api.admin.deleteParser(p.id), 'Parser removed.')}>Remove</button>{/if}
              </span>
            </li>
          {:else}
            <li><span class="item"><small>No shared parsers yet.</small></span></li>
          {/each}
        </ul>
      </section>
    </div>
  </div>
</section>

<style>
  .admin {
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
    grid-template-columns: 200px minmax(0, 760px);
    gap: 0 36px;
    padding: 0 28px calc(96px + env(safe-area-inset-bottom));
  }
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
    transition: background var(--fast) var(--ease), border-color var(--fast) var(--ease);
  }
  .rail button[aria-current='true'] {
    color: var(--ink);
    font-weight: 600;
  }
  .rail button[aria-current='true'] .dot {
    border-color: var(--accent);
    background: var(--accent);
  }
  .chips {
    display: none;
  }
  .content {
    min-width: 0;
    padding-top: 8px;
  }
  section[id^='admin-'] {
    display: grid;
    gap: 16px;
    padding: 22px 0 34px;
    border-bottom: 1px solid var(--rule);
  }
  section[id^='admin-']:last-child {
    border-bottom: 0;
  }
  h2 {
    margin: 0;
    font-family: var(--font-read);
    font-size: calc(21px * var(--ui-scale));
    font-weight: 560;
  }
  h3 {
    margin: 0;
    font-size: calc(14px * var(--ui-scale));
    font-weight: 600;
  }
  .hint {
    margin: 0;
    font-size: calc(13px * var(--ui-scale));
    color: var(--slate);
    line-height: 1.5;
  }
  .error {
    margin: 16px 0 0;
    color: var(--danger);
  }
  .card {
    display: grid;
    gap: 12px;
    padding: 18px 20px;
    border: 1px solid var(--rule);
    border-radius: var(--radius-l);
  }
  .invite-row {
    display: grid;
    grid-template-columns: 1fr auto auto;
    gap: 8px;
  }
  .fresh {
    margin-top: 16px;
    padding: 16px 18px;
    border-radius: var(--radius-l);
    background: var(--accent-wash);
    border: 1px solid color-mix(in oklab, var(--accent) 30%, var(--rule));
  }
  .fresh p {
    margin: 0 0 10px;
  }
  .linkrow {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }
  .linkrow code {
    flex: 1 1 260px;
    min-width: 0;
    padding: 8px 10px;
    border-radius: var(--radius-s);
    background: var(--sheet);
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: calc(12.5px * var(--ui-scale));
    overflow-wrap: anywhere;
    user-select: all;
  }
  .list,
  .accounts {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .list li,
  .account {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px 12px;
    padding: 12px 0;
    border-top: 1px solid var(--rule-soft);
  }
  .list li:first-child,
  .account:first-child {
    border-top: 0;
  }
  .account.disabled .item,
  .account.disabled .avatar {
    opacity: 0.55;
  }
  .avatar {
    display: grid;
    place-items: center;
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: 50%;
    background: var(--accent-wash-strong);
    color: var(--accent);
    font-family: var(--font-read);
    font-weight: 600;
  }
  .item {
    flex: 1 1 220px;
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
  .item .result {
    color: var(--ink);
  }
  .tag {
    margin-left: 8px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--fog);
    color: var(--slate);
    font-size: calc(11px * var(--ui-scale));
    font-weight: 600;
    vertical-align: 1px;
  }
  .tag.warn {
    background: color-mix(in oklab, var(--danger) 14%, var(--sheet));
    color: var(--danger);
  }
  .row-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 14px;
  }
  .link {
    color: var(--accent);
    font-weight: 500;
    font-size: calc(12.5px * var(--ui-scale));
  }
  .link.danger {
    color: var(--danger);
  }
  .confirm {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    font-size: calc(13px * var(--ui-scale));
  }
  .btn.danger {
    background: var(--danger);
    border-color: var(--danger);
    color: var(--sheet);
  }
  .facts {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 10px;
    margin: 0;
  }
  .facts div {
    padding: 12px 14px;
    border-radius: var(--radius-l);
    background: var(--fog);
  }
  .facts dt {
    font-size: calc(12px * var(--ui-scale));
    color: var(--slate);
  }
  .facts dd {
    margin: 2px 0 0;
    font-family: var(--font-read);
    font-size: calc(19px * var(--ui-scale));
    font-variant-numeric: tabular-nums;
  }
  .facts dd small {
    display: block;
    font-family: var(--font-ui);
    font-size: calc(12px * var(--ui-scale));
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
    .facts {
      grid-template-columns: 1fr 1fr;
    }
  }
  @media (max-width: 559px) {
    .invite-row {
      grid-template-columns: 1fr;
    }
    .invite-row .btn {
      justify-self: start;
    }
  }
</style>
