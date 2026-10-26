import { describe, expect, it } from 'vitest'
import { coarse, estimateFromTimezone, nauticalTwilight, sunTheme, sunTimes } from './sun'

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

  it('is Day between sunrise and sunset, switching at sunset', () => {
    const r = sunTheme(new Date('2026-06-21T12:00:00Z'), ...madrid)
    expect(r.theme).toBe('day')
    expect(minutesApart(r.next, new Date('2026-06-21T19:48:00Z'))).toBeLessThan(4)
  })

  it('is Evening from sunset until nautical dusk', () => {
    // NOAA: Madrid, 21 June 2026, nautical dusk about 20:58 UTC.
    const r = sunTheme(new Date('2026-06-21T20:00:00Z'), ...madrid)
    expect(r.theme).toBe('evening')
    expect(minutesApart(r.next, new Date('2026-06-21T20:58:00Z'))).toBeLessThan(6)
  })

  it('is Night desk after nautical dusk until sunrise', () => {
    const r = sunTheme(new Date('2026-06-21T22:00:00Z'), ...madrid)
    expect(r.theme).toBe('night')
    expect(minutesApart(r.next, new Date('2026-06-22T04:44:00Z'))).toBeLessThan(5)
    expect(sunTheme(new Date('2026-06-22T03:00:00Z'), ...madrid).theme).toBe('night')
  })

  it('stays Evening all night where the sky never gets dark', () => {
    // Oslo in June: the sun stays above -12°, so no nautical dusk.
    expect(nauticalTwilight(new Date('2026-06-21T12:00:00Z'), 59.91, 10.75).polar).toBe('day')
    const r = sunTheme(new Date('2026-06-21T23:30:00Z'), 59.91, 10.75)
    expect(r.theme).toBe('evening')
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
