import SwiftUI
import CoreText
import CorrespondentKit

// The web app's design tokens (ui/src/app.css), so the apps look the same:
// fog (chrome), sheet (reading surfaces), ink and slate (text), rule
// (structure), accent (unread, focus, selection), seal (starred), ring
// (the read marker). Three themes share these roles.

struct Theme: Equatable {
    var name: String
    var fog, sheet, ink, slate, rule, accent, seal, ring, danger: Color
    var dark: Bool
    /// The same colours as CSS, for the article page.
    var css: [String: String]

    static let day = Theme(name: "day", hex: [
        "fog": "#eef0f3", "sheet": "#fcfcfb", "ink": "#2a3349", "slate": "#5f6879", "rule": "#d9dee7",
        "accent": "#2742c7", "seal": "#b7791f", "ring": "#aeb6c6", "danger": "#b4322b",
    ], dark: false)

    static let night = Theme(name: "night", hex: [
        "fog": "#141a25", "sheet": "#1b2230", "ink": "#c5ccd8", "slate": "#8a94a7", "rule": "#2b3446",
        "accent": "#8a9cf0", "seal": "#d9a24a", "ring": "#4f5a70", "danger": "#ff8a7a",
    ], dark: true)

    static let evening = Theme(name: "evening", hex: [
        "fog": "#e6dcc8", "sheet": "#f3ebdc", "ink": "#3b3123", "slate": "#6a5c48", "rule": "#d8cbb2",
        "accent": "#2f4a8a", "seal": "#9a6516", "ring": "#b3a383", "danger": "#b4322b",
    ], dark: false)

    init(name: String, hex: [String: String], dark: Bool) {
        self.name = name
        self.dark = dark
        css = hex
        func c(_ k: String) -> Color { Color(hex: hex[k]!) }
        fog = c("fog"); sheet = c("sheet"); ink = c("ink"); slate = c("slate"); rule = c("rule")
        accent = c("accent"); seal = c("seal"); ring = c("ring"); danger = c("danger")
    }

    /// The reader's choice in the web app (auto, sun, day, night, evening) on this device.
    static func resolve(_ pref: String, scheme: ColorScheme, place: Prefs.Place? = nil, now: Date = Date()) -> Theme {
        switch pref {
        case "day": return .day
        case "night": return .night
        case "evening": return .evening
        case "sun": return sun(place: place, now: now).theme
        default: return scheme == .dark ? .night : .day
        }
    }

    /// The Sun theme as the web has it: Day from sunrise, Evening from sunset,
    /// Night desk from nautical dusk; and when it next changes.
    static func sun(place: Prefs.Place?, now: Date = Date()) -> (theme: Theme, phase: Sun.Phase, next: Date) {
        let p = place ?? { let e = Sun.estimate(now: now); return Prefs.Place(lat: e.lat, lon: e.lon, estimated: true) }()
        let r = Sun.theme(now, lat: p.lat, lon: p.lon)
        return (r.phase == .day ? .day : r.phase == .evening ? .evening : .night, r.phase, r.next)
    }

    /// A wash of the accent, as the web's color-mix(… 8%).
    var accentWash: Color { accent.opacity(0.08) }
    var hover: Color { ink.opacity(0.04) }
}

extension Color {
    init(hex: String) {
        var v: UInt64 = 0
        Scanner(string: String(hex.dropFirst())).scanHexInt64(&v)
        self.init(.sRGB, red: Double((v >> 16) & 0xff) / 255, green: Double((v >> 8) & 0xff) / 255, blue: Double(v & 0xff) / 255)
    }
}

private struct ThemeKey: EnvironmentKey { static let defaultValue = Theme.day }
extension EnvironmentValues {
    var theme: Theme {
        get { self[ThemeKey.self] }
        set { self[ThemeKey.self] = newValue }
    }
}

// MARK: Type

/// The web's two typefaces, at the weights and optical sizes it uses:
/// Newsreader for reading (titles, articles), Schibsted Grotesk for the
/// interface. Both are variable fonts, set through their axes.
enum Typeface {
    case read, ui

    var postScriptName: String { self == .read ? "Newsreader16pt-Regular" : "SchibstedGrotesk-Regular" }

    /// A font at a size and weight (100–900), and for Newsreader an optical
    /// size. On iPhone and iPad the size follows the reader's text size
    /// (Dynamic Type), up to the first accessibility size.
    func font(_ size: CGFloat, weight: CGFloat = 400, opsz: CGFloat? = nil, italic: Bool = false) -> Font {
        Font(ctFont(Self.interface(size), weight: weight, opsz: opsz, italic: italic))
    }

    /// The interface size the reader chose on this device (the web's
    /// Interface text size: 0.92, 1, 1.1, 1.2). Set by the app model.
    nonisolated(unsafe) static var uiScale: CGFloat = 1

    /// A size in the interface: Dynamic Type, then the reader's interface size.
    static func interface(_ size: CGFloat) -> CGFloat { scaled(size) * uiScale }

    /// The Dynamic Type scale for a size (1 on the Mac, and at the default text size).
    static func scaled(_ size: CGFloat) -> CGFloat {
        #if os(iOS)
        var category = UITraitCollection.current.preferredContentSizeCategory
        if category == .unspecified { category = .large } // outside any view: the default
        let capped = min(category, .accessibilityMedium)
        return UIFontMetrics.default.scaledValue(for: size, compatibleWith: UITraitCollection(preferredContentSizeCategory: capped))
        #else
        return size
        #endif
    }

    func ctFont(_ size: CGFloat, weight: CGFloat = 400, opsz: CGFloat? = nil, italic: Bool = false) -> CTFont {
        let name = italic ? postScriptName.replacingOccurrences(of: "Regular", with: "Italic") : postScriptName
        var axes: [NSNumber: NSNumber] = [Self.tag("wght"): NSNumber(value: Double(weight))]
        if self == .read { axes[Self.tag("opsz")] = NSNumber(value: Double(opsz ?? min(max(size, 6), 72))) }
        let desc = CTFontDescriptorCreateWithAttributes([
            kCTFontNameAttribute: name,
            kCTFontVariationAttribute: axes,
        ] as CFDictionary)
        return CTFontCreateWithFontDescriptor(desc, size, nil)
    }

    private static func tag(_ s: String) -> NSNumber {
        NSNumber(value: s.utf8.reduce(UInt32(0)) { $0 << 8 | UInt32($1) })
    }

    /// Registers the bundled fonts once, at launch.
    static func register() {
        for name in ["Newsreader", "Newsreader-Italic", "SchibstedGrotesk", "SchibstedGrotesk-Italic"] {
            if let url = Bundle.main.url(forResource: name, withExtension: "ttf") {
                CTFontManagerRegisterFontsForURL(url as CFURL, .process, nil)
            }
        }
    }
}
