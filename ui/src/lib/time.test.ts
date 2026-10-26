import { describe, expect, it } from 'vitest'
import { arrivedAt, clock, dayLabel, readingMinutes, relative } from './time'

describe('arrivedAt', () => {
  it('decodes microsecond IDs without losing precision', () => {
    // Beyond Number.MAX_SAFE_INTEGER in microseconds would be lossy as a number.
    expect(arrivedAt('1793024993075794').toISOString()).toBe('2026-10-26T14:29:53.075Z')
  })
})

describe('dayLabel', () => {
  const now = new Date(2026, 9, 26, 15, 0)
  it('names recent days', () => {
    expect(dayLabel(new Date(2026, 9, 26, 1, 0), now)).toBe('Today')
    expect(dayLabel(new Date(2026, 9, 25, 23, 59), now)).toBe('Yesterday')
  })
  it('uses the weekday within a week, then a date', () => {
    expect(dayLabel(new Date(2026, 9, 22, 12), now)).not.toMatch(/\d/)
    expect(dayLabel(new Date(2026, 8, 1, 12), now)).toMatch(/1/)
  })
})

describe('clock', () => {
  it('uses a 24-hour clock', () => {
    expect(clock(new Date(2026, 9, 26, 14, 5))).toMatch(/^14.05$/)
    expect(clock(new Date(2026, 9, 26, 0, 30))).toMatch(/^00.30$/)
  })
})

describe('relative', () => {
  const now = Date.parse('2026-10-26T15:00:00Z')
  it.each([
    [now / 1000 - 10, 'just now'],
    [now / 1000 - 600, '10 min ago'],
    [now / 1000 - 3 * 3600, '3 h ago'],
    [now / 1000 - 30 * 3600, 'yesterday'],
    [now / 1000 - 5 * 86400, '5 days ago'],
  ])('formats %d', (t, want) => {
    expect(relative(t, now)).toBe(want)
  })
})

describe('readingMinutes', () => {
  it('estimates from words, ignoring markup, with a minimum of one', () => {
    expect(readingMinutes('<p>short</p>')).toBe(1)
    expect(readingMinutes('<p>' + 'word '.repeat(690) + '</p>')).toBe(3)
  })
})
