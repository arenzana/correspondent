// Sunrise and sunset from the NOAA solar equations, accurate to a minute or
// two, which is plenty for switching a theme. Everything runs on the device.

export interface SunTimes {
  sunrise: Date | null
  sunset: Date | null
  /** Set when the sun never rises (polar night) or never sets (midnight sun). */
  polar: 'day' | 'night' | null
}

const rad = Math.PI / 180

export function sunTimes(date: Date, lat: number, lon: number): SunTimes {
  return solarEvent(date, lat, lon, 90.833) // refraction plus the sun's radius
}

/** When the sun crosses the given zenith angle on date's UTC day (NOAA equations). */
function solarEvent(date: Date, lat: number, lon: number, zenith: number): SunTimes {
  const midnightUTC = Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate())
  const dayOfYear = (midnightUTC - Date.UTC(date.getUTCFullYear(), 0, 1)) / 86_400_000 + 1
  const g = ((2 * Math.PI) / 365) * (dayOfYear - 1) // fractional year at noon

  const eqTime =
    229.18 *
    (0.000075 + 0.001868 * Math.cos(g) - 0.032077 * Math.sin(g) - 0.014615 * Math.cos(2 * g) - 0.040849 * Math.sin(2 * g))
  const decl =
    0.006918 -
    0.399912 * Math.cos(g) +
    0.070257 * Math.sin(g) -
    0.006758 * Math.cos(2 * g) +
    0.000907 * Math.sin(2 * g) -
    0.002697 * Math.cos(3 * g) +
    0.00148 * Math.sin(3 * g)

  const cosHA = Math.cos(zenith * rad) / (Math.cos(lat * rad) * Math.cos(decl)) - Math.tan(lat * rad) * Math.tan(decl)
  if (cosHA > 1) return { sunrise: null, sunset: null, polar: 'night' }
  if (cosHA < -1) return { sunrise: null, sunset: null, polar: 'day' }
  const ha = Math.acos(cosHA) / rad

  const at = (minutes: number) => new Date(midnightUTC + minutes * 60_000)
  return {
    sunrise: at(720 - 4 * (lon + ha) - eqTime),
    sunset: at(720 - 4 * (lon - ha) - eqTime),
    polar: null,
  }
}

export type SunTheme = 'day' | 'evening' | 'night'

/** Evening starts this long before sunset; Night desk this long after it. */
export const EVENING_BEFORE = 60 * 60_000
export const NIGHT_AFTER = 45 * 60_000

/**
 * Which theme applies at `now`, and when it next changes: Day from sunrise
 * until an hour before sunset, Evening from then until 45 minutes after
 * sunset, Night desk from then until sunrise.
 */
export function sunTheme(now: Date, lat: number, lon: number): { theme: SunTheme; next: Date } {
  const day = 86_400_000
  const sun = sunTimes(now, lat, lon)
  if (sun.polar) {
    // Midnight sun or polar night: re-check at the next UTC midnight.
    const tomorrow = new Date(now.getTime() + day)
    const next = new Date(Date.UTC(tomorrow.getUTCFullYear(), tomorrow.getUTCMonth(), tomorrow.getUTCDate()))
    return { theme: sun.polar === 'day' ? 'day' : 'night', next }
  }
  const { sunrise, sunset } = sun as { sunrise: Date; sunset: Date }
  // On very short days evening can't start before the sun is up.
  const evening = new Date(Math.max(sunrise.getTime(), sunset.getTime() - EVENING_BEFORE))
  const night = new Date(sunset.getTime() + NIGHT_AFTER)
  if (now < sunrise) return { theme: 'night', next: sunrise }
  if (now < evening) return { theme: 'day', next: evening }
  if (now < night) return { theme: 'evening', next: night }
  const nextSunrise = sunTimes(new Date(now.getTime() + day), lat, lon).sunrise ?? new Date(now.getTime() + 3_600_000)
  return { theme: 'night', next: nextSunrise }
}

/** Round to one decimal (about 11 km) before a location is ever stored. */
export function coarse(n: number): number {
  return Math.round(n * 10) / 10
}

/** Rough position from the time zone, for when location access is refused. */
export function estimateFromTimezone(now = new Date()): { lat: number; lon: number } {
  // UTC offset gives longitude to within a time zone; latitude defaults to
  // the mid-latitudes where most readers live.
  return { lat: 40, lon: coarse(-now.getTimezoneOffset() / 4) }
}
