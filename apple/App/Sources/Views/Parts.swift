import SwiftUI
import CorrespondentKit

/// The Correspondent mark, as beside the web's wordmark (ui/src/components/Mark.svelte):
/// a blue tile with the wire's spine, an unread dot and a read ring.
struct Mark: View {
    var size: CGFloat = 28

    var body: some View {
        Canvas { ctx, sz in
            let s = sz.width / 1024
            let tile = Path(roundedRect: CGRect(origin: .zero, size: sz), cornerRadius: 230 * s, style: .continuous)
            ctx.fill(tile, with: .color(Color(hex: "#2742c7")))
            var spine = Path()
            spine.move(to: CGPoint(x: 328 * s, y: 160 * s))
            spine.addLine(to: CGPoint(x: 328 * s, y: 864 * s))
            ctx.stroke(spine, with: .color(Color(hex: "#8291df")), style: StrokeStyle(lineWidth: 28 * s, lineCap: .round))
            ctx.fill(Path(ellipseIn: CGRect(x: 232 * s, y: 264 * s, width: 192 * s, height: 192 * s)), with: .color(.white))
            let ring = Path(ellipseIn: CGRect(x: 256 * s, y: 592 * s, width: 144 * s, height: 144 * s))
            ctx.fill(ring, with: .color(Color(hex: "#2742c7")))
            ctx.stroke(ring, with: .color(.white), lineWidth: 56 * s)
            var lines = Path()
            lines.move(to: CGPoint(x: 528 * s, y: 360 * s)); lines.addLine(to: CGPoint(x: 792 * s, y: 360 * s))
            lines.move(to: CGPoint(x: 528 * s, y: 664 * s)); lines.addLine(to: CGPoint(x: 704 * s, y: 664 * s))
            ctx.stroke(lines, with: .color(.white), style: StrokeStyle(lineWidth: 56 * s, lineCap: .round))
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// A feed's icon from the server's image proxy, or its initial on a tile.
struct FeedIcon: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    var feed: Feed?
    var size: CGFloat = 16

    var body: some View {
        let url = feed.flatMap { app.client?.absolute($0.iconUrl) }
        RemoteImage(url: url, size: CGSize(width: size, height: size)) {
            let letter = (feed?.title ?? "?").first.map(String.init) ?? "?"
            ZStack {
                RoundedRectangle(cornerRadius: size * 0.22, style: .continuous).fill(theme.slate.opacity(0.85))
                Text(letter.uppercased()).font(Typeface.read.font(size * 0.62, weight: 700)).foregroundStyle(.white)
            }
        }
        .frame(width: size, height: size)
        .clipShape(RoundedRectangle(cornerRadius: size * 0.22, style: .continuous))
        .accessibilityHidden(true)
    }
}

/// A count beside a row, as the web's .count.
struct Count: View {
    @Environment(\.theme) private var theme
    var n: Int
    var body: some View {
        if n > 0 {
            Text("\(n)").font(Typeface.ui.font(12.5, weight: 500)).monospacedDigit().foregroundStyle(theme.slate)
        }
    }
}

/// A brief message at the bottom, as the web's toasts.
struct Toast: View {
    @Environment(\.theme) private var theme
    var message: String
    var body: some View {
        Text(message)
            .font(Typeface.ui.font(14))
            .foregroundStyle(theme.dark ? theme.fog : theme.sheet)
            .padding(.horizontal, 16).padding(.vertical, 11)
            .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(theme.dark ? theme.ink : Color(hex: "#2a3349")))
            .shadow(color: .black.opacity(0.16), radius: 20, y: 8)
            .padding(.bottom, 24)
            .transition(.move(edge: .bottom).combined(with: .opacity))
    }
}

extension View {
    /// The web's .icon-btn: a quiet square button.
    func iconButton() -> some View {
        self.buttonStyle(IconButtonStyle())
    }
}

struct IconButtonStyle: ButtonStyle {
    @Environment(\.theme) private var theme
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 17, weight: .regular))
            .foregroundStyle(theme.slate)
            .frame(width: 36, height: 36)
            .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(configuration.isPressed ? theme.hover : .clear))
            .contentShape(Rectangle())
    }
}
