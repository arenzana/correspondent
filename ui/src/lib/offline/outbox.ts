// Sends changes made offline, in order. Used by the app (when the connection
// returns or the app opens) and by the service worker (Background Sync).
// The server's state changes are idempotent, so resending is always safe.

import { pending, remove, type OutboxOp } from './db'

export type FlushResult = { sent: number; left: number; stopped?: 'offline' | 'signed-out' | 'server' }

/**
 * Merges runs of state changes so a burst of offline actions becomes a few
 * requests: within a run, the last action per article and axis (read, star)
 * wins. Mark-all operations keep their place in the order.
 */
export function plan(ops: OutboxOp[]): OutboxOp[] {
  const out: OutboxOp[] = []
  let run: { read: Map<string, boolean>; star: Map<string, boolean>; at: number } | null = null
  const closeRun = () => {
    if (!run) return
    const group = (m: Map<string, boolean>, yes: 'read' | 'star', no: 'unread' | 'unstar') => {
      const on = [...m].filter(([, v]) => v).map(([k]) => k)
      const off = [...m].filter(([, v]) => !v).map(([k]) => k)
      if (on.length) out.push({ kind: 'state', action: yes, ids: on, at: run!.at })
      if (off.length) out.push({ kind: 'state', action: no, ids: off, at: run!.at })
    }
    group(run.read, 'read', 'unread')
    group(run.star, 'star', 'unstar')
    run = null
  }
  for (const op of ops) {
    if (op.kind === 'markAll') {
      closeRun()
      out.push(op)
      continue
    }
    run ??= { read: new Map(), star: new Map(), at: op.at }
    const axis = op.action === 'read' || op.action === 'unread' ? run.read : run.star
    for (const id of op.ids) axis.set(id, op.action === 'read' || op.action === 'star')
  }
  closeRun()
  return out
}

async function send(op: OutboxOp, fetchFn: typeof fetch): Promise<Response> {
  const headers = { 'Content-Type': 'application/json', 'X-Correspondent': '1' }
  if (op.kind === 'state') {
    return fetchFn('/api/v1/entries/state', { method: 'POST', headers, credentials: 'same-origin',
      body: JSON.stringify({ action: op.action, ids: op.ids }) })
  }
  return fetchFn('/api/v1/entries/mark-all-read', { method: 'POST', headers, credentials: 'same-origin',
    body: JSON.stringify({
      feed: op.scope.feed ? String(op.scope.feed) : undefined,
      folder: op.scope.folder ? String(op.scope.folder) : undefined,
      view: op.scope.view,
      before: op.before,
    }) })
}

/** Sends a step, splitting state changes to the server's 1000-ID limit. Returns the first failure, or the last response. */
async function sendSplit(step: OutboxOp, fetchFn: typeof fetch): Promise<Response> {
  if (step.kind !== 'state' || step.ids.length <= 1000) return send(step, fetchFn)
  let res!: Response
  for (let i = 0; i < step.ids.length; i += 1000) {
    res = await send({ ...step, ids: step.ids.slice(i, i + 1000) }, fetchFn)
    if (!res.ok) return res
  }
  return res
}

let flushing: Promise<FlushResult> | null = null

/** Send everything queued. Concurrent calls share one run. */
export function flush(fetchFn: typeof fetch = fetch): Promise<FlushResult> {
  flushing ??= run(fetchFn).finally(() => (flushing = null))
  return flushing
}

async function run(fetchFn: typeof fetch): Promise<FlushResult> {
  const queued = await pending()
  if (!queued.length) return { sent: 0, left: 0 }
  const steps = plan(queued.map((q) => q.op))
  let sent = 0
  for (const step of steps) {
    let res: Response
    try {
      res = await sendSplit(step, fetchFn)
    } catch {
      return { sent, left: queued.length, stopped: 'offline' }
    }
    if (res.status === 401) return { sent, left: queued.length, stopped: 'signed-out' }
    if (res.status >= 500 || res.status === 429) return { sent, left: queued.length, stopped: 'server' }
    // 2xx: done. Other 4xx: the server rejects it outright (e.g. an article
    // that no longer exists); retrying can't help, so it is dropped.
    sent++
  }
  // Everything planned went through: clear exactly the ops we read.
  await remove(queued.map((q) => q.key))
  return { sent, left: 0 }
}
