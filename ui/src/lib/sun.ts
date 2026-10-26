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

  // 90.833° accounts for refraction and the sun's apparent radius.
  const cosHA = Math.cos(90.833 * rad) / (Math.cos(lat * rad) * Math.cos(decl)) - Math.tan(lat * rad) * Math.tan(decl)
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

export type SunTheme = 'day' | 'evening'

/** Which theme applies at `now`, and when it next changes. */
export function sunTheme(now: Date, lat: number, lon: number): { theme: SunTheme; next: Date } {
  const t = sunTimes(now, lat, lon)
  const tomorrow = new Date(now.getTime() + 86_400_000)
  if (t.polar) {
    // Re-check at the next UTC midnight; polar periods change slowly.
    const next = new Date(Date.UTC(tomorrow.getUTCFullYear(), tomorrow.getUTCMonth(), tomorrow.getUTCDate()))
    return { theme: t.polar === 'day' ? 'day' : 'evening', next }
  }
  const { sunrise, sunset } = t as { sunrise: Date; sunset: Date }
  if (now < sunrise) return { theme: 'evening', next: sunrise }
  if (now < sunset) return { theme: 'day', next: sunset }
  return { theme: 'evening', next: sunTimes(tomorrow, lat, lon).sunrise ?? new Date(now.getTime() + 3_600_000) }
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
