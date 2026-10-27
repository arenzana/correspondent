import SwiftUI
import WidgetKit
import CorrespondentKit

// Home Screen and Lock Screen widgets: the unread count, and the newest
// unread headlines on the time spine. They read the server with the app's
// shared sign-in (App Group and Keychain group).

struct Snapshot: TimelineEntry {
    var date = Date()
    var unread = 0
    var headlines: [Headline] = []
    var signedIn = true

    struct Headline: Hashable { var id: EntryID; var title: String; var feed: String; var time: Date }

    static let sample = Snapshot(unread: 12, headlines: [
        Headline(id: "1", title: "The patience of a news desk", feed: "Field Notes", time: .now.addingTimeInterval(-1800)),
        Headline(id: "2", title: "Optical sizes, finally on the web", feed: "Quiet Type", time: .now.addingTimeInterval(-7200)),
        Headline(id: "3", title: "What a harbour pilot knows about latency", feed: "Port & Starboard", time: .now.addingTimeInterval(-10800)),
        Headline(id: "4", title: "Letters to a younger engineer", feed: "The Long View", time: .now.addingTimeInterval(-14400)),
    ])
}

struct Provider: TimelineProvider {
    func placeholder(in context: Context) -> Snapshot { .sample }

    func getSnapshot(in context: Context, completion: @escaping (Snapshot) -> Void) {
        if context.isPreview { completion(.sample); return }
        // WidgetKit hands the callback over to be called later, from anywhere.
        nonisolated(unsafe) let done = completion
        Task { done(await Self.load()) }
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<Snapshot>) -> Void) {
        nonisolated(unsafe) let done = completion
        Task {
            let s = await Self.load()
            done(Timeline(entries: [s], policy: .after(Date().addingTimeInterval(15 * 60))))
        }
    }

    static func load() async -> Snapshot {
        guard let (account, token) = Account.load() else { return Snapshot(signedIn: false) }
        let client = Client(server: account.server, token: token)
        guard let tree = try? await client.tree() else { return Snapshot() }
        let titles = Dictionary(uniqueKeysWithValues: tree.feeds.map { ($0.id, $0.title) })
        let page = try? await client.entries(.init(view: "unread", limit: 6))
        return Snapshot(unread: tree.counts.unread, headlines: (page?.entries ?? []).map {
            .init(id: $0.id, title: $0.title, feed: titles[$0.feedId] ?? "", time: $0.published)
        })
    }
}

/// The app opens these: correspondent://unread, correspondent://entry/ID.
enum Links {
    static let unread = URL(string: "correspondent://unread")!
    static func entry(_ id: EntryID) -> URL { URL(string: "correspondent://entry/\(id)")! }
}

// MARK: Unread

struct UnreadWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "unread", provider: Provider()) { s in UnreadView(s: s) }
            .configurationDisplayName("Unread")
            .description("How many articles are waiting.")
            .supportedFamilies([.systemSmall, .accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}

struct UnreadView: View {
    @Environment(\.widgetFamily) private var family
    @Environment(\.colorScheme) private var scheme
    var s: Snapshot

    var body: some View {
        let theme = Theme.resolve("auto", scheme: scheme)
        Group {
            switch family {
            case .accessoryCircular:
                ZStack {
                    AccessoryWidgetBackground()
                    VStack(spacing: 0) {
                        Text("\(s.unread)").font(.system(size: 20, weight: .semibold, design: .rounded)).minimumScaleFactor(0.5)
                        Text("unread").font(.system(size: 9))
                    }
                }
            case .accessoryInline:
                Text("\(s.unread) unread")
            case .accessoryRectangular:
                VStack(alignment: .leading, spacing: 1) {
                    Text("\(s.unread) unread").font(.headline)
                    if let h = s.headlines.first { Text(h.title).font(.caption).lineLimit(2) }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            default:
                VStack(alignment: .leading, spacing: 0) {
                    MarkGlyph().frame(width: 26, height: 26)
                    Spacer()
                    Text(s.signedIn ? "\(s.unread)" : "–")
                        .font(Typeface.read.font(44, weight: 560, opsz: 72)).foregroundStyle(theme.ink)
                        .minimumScaleFactor(0.5).lineLimit(1)
                    Text("unread").font(Typeface.ui.font(14, weight: 500)).foregroundStyle(theme.slate)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .containerBackground(for: .widget) { theme.sheet }
        .widgetURL(Links.unread)
    }
}

// MARK: Latest

struct LatestWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "latest", provider: Provider()) { s in LatestView(s: s) }
            .configurationDisplayName("Latest")
            .description("The newest unread articles.")
            .supportedFamilies([.systemMedium, .systemLarge])
    }
}

struct LatestView: View {
    @Environment(\.widgetFamily) private var family
    @Environment(\.colorScheme) private var scheme
    var s: Snapshot

    var body: some View {
        let theme = Theme.resolve("auto", scheme: scheme)
        let count = family == .systemLarge ? 6 : 3
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                MarkGlyph().frame(width: 16, height: 16)
                Text("\(s.unread) unread").font(Typeface.ui.font(13, weight: 600)).foregroundStyle(theme.slate)
                Spacer()
            }
            if !s.signedIn {
                Text("Sign in to Correspondent.").font(Typeface.ui.font(14)).foregroundStyle(theme.slate)
            } else if s.headlines.isEmpty {
                Text("All caught up.").font(Typeface.read.font(18, weight: 520)).foregroundStyle(theme.ink)
            }
            ForEach(s.headlines.prefix(count), id: \.self) { h in
                Link(destination: Links.entry(h.id)) {
                    HStack(alignment: .top, spacing: 8) {
                        Circle().fill(theme.accent).frame(width: 7, height: 7).padding(.top, 6)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(h.title).font(Typeface.read.font(15, weight: 600, opsz: 18)).foregroundStyle(theme.ink).lineLimit(2)
                            Text("\(h.feed) · \(h.time.formatted(.dateTime.hour().minute()))")
                                .font(Typeface.ui.font(11.5)).foregroundStyle(theme.slate).lineLimit(1)
                        }
                    }
                }
            }
            Spacer(minLength: 0)
        }
        .containerBackground(for: .widget) { theme.sheet }
        .widgetURL(Links.unread)
    }
}

/// The mark, small: a blue tile with the dot and the ring.
struct MarkGlyph: View {
    var body: some View {
        GeometryReader { g in
            let s = g.size.width / 32
            ZStack {
                RoundedRectangle(cornerRadius: 7 * s, style: .continuous).fill(Color(hex: "#2742c7"))
                Rectangle().fill(Color(hex: "#8291df")).frame(width: 1.2 * s, height: 22 * s).offset(x: -5.7 * s)
                Circle().fill(.white).frame(width: 6 * s).offset(x: -5.7 * s, y: -4.75 * s)
                Circle().stroke(.white, lineWidth: 1.75 * s).frame(width: 5 * s).offset(x: -5.7 * s, y: 4.75 * s)
                Capsule().fill(.white).frame(width: 8.5 * s, height: 1.75 * s).offset(x: 4.5 * s, y: -4.75 * s)
                Capsule().fill(.white).frame(width: 5.5 * s, height: 1.75 * s).offset(x: 3 * s, y: 4.75 * s)
            }
        }
    }
}

@main
struct CorrespondentWidgets: WidgetBundle {
    init() { Typeface.register() }
    var body: some Widget {
        UnreadWidget()
        LatestWidget()
    }
}
