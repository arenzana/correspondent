import SwiftUI

/// The app's icon: wire (the default), byline or morse, as in the web app.
/// iOS switches its home-screen icon; the Mac shows it in the Dock while
/// the app runs (macOS doesn't let an app change its icon for good).
enum AppIconChoice: String, CaseIterable, Identifiable {
    case wire, byline, morse
    var id: String { rawValue }
    var label: String { rawValue.capitalized }

    /// The alternate icon's name in the asset catalog; nil is the primary.
    var alternate: String? { self == .wire ? nil : "AppIcon-" + label }

    var preview: Image {
        #if os(iOS)
        Image(uiImage: UIImage(named: "icon-\(rawValue).png") ?? UIImage())
        #else
        Image(nsImage: NSImage(named: "icon-\(rawValue).png") ?? NSImage())
        #endif
    }

    @MainActor
    func apply() {
        #if os(iOS)
        guard UIApplication.shared.supportsAlternateIcons, UIApplication.shared.alternateIconName != alternate else { return }
        UIApplication.shared.setAlternateIconName(alternate)
        #else
        if let url = Bundle.main.url(forResource: rawValue, withExtension: "png"), let img = NSImage(contentsOf: url) {
            NSApplication.shared.applicationIconImage = img
        }
        #endif
    }
}
