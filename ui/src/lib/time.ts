// Time helpers. The reader lists articles by publication time; entry IDs are
// microsecond arrival (crawl) timestamps, which only sync and boundaries use.

const dayFmt = new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long' })
const longFmt = new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })
const clockFmt = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
const shortFmt = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' })

export function arrivedAt(id: string): Date {
  return new Date(Number(BigInt(id) / 1000n))
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

export function dayKey(d: Date): number {
  return startOfDay(d)
}

export function dayLabel(d: Date, now = new Date()): string {
  const diff = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000)
  if (diff === 0) return 'Today'
  if (diff === 1) return 'Yesterday'
  if (diff < 7) return dayFmt.format(d).split(',')[0]
  return dayFmt.format(d)
}

export function clock(d: Date): string {
  return clockFmt.format(d)
}

export function longDate(seconds: number): string {
  return longFmt.format(new Date(seconds * 1000))
}

export function relative(seconds: number, now = Date.now()): string {
  const s = Math.max(0, Math.round(now / 1000 - seconds))
  if (s < 60) return 'just now'
  const m = Math.round(s / 60)
  if (m < 60) return `${m} min ago`
  const h = Math.round(m / 60)
  if (h < 24) return `${h} h ago`
  const d = Math.round(h / 24)
  if (d < 30) return d === 1 ? 'yesterday' : `${d} days ago`
  return shortFmt.format(new Date(seconds * 1000))
}

export function readingMinutes(html: string): number {
  const words = html.replace(/<[^>]+>/g, ' ').split(/\s+/).filter(Boolean).length
  return Math.max(1, Math.round(words / 230))
}
