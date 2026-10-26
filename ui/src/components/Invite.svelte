<script lang="ts">
  // The page an invitation or password-reset link opens: choose a username
  // and password (or a new password), then you're signed in.
  import { app } from '../lib/state.svelte'
  import { api } from '../lib/api'

  let { token }: { token: string } = $props()

  let details = $state<{ kind: 'invite' | 'reset'; username: string; invitedBy: string; expiresAt: number } | null>(null)
  let invalid = $state('')
  let username = $state('')
  let password = $state('')
  let again = $state('')
  let error = $state('')
  let busy = $state(false)

  $effect(() => {
    api
      .invite(token)
      .then((d) => (details = d))
      .catch((e) => (invalid = (e as Error).message))
  })

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    if (password !== again) {
      error = 'The passwords don’t match.'
      return
    }
    busy = true
    try {
      await api.acceptInvite(token, username.trim(), password)
      await app.signedIn()
    } catch (err) {
      error = (err as Error).message
      busy = false
    }
  }
</script>

<main class="login">
  <form onsubmit={submit} aria-labelledby="invite-title">
    <div class="wire" aria-hidden="true"><i></i><b></b><b></b></div>
    <h1 id="invite-title">Correspondent</h1>
    {#if invalid}
      <p class="lead">{invalid}</p>
      <a class="btn" href="/">Go to sign in</a>
    {:else if !details}
      <p class="lead">Checking your link…</p>
    {:else}
      {#if details.kind === 'invite'}
        <p class="lead">{details.invitedBy ? `${details.invitedBy} invited you.` : 'You’re invited.'} Choose a username and a password.</p>
        <label>
          <span>Username</span>
          <!-- svelte-ignore a11y_autofocus -->
          <input class="field" bind:value={username} autocomplete="username" autocapitalize="none" spellcheck="false"
            minlength="2" maxlength="40" autofocus required />
        </label>
      {:else}
        <p class="lead">Choose a new password for <strong>{details.username}</strong>.</p>
        <input type="text" autocomplete="username" value={details.username} hidden readonly />
      {/if}
      <label>
        <span>Password</span>
        <input class="field" type="password" bind:value={password} autocomplete="new-password" minlength="10" required />
      </label>
      <label>
        <span>Password again</span>
        <input class="field" type="password" bind:value={again} autocomplete="new-password" minlength="10" required />
      </label>
      <p class="hint">At least 10 characters. You can add a passkey in Settings afterwards.</p>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <button class="btn primary" disabled={busy}>
        {busy ? 'One moment…' : details.kind === 'invite' ? 'Create account' : 'Set password'}
      </button>
    {/if}
  </form>
</main>

<style>
  .login {
    min-height: 100%;
    display: grid;
    place-items: center;
    padding: 24px 16px;
  }
  form {
    position: relative;
    width: min(340px, 100%);
    display: grid;
    gap: 14px;
    padding-left: 34px;
  }
  /* The form hangs off the same spine as the wire. */
  .wire {
    position: absolute;
    left: 6px;
    top: 4px;
    bottom: 4px;
    width: 10px;
  }
  .wire::before {
    content: '';
    position: absolute;
    left: 4.25px;
    top: 0;
    bottom: 0;
    width: 1.5px;
    background: var(--rule);
  }
  .wire i,
  .wire b {
    position: absolute;
    left: 0;
    width: 10px;
    height: 10px;
    border-radius: 50%;
  }
  .wire i {
    top: 12px;
    background: var(--accent);
  }
  .wire b {
    top: 50%;
    border: 1.5px solid var(--ring);
    background: var(--fog);
  }
  .wire b + b {
    top: auto;
    bottom: 10px;
  }
  h1 {
    margin: 0 0 10px;
    font-family: var(--font-read);
    font-size: calc(34px * var(--ui-scale));
    font-weight: 540;
    font-variation-settings: 'opsz' 72;
    letter-spacing: -0.015em;
  }
  label span {
    display: block;
    margin-bottom: 5px;
    font-size: calc(12.5px * var(--ui-scale));
    font-weight: 560;
  }
  .lead {
    margin: -4px 0 4px;
    line-height: 1.5;
  }
  .hint {
    margin: -4px 0 0;
    font-size: calc(12.5px * var(--ui-scale));
    color: var(--slate);
  }
  .error {
    margin: 0;
    color: var(--danger);
  }
  .btn {
    height: 38px;
    justify-content: center;
  }
</style>
