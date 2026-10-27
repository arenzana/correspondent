import Foundation

// Sunrise and sunset from the NOAA solar equations, accurate to a minute or
// two, which is plenty for switching a theme. The web's ui/src/lib/sun.ts,
// line for line, so the Sun theme changes at the same moment everywhere.

public enum Sun {
    public struct Times: Sendable {
        public var sunrise: Date?
        public var sunset: Date?
        /// Set when the sun never rises (polar night) or never sets (midnight sun).
        public var polar: Phase?
    }

    /// The themes the Sun theme moves through: day, evening, night.
    public enum Phase: String, Sendable { case day, evening, night }

    public static func times(_ date: Date, lat: Double, lon: Double) -> Times {
        solarEvent(date, lat: lat, lon: lon, zenith: 90.833) // refraction plus the sun's radius
    }

    /// Nautical twilight: the sun 12° below the horizon; the sky is dark after it.
    public static func nauticalTwilight(_ date: Date, lat: Double, lon: Double) -> Times {
        solarEvent(date, lat: lat, lon: lon, zenith: 102)
    }

    private static let rad = Double.pi / 180
    private static let day: TimeInterval = 86_400

    private static var utc: Calendar {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = TimeZone(identifier: "UTC")!
        return c
    }

    /// When the sun crosses the given zenith angle on date's UTC day (NOAA equations).
    private static func solarEvent(_ date: Date, lat: Double, lon: Double, zenith: Double) -> Times {
        let midnight = utc.startOfDay(for: date)
        let dayOfYear = Double(utc.ordinality(of: .day, in: .year, for: date) ?? 1)
        let g = (2 * Double.pi / 365) * (dayOfYear - 1) // fractional year at noon

        let eqTime = 229.18 * (0.000075 + 0.001868 * cos(g) - 0.032077 * sin(g) - 0.014615 * cos(2 * g) - 0.040849 * sin(2 * g))
        let decl = 0.006918 - 0.399912 * cos(g) + 0.070257 * sin(g) - 0.006758 * cos(2 * g)
            + 0.000907 * sin(2 * g) - 0.002697 * cos(3 * g) + 0.00148 * sin(3 * g)

        let cosHA = cos(zenith * rad) / (cos(lat * rad) * cos(decl)) - tan(lat * rad) * tan(decl)
        if cosHA > 1 { return Times(polar: .night) }
        if cosHA < -1 { return Times(polar: .day) }
        let ha = acos(cosHA) / rad

        func at(_ minutes: Double) -> Date { midnight.addingTimeInterval(minutes * 60) }
        return Times(sunrise: at(720 - 4 * (lon + ha) - eqTime), sunset: at(720 - 4 * (lon - ha) - eqTime))
    }

    /// Which theme applies at `now`, and when it next changes: Day from
    /// sunrise to sunset, Evening from sunset until nautical dusk (the sun 12°
    /// down), Night desk from then until sunrise. Where the sun never gets
    /// that low (summer at high latitudes), Evening lasts until sunrise.
    public static func theme(_ now: Date, lat: Double, lon: Double) -> (phase: Phase, next: Date) {
        let sun = times(now, lat: lat, lon: lon)
        if let polar = sun.polar {
            // Midnight sun or polar night: re-check at the next UTC midnight.
            let next = utc.startOfDay(for: now.addingTimeInterval(day))
            return (polar == .day ? .day : .night, next)
        }
        let sunrise = sun.sunrise!, sunset = sun.sunset!
        func nextSunrise() -> Date { times(now.addingTimeInterval(day), lat: lat, lon: lon).sunrise ?? now.addingTimeInterval(3600) }

        if now >= sunrise && now < sunset { return (.day, sunset) }

        // Between sunset and midnight, or after midnight before sunrise: find
        // the dusk that follows the most recent sunset (it may fall after midnight).
        let lastSunset = now >= sunset ? sunset : times(now.addingTimeInterval(-day), lat: lat, lon: lon).sunset
        let dusk = lastSunset.map { nauticalTwilight($0, lat: lat, lon: lon) }
        let duskEnd = dusk?.polar == nil ? dusk?.sunset : nil // nil: the sky never gets dark
        let until = now >= sunset ? nextSunrise() : sunrise
        guard let duskEnd, duskEnd >= (lastSunset ?? now) else { return (.evening, until) }
        if now < duskEnd { return (.evening, duskEnd) }
        return (.night, until)
    }

    /// Rough position from the time zone, for when there is no saved place.
    public static func estimate(now: Date = Date(), zone: TimeZone = .current) -> (lat: Double, lon: Double) {
        // UTC offset gives longitude to within a time zone; latitude defaults
        // to the mid-latitudes where most readers live.
        (40, coarse(Double(zone.secondsFromGMT(for: now)) / 240))
    }

    /// Round to one decimal (about 11 km) before a location is ever stored.
    public static func coarse(_ n: Double) -> Double { (n * 10).rounded() / 10 }
}
