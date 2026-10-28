import SwiftUI

/// All caught up: the paper read, a cup of coffee still steaming. Drawn
/// once here and in ui/src/components/CaughtUp.svelte; keep the two the
/// same. The paths are the web's SVG paths, in its 160 × 80 view box.
struct CaughtUp: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var rising = false

    var body: some View {
        ZStack {
            line("M6 100.5 L154 100.5", theme.rule, 1.5)
            filled("M16 63 L78 57 L82 99 L20 100.5 Z", theme.sheet, theme.ink)
            line("M48.5 60 L51.5 99.8", theme.rule, 1.2)
            line("M24 68 L56 64.9", theme.accent, 3)
            line("M25 75 L43 73.3 M25.5 81 L43.5 79.3 M26 87 L44 85.3 M26.5 93 L44.5 91.3 M56 71.3 L73 69.7 M56.5 77.3 L73.5 75.7 M57 83.3 L74 81.7 M57.5 89.3 L74.5 87.7 M58 95 L68 94", theme.ring, 1.4)
            filled("M86 57 C86 61.4 82.4 65 78 65 C73.6 65 70 61.4 70 57 C70 52.6 73.6 49 78 49 C82.4 49 86 52.6 86 57 Z", theme.sheet, theme.accent)
            line("M74.5 57.3 L77 59.8 L81.8 54.6", theme.accent, 1.8)
            filled("M90 99 L150 99 Q146 104.5 120 104.5 Q94 104.5 90 99 Z", theme.fog, theme.ink)
            line("M141 76 C153 73 155 90 138 90", theme.ink, 1.6)
            filled("M98 70 L142 70 Q141 96 120 97.5 Q99 96 98 70 Z", theme.sheet, theme.ink)
            filled("M98 70 Q120 63 142 70 Q120 77 98 70 Z", theme.seal, theme.ink)
            steam("M111 58 C107 52 115 48 111 41", delay: 0)
            steam("M120.5 56 C116.5 49 124.5 45 120.5 36", delay: 0.9)
            steam("M130 58 C126 52 134 48 130 41", delay: 1.8)
        }
        .aspectRatio(2, contentMode: .fit)
        .frame(maxWidth: 200)
        .accessibilityHidden(true)
        .onAppear { if !reduceMotion { rising = true } }
    }

    private func line(_ d: String, _ color: Color, _ width: CGFloat = 1.6) -> some View {
        Drawing(d: d).stroke(color, style: StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round))
    }

    private func filled(_ d: String, _ fill: Color, _ stroke: Color) -> some View {
        ZStack { Drawing(d: d).fill(fill); line(d, stroke) }
    }

    private func steam(_ d: String, delay: Double) -> some View {
        line(d, theme.slate, 1.5)
            .opacity(rising ? 0.3 : 0.75)
            .offset(y: rising ? -3 : 1)
            .animation(.easeInOut(duration: 2.8).repeatForever(autoreverses: true).delay(delay), value: rising)
    }
}

/// An SVG path (absolute M, L, Q, C and Z only) in the 160 × 80 view box
/// starting at y 30, scaled to fill its frame.
private struct Drawing: Shape {
    var d: String

    func path(in rect: CGRect) -> Path {
        let scale = min(rect.width / 160, rect.height / 80)
        var numbers: [CGFloat] = []
        var command: Character = "M"
        var path = Path()
        func point(_ i: Int) -> CGPoint {
            CGPoint(x: rect.minX + numbers[i] * scale, y: rect.minY + (numbers[i + 1] - 30) * scale)
        }
        func flush() {
            switch command {
            case "M" where numbers.count >= 2: path.move(to: point(0))
            case "L" where numbers.count >= 2: path.addLine(to: point(0))
            case "Q" where numbers.count >= 4: path.addQuadCurve(to: point(2), control: point(0))
            case "C" where numbers.count >= 6: path.addCurve(to: point(4), control1: point(0), control2: point(2))
            case "Z": path.closeSubpath()
            default: break
            }
            numbers = []
        }
        for token in d.split(separator: " ") {
            if let c = token.first, c.isLetter {
                flush()
                command = c
                if c == "Z" { flush(); command = " " }
            }
            if let n = Double(token.drop(while: \.isLetter)) {
                numbers.append(CGFloat(n))
            }
        }
        flush()
        return path
    }
}
