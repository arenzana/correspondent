<script lang="ts">
  import { app } from '../lib/state.svelte'

  let username = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      await app.signIn(username, password)
    } catch (err) {
      error = (err as Error).message
      busy = false
    }
  }
</script>

<main class="login">
  <form onsubmit={submit} aria-labelledby="login-title">
    <div class="wire" aria-hidden="true"><i></i><b></b><b></b></div>
    <h1 id="login-title">Correspondent</h1>
    <label>
      <span>Username</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input class="field" bind:value={username} autocomplete="username" autofocus required />
    </label>
    <label>
      <span>Password</span>
      <input class="field" type="password" bind:value={password} autocomplete="current-password" required />
    </label>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <button class="btn primary" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'}</button>
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
    font-size: 34px;
    font-weight: 540;
    font-variation-settings: 'opsz' 72;
    letter-spacing: -0.015em;
  }
  label span {
    display: block;
    margin-bottom: 5px;
    font-size: 12.5px;
    font-weight: 560;
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
