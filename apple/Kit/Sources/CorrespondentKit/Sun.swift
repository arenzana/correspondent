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

    private static let rad = Double.pi / 180
    private static let day: TimeInterval = 86_400

    private static var utc: Calendar {
        var c = Calendar(identifier: .gregorian)
        c.timeZone = TimeZone(identifier: "UTC")!
        return c
    }

    /// The UTC midnight of the calendar day `date` falls on at this
    /// longitude's local solar time. Its sunrise and sunset bracket that
    /// day's noon, so they belong to the day the reader is living, wherever
    /// they are: by the UTC day, an evening in San Francisco is already tomorrow.
    private static func solarDay(_ date: Date, lon: Double) -> Date {
        utc.startOfDay(for: date.addingTimeInterval(lon * 240)) // 4 minutes a degree
    }

    /// When the sun crosses the given zenith angle on date's local solar day (NOAA equations).
    private static func solarEvent(_ date: Date, lat: Double, lon: Double, zenith: Double) -> Times {
        let midnight = solarDay(date, lon: lon)
        let dayOfYear = Double(utc.ordinality(of: .day, in: .year, for: midnight) ?? 1)
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

    /// Evening starts this long before sunset; Night desk this long after it.
    public static let eveningBefore: TimeInterval = 60 * 60
    public static let nightAfter: TimeInterval = 45 * 60

    /// Which theme applies at `now`, and when it next changes: Day from
    /// sunrise until an hour before sunset, Evening from then until 45
    /// minutes after sunset, Night desk from then until sunrise.
    public static func theme(_ now: Date, lat: Double, lon: Double) -> (phase: Phase, next: Date) {
        let sun = times(now, lat: lat, lon: lon)
        if let polar = sun.polar {
            // Midnight sun or polar night: re-check at the next local midnight.
            let next = solarDay(now, lon: lon).addingTimeInterval(day - lon * 240)
            return (polar == .day ? .day : .night, next)
        }
        let sunrise = sun.sunrise!, sunset = sun.sunset!
        // On very short days evening can't start before the sun is up.
        let evening = max(sunrise, sunset.addingTimeInterval(-eveningBefore))
        let night = sunset.addingTimeInterval(nightAfter)
        if now < sunrise { return (.night, sunrise) }
        if now < evening { return (.day, evening) }
        if now < night { return (.evening, night) }
        let nextSunrise = times(now.addingTimeInterval(day), lat: lat, lon: lon).sunrise ?? now.addingTimeInterval(3600)
        return (.night, nextSunrise)
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
