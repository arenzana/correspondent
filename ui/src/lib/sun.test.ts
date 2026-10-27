import { describe, expect, it } from 'vitest'
import { coarse, estimateFromTimezone, sunTheme, sunTimes } from './sun'

const minutesApart = (a: Date | null, b: Date) => Math.abs((a?.getTime() ?? NaN) - b.getTime()) / 60_000

describe('sunTimes', () => {
  // Reference values from the NOAA solar calculator.
  it('matches Madrid at the June solstice', () => {
    const t = sunTimes(new Date('2026-06-21T12:00:00Z'), 40.4168, -3.7038)
    expect(minutesApart(t.sunrise, new Date('2026-06-21T04:44:00Z'))).toBeLessThan(4)
    expect(minutesApart(t.sunset, new Date('2026-06-21T19:48:00Z'))).toBeLessThan(4)
  })

  it('matches New York at the December solstice', () => {
    const t = sunTimes(new Date('2026-12-21T12:00:00Z'), 40.7128, -74.006)
    expect(minutesApart(t.sunrise, new Date('2026-12-21T12:16:00Z'))).toBeLessThan(4)
    expect(minutesApart(t.sunset, new Date('2026-12-21T21:32:00Z'))).toBeLessThan(4)
  })

  it('reports polar night and midnight sun', () => {
    expect(sunTimes(new Date('2026-12-21T12:00:00Z'), 69.65, 18.96).polar).toBe('night')
    expect(sunTimes(new Date('2026-06-21T12:00:00Z'), 69.65, 18.96).polar).toBe('day')
  })
})

describe('sunTheme', () => {
  const madrid = [40.4168, -3.7038] as const
  // Madrid, 21 June 2026: sunrise about 04:44 UTC, sunset about 19:48 UTC.

  it('is Day until an hour before sunset', () => {
    const r = sunTheme(new Date('2026-06-21T12:00:00Z'), ...madrid)
    expect(r.theme).toBe('day')
    expect(minutesApart(r.next, new Date('2026-06-21T18:48:00Z'))).toBeLessThan(4)
  })

  it('is Evening from an hour before sunset until 45 minutes after it', () => {
    const r = sunTheme(new Date('2026-06-21T19:00:00Z'), ...madrid)
    expect(r.theme).toBe('evening')
    expect(minutesApart(r.next, new Date('2026-06-21T20:33:00Z'))).toBeLessThan(4)
    expect(sunTheme(new Date('2026-06-21T20:20:00Z'), ...madrid).theme).toBe('evening')
  })

  it('is Night desk from 45 minutes after sunset until sunrise', () => {
    const r = sunTheme(new Date('2026-06-21T22:00:00Z'), ...madrid)
    expect(r.theme).toBe('night')
    expect(minutesApart(r.next, new Date('2026-06-22T04:44:00Z'))).toBeLessThan(5)
    const early = sunTheme(new Date('2026-06-22T03:00:00Z'), ...madrid)
    expect(early.theme).toBe('night')
    expect(minutesApart(early.next, new Date('2026-06-22T04:44:00Z'))).toBeLessThan(5)
  })

  it('handles polar day and polar night', () => {
    expect(sunTheme(new Date('2026-06-21T23:00:00Z'), 69.65, 18.96).theme).toBe('day')
    const r = sunTheme(new Date('2026-12-21T12:00:00Z'), 69.65, 18.96)
    expect(r.theme).toBe('night')
    expect(r.next.getTime()).toBeGreaterThan(Date.parse('2026-12-21T12:00:00Z'))
  })
})

describe('privacy helpers', () => {
  it('rounds coordinates to about 11 km', () => {
    expect(coarse(40.41678)).toBe(40.4)
    expect(coarse(-3.70379)).toBe(-3.7)
  })

  it('estimates a position from the time zone', () => {
    const est = estimateFromTimezone()
    expect(est.lat).toBe(40)
    expect(Math.abs(est.lon)).toBeLessThanOrEqual(180)
  })
})
