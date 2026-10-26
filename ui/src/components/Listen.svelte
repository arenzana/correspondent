<script lang="ts">
  import { app } from '../lib/state.svelte'
  import Icon from './Icon.svelte'

  const speeds = [1, 1.25, 1.5, 1.75, 2, 0.8]

  let player: HTMLAudioElement | undefined = $state()
  let paused = $state(true)
  let current = $state(0)
  let duration = $state(0)
  let speed = $state(1)

  const now = $derived(app.listening)

  // Lock screen and headphone controls.
  $effect(() => {
    if (!now || !('mediaSession' in navigator)) return
    navigator.mediaSession.metadata = new MediaMetadata({ title: now.title, artist: now.feed, album: 'Correspondent' })
    const set = (action: MediaSessionAction, fn: MediaSessionActionHandler | null) => {
      try {
        navigator.mediaSession.setActionHandler(action, fn)
      } catch {
        /* not supported here */
      }
    }
    set('play', () => player?.play())
    set('pause', () => player?.pause())
    set('seekbackward', () => skip(-15))
    set('seekforward', () => skip(30))
    set('seekto', (d) => d.seekTime !== undefined && player && (player.currentTime = d.seekTime))
    return () => {
      for (const a of ['play', 'pause', 'seekbackward', 'seekforward', 'seekto'] as const) set(a, null)
      navigator.mediaSession.metadata = null
    }
  })

  $effect(() => {
    if (player) player.playbackRate = speed
  })

  function skip(seconds: number) {
    if (!player) return
    player.currentTime = Math.max(0, Math.min(player.duration || 0, player.currentTime + seconds))
  }

  function nextSpeed() {
    speed = speeds[(speeds.indexOf(speed) + 1) % speeds.length]
  }

  function clock(s: number): string {
    if (!isFinite(s)) return '0:00'
    const m = Math.floor(s / 60)
    return `${m}:${String(Math.floor(s % 60)).padStart(2, '0')}`
  }

  /** Space-free keyboard control, for the app's shortcuts. */
  export function toggle() {
    if (!player) return
    if (player.paused) player.play()
    else player.pause()
  }
</script>

{#if now}
  <section class="listen" aria-label="Reading aloud">
    <audio
      bind:this={player}
      src={now.url}
      autoplay
      preload="auto"
      onplay={() => (paused = false)}
      onpause={() => (paused = true)}
      onended={() => (paused = true)}
      ontimeupdate={() => (current = player?.currentTime ?? 0)}
      ondurationchange={() => (duration = player?.duration ?? 0)}
      onratechange={() => player && player.playbackRate !== speed && (player.playbackRate = speed)}
    ></audio>
    <button class="icon-btn" aria-label="Back 15 seconds" title="Back 15 seconds" onclick={() => skip(-15)}><Icon name="back15" /></button>
    <button class="icon-btn play" aria-label={paused ? 'Play' : 'Pause'} onclick={toggle}><Icon name={paused ? 'play' : 'pause'} /></button>
    <button class="icon-btn" aria-label="Ahead 30 seconds" title="Ahead 30 seconds" onclick={() => skip(30)}><Icon name="ahead15" /></button>
    <button class="what" onclick={() => app.select(now.id)} title="Go to the article">
      <span class="title">{now.title}</span>
      <span class="meta">{now.feed}</span>
    </button>
    <input
      class="seek"
      type="range"
      min="0"
      max={duration || 0}
      step="1"
      value={current}
      aria-label="Position"
      oninput={(e) => player && (player.currentTime = Number(e.currentTarget.value))}
      style:--p={duration ? current / duration : 0}
    />
    <span class="time" aria-hidden="true">{clock(current)} / {clock(duration)}</span>
    <button class="speed" aria-label="Speed {speed}×" title="Speed" onclick={nextSpeed}>{speed}×</button>
    <button class="icon-btn" aria-label="Stop listening" title="Stop listening" onclick={() => (app.listening = null)}><Icon name="x" /></button>
  </section>
{/if}

<style>
  .listen {
    position: fixed;
    z-index: 40;
    left: 50%;
    bottom: max(16px, env(safe-area-inset-bottom));
    transform: translateX(-50%);
    width: min(720px, calc(100vw - 24px));
    display: grid;
    grid-template-columns: auto auto auto minmax(0, 1fr) auto auto;
    grid-template-areas:
      'back play ahead what speed close'
      'seek seek seek seek seek time';
    align-items: center;
    column-gap: 2px;
    row-gap: 2px;
    padding: 8px 10px 8px 6px;
    border: 1px solid var(--rule);
    border-radius: var(--radius-l);
    background: var(--sheet);
    color: var(--ink);
    box-shadow: var(--overlay-shadow);
    animation: rise 220ms var(--ease);
  }
  @keyframes rise {
    from {
      opacity: 0;
      transform: translate(-50%, 8px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .listen {
      animation: none;
    }
  }
  .listen > :nth-child(2) {
    grid-area: back;
  }
  .play {
    grid-area: play;
    color: var(--accent);
  }
  .listen > :nth-child(4) {
    grid-area: ahead;
  }
  .what {
    grid-area: what;
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    padding: 2px 8px;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
    border-radius: var(--radius-s);
  }
  .what:hover {
    background: var(--hover);
  }
  .title,
  .meta {
    max-width: 100%;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .title {
    font-weight: 600;
    font-size: calc(14px * var(--ui-scale));
  }
  .meta {
    color: var(--slate);
    font-size: calc(12px * var(--ui-scale));
  }
  .speed {
    grid-area: speed;
    min-width: 44px;
    height: 32px;
    border: 1px solid var(--rule);
    border-radius: var(--radius-s);
    background: none;
    color: var(--slate);
    font: inherit;
    font-size: calc(13px * var(--ui-scale));
    font-variant-numeric: tabular-nums;
    cursor: pointer;
  }
  .speed:hover {
    color: var(--ink);
    background: var(--hover);
  }
  .listen > :last-child {
    grid-area: close;
  }
  .seek {
    grid-area: seek;
    width: 100%;
    height: 18px;
    margin: 0 4px 0 8px;
    appearance: none;
    background: none;
    cursor: pointer;
  }
  .seek::-webkit-slider-runnable-track {
    height: 3px;
    border-radius: 3px;
    background: linear-gradient(to right, var(--accent) calc(var(--p) * 100%), var(--rule) 0);
  }
  .seek::-moz-range-track {
    height: 3px;
    border-radius: 3px;
    background: linear-gradient(to right, var(--accent) calc(var(--p) * 100%), var(--rule) 0);
  }
  .seek::-webkit-slider-thumb {
    appearance: none;
    width: 12px;
    height: 12px;
    margin-top: -4.5px;
    border-radius: 50%;
    background: var(--accent);
  }
  .seek::-moz-range-thumb {
    width: 12px;
    height: 12px;
    border: 0;
    border-radius: 50%;
    background: var(--accent);
  }
  .seek:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: 4px;
  }
  .time {
    grid-area: time;
    padding-left: 8px;
    color: var(--slate);
    font-size: calc(12px * var(--ui-scale));
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
</style>
