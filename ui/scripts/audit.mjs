// npm audit at every severity, failing on any advisory except the ones
// listed here, each with the reason it can't reach readers. Remove an entry
// as soon as a fixed version exists.

import { execFileSync } from 'node:child_process'

const accepted = {
  // deepmerge prototype poisoning, no fixed version (2026-10-28). Only
  // @sveltejs/vite-plugin-svelte uses it, at build time, to merge our own
  // Svelte config; it isn't in the app readers download.
  'GHSA-gcqx-w48m-w2h4': 'deepmerge, build-time only',
}

let out
try {
  out = execFileSync('npm', ['audit', '--json'], { encoding: 'utf8' })
} catch (e) {
  out = e.stdout // npm audit exits non-zero when it finds anything
}
const report = JSON.parse(out)
const found = new Set()
for (const v of Object.values(report.vulnerabilities ?? {}))
  for (const via of v.via) if (typeof via === 'object') found.add(via.url.split('/').pop())

const unexpected = [...found].filter((id) => !accepted[id])
for (const id of found) console.log(`${accepted[id] ? 'accepted' : 'FOUND   '} ${id}${accepted[id] ? ` (${accepted[id]})` : ''}`)
if (unexpected.length) {
  console.error(`\n${unexpected.length} advisory(ies) not accepted: run npm audit for details.`)
  process.exit(1)
}
console.log(found.size ? 'Only accepted advisories.' : 'No advisories.')
