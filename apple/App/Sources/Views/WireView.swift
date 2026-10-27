import SwiftUI
import CorrespondentKit

/// The list of articles, as the web's wire (ui/src/components/Wire.svelte):
/// articles hang off a vertical time spine, grouped by day. A filled dot
/// is unread, a hollow ring read, a seal starred; the selection washes the
/// row and turns its stretch of spine accent.
struct WireView: View {
    @Environment(AppModel.self) private var app
    @Environment(Player.self) private var player
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var onOpen: (EntryID) -> Void = { _ in }
    /// Pushed on iPhone: the navigation bar shows the title.
    var pushed = false
    var leading: AnyView? = nil
    /// Below the list: the sidebar's own actions while the sidebar is hidden.
    var footer: AnyView? = nil

    /// The time's column, wider with larger text (Dynamic Type).
    static var gutter: CGFloat { Typeface.interface(52) }
    static var spineX: CGFloat { gutter + 9 }

    var body: some View {
        VStack(spacing: 0) {
            header
            feedNotice
            if app.outboxCount > 0 {
                Text("\(app.outboxCount) \(app.outboxCount == 1 ? "change" : "changes") waiting to be sent.")
                    .font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 18).padding(.vertical, 6)
                    .background(theme.fog)
            }
            if !app.online {
                Text("Offline. Reading the copy kept on this device.")
                    .font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(.horizontal, 18).padding(.vertical, 6)
                    .background(theme.fog)
            }
            ZStack(alignment: .top) {
                if app.entries.isEmpty && !app.loading {
                    empty
                } else {
                    list
                }
                if app.pending > 0 {
                    Button {
                        Task { await app.load() }
                    } label: {
                        Text("\(app.pending) new \(app.pending == 1 ? "article" : "articles")")
                            .font(Typeface.ui.font(13.5, weight: 560)).foregroundStyle(.white)
                            .padding(.horizontal, 14).padding(.vertical, 7)
                            .background(Capsule().fill(theme.accent))
                            .shadow(color: .black.opacity(0.15), radius: 8, y: 3)
                    }
                    .buttonStyle(.plain)
                    .padding(.top, 10)
                    .transition(.move(edge: .top).combined(with: .opacity))
                }
            }
            .animation(reduceMotion ? nil : .spring(duration: 0.35), value: app.pending > 0)
            if let footer { footer }
        }
        .background(theme.sheet)
    }

    private var header: some View {
        HStack(spacing: 8) {
            if let leading { leading }
            VStack(alignment: .leading, spacing: 2) {
                if !pushed {
                    Text(app.query.isEmpty ? app.title : "“\(app.query)”")
                        .font(Typeface.read.font(23, weight: 560, opsz: 36)).foregroundStyle(theme.ink).lineLimit(1)
                }
                Text(app.subtitle).font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate).lineLimit(1)
            }
            Spacer()
            if !app.query.isEmpty, !isSavedSearch {
                Button { Task { await app.saveSearch() } } label: {
                    Text("Save search").font(Typeface.ui.font(13.5, weight: 560)).foregroundStyle(theme.ink)
                        .padding(.horizontal, 10).frame(height: 30)
                        .background(RoundedRectangle(cornerRadius: 6).stroke(theme.rule))
                }
                .buttonStyle(.plain)
                Button { Task { await app.search("") ; await app.go(.all) } } label: { Image(systemName: "xmark") }
                    .iconButton().accessibilityLabel("Clear search")
            }
            unreadSwitch
            if let f = currentFeed {
                Button { Task { await app.updateFeed(f.id, .init(fullText: !f.fullText), done: f.fullText ? "Back to the text the feed publishes." : "Fetching full articles for \(f.title). Recent ones are on their way.") } } label: {
                    Image(systemName: f.fullText ? "doc.text.fill" : "doc.text")
                }
                .iconButton().accessibilityLabel(f.fullText ? "Stop fetching full articles for this feed" : "Fetch full articles for this feed")
                Button { Task { await app.updateFeed(f.id, .init(notify: !f.notify)) } } label: { Image(systemName: f.notify ? "bell.fill" : "bell") }
                    .iconButton().accessibilityLabel(f.notify ? "Turn off notifications for this feed" : "Notify me about new articles in this feed")
            }
            if app.entries.contains(where: { $0.audio && !$0.read }) {
                Button { Task { await player.playList(app.entries, from: app.selectedID) } } label: { Image(systemName: "headphones") }
                    .iconButton().accessibilityLabel("Listen to the unread articles that have audio")
            }
            if app.source != .starred {
                Button { Task { await app.markAllRead() } } label: { Image(systemName: "checkmark.circle") }
                    .iconButton().accessibilityLabel("Mark all read")
            }
        }
        .padding(.leading, 18).padding(.trailing, 12).padding(.vertical, 12)
        .background(theme.sheet)
        .overlay(alignment: .bottom) { Rectangle().fill(theme.rule).frame(height: 1) }
    }

    private var isSavedSearch: Bool {
        if case .search = app.source { return true }
        return false
    }

    private var list: some View {
        ScrollViewReader { proxy in
            List {
                ForEach(groups, id: \.key) { group in
                    Section {
                        ForEach(group.entries) { e in
                            WireRow(entry: e, feed: app.feedsByID[e.feedId], selected: e.id == app.selectedID, arrivedFrom: app.selectionStep)
                                .id(e.id)
                                // Arrivals grow in where they land, as on the web.
                                .transition(.asymmetric(insertion: .opacity.combined(with: .scale(scale: 0.97, anchor: .top)), removal: .opacity))
                                .listRowInsets(EdgeInsets())
                                .listRowSeparator(.hidden)
                                .listRowBackground(Color.clear)
                                .contentShape(Rectangle())
                                .onTapGesture { Task { await app.select(e.id) }; onOpen(e.id) }
                                .swipeActions(edge: .leading) {
                                    Button { Task { await app.toggleRead(e.id) } } label: {
                                        Label(e.read ? "Unread" : "Read", systemImage: e.read ? "circle" : "checkmark")
                                    }
                                    .tint(theme.accent)
                                }
                                .swipeActions(edge: .trailing) {
                                    Button { Task { await app.toggleStar(e.id) } } label: {
                                        Label(e.starred ? "Unstar" : "Star", systemImage: e.starred ? "star.slash" : "star")
                                    }
                                    .tint(theme.seal)
                                }
                                .onAppear { if e.id == app.entries.last?.id { Task { await app.loadMore() } } }
                                .contextMenu { rowMenu(e) }
                        }
                    } header: {
                        DayLabel(label: group.label)
                            .listRowInsets(EdgeInsets())
                    }
                }
                if !app.cursor.isEmpty {
                    ProgressView().frame(maxWidth: .infinity).listRowBackground(Color.clear).listRowSeparator(.hidden)
                } else {
                    // Where the list ends, the spine ends in a short bar.
                    HStack {
                        Rectangle().fill(theme.ring).frame(width: 12, height: 1.5).padding(.leading, Self.spineX - 5)
                        Spacer()
                    }
                    .frame(height: 64)
                    .listRowInsets(EdgeInsets()).listRowSeparator(.hidden).listRowBackground(Color.clear)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.35), value: app.entries.map(\.id))
            .onScrollGeometryChange(for: Bool.self) { $0.contentOffset.y + $0.contentInsets.top < 40 } action: { _, top in
                if app.atTop != top { app.atTop = top }
            }
            .animation(reduceMotion ? nil : .easeOut(duration: 0.18), value: app.selectedID)
            .environment(\.defaultMinListRowHeight, 1)
            .refreshable {
                await app.refreshTree()
                await app.load()
            }
            .onChange(of: app.selectedID) { _, id in
                if let id { withAnimation { proxy.scrollTo(id) } }
            }
        }
    }

    /// What a long-press (or right-click) on an article offers.
    @ViewBuilder
    private func rowMenu(_ e: Entry) -> some View {
        Button { Task { await app.toggleRead(e.id) } } label: {
            Label(e.read ? "Mark as Unread" : "Mark as Read", systemImage: e.read ? "circle" : "checkmark.circle")
        }
        Button { Task { await app.toggleStar(e.id) } } label: {
            Label(e.starred ? "Unstar" : "Star", systemImage: e.starred ? "star.slash" : "star")
        }
        if app.speechConfigured || e.audio {
            Button { Task { await player.play(e) } } label: { Label("Read Aloud", systemImage: "headphones") }
        }
        Divider()
        if let i = app.entries.firstIndex(where: { $0.id == e.id }) {
            let above = app.entries[..<i].filter { !$0.read }.map(\.id)
            let below = app.entries[(i + 1)...].filter { !$0.read }.map(\.id)
            Button { Task { await app.setRead(above, true) } } label: { Label("Mark Above as Read", systemImage: "arrow.up.to.line") }
                .disabled(above.isEmpty)
            Button { Task { await app.setRead(below, true) } } label: { Label("Mark Below as Read", systemImage: "arrow.down.to.line") }
                .disabled(below.isEmpty)
        }
        if let url = URL(string: e.url), !e.url.isEmpty {
            Divider()
            Link(destination: url) { Label("Open Original", systemImage: "safari") }
            ShareLink(item: url, subject: Text(e.title)) { Label("Share…", systemImage: "square.and.arrow.up") }
            Button {
                #if os(iOS)
                UIPasteboard.general.url = url
                #else
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(url.absoluteString, forType: .string)
                #endif
            } label: { Label("Copy Link", systemImage: "link") }
        }
    }

    private var empty: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !app.loadError.isEmpty {
                Text("Couldn't load articles. \(app.loadError)").font(Typeface.ui.font(15)).foregroundStyle(theme.ink)
                Button("Try again") { Task { await app.load() } }.buttonStyle(QuietButton()).padding(.top, 6)
            } else if !app.query.isEmpty {
                headline("No articles match “\(app.query)”.")
                Button("Clear search") { Task { await app.search("") } }.buttonStyle(QuietButton()).padding(.top, 6)
            } else if app.unreadFilter {
                headline("All caught up.")
                note("New articles will appear here as your feeds update.")
                Button("Show today’s articles") { Task { await app.go(.today) } }.buttonStyle(QuietButton()).padding(.top, 6)
            } else if app.source == .starred {
                headline("Nothing starred yet.")
                note("Star an article (s, or swipe) to keep it here.")
            } else {
                headline("No articles yet.")
                note("This feed hasn't published anything Correspondent has seen.")
            }
            Spacer()
        }
        .padding(.leading, Self.spineX + 26).padding(.trailing, 24).padding(.top, 56)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func headline(_ s: String) -> some View { Text(s).font(Typeface.read.font(22, weight: 520)).foregroundStyle(theme.ink) }
    private func note(_ s: String) -> some View { Text(s).font(Typeface.ui.font(15)).foregroundStyle(theme.slate) }

    /// The feed the list shows, if it's one feed.
    private var currentFeed: Feed? {
        if case .feed(let id) = app.source { return app.feedsByID[id] }
        return nil
    }

    /// A paused or failing feed says so above its articles, with the remedy.
    @ViewBuilder private var feedNotice: some View {
        if let f = currentFeed, f.paused || f.errorCount > 0 {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: f.paused ? "pause.circle" : "exclamationmark.triangle").foregroundStyle(theme.slate)
                VStack(alignment: .leading, spacing: 4) {
                    Text(f.paused ? "Updates for this feed are paused." : "Couldn't update this feed. \(f.lastError)")
                        .font(Typeface.ui.font(14)).foregroundStyle(theme.ink).fixedSize(horizontal: false, vertical: true)
                    Button(f.paused ? "Resume updates" : "Retry now") {
                        Task {
                            if f.paused { await app.updateFeed(f.id, .init(paused: false), done: "Updates resumed for \(f.title).") }
                            else { await app.refreshFeed(f.id) }
                        }
                    }
                    .buttonStyle(.plain).font(Typeface.ui.font(14, weight: 560)).foregroundStyle(theme.accent)
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 18).padding(.vertical, 10)
            .background(theme.fog)
            .overlay(alignment: .bottom) { Rectangle().fill(theme.rule).frame(height: 1) }
        }
    }

    /// Feeds and folders can show only their unread articles, or all of them.
    @ViewBuilder private var unreadSwitch: some View {
        switch app.source {
        case .feed, .folder, .search:
            Picker("Show", selection: Binding(get: { app.prefs.unreadOnly }, set: { v in app.setPrefs({ $0.unreadOnly = v }, ["unreadOnly": v]) })) {
                Text("Unread").tag(true)
                Text("All").tag(false)
            }
            .pickerStyle(.segmented).labelsHidden().fixedSize()
        default: EmptyView()
        }
    }

    // MARK: Days

    struct Group { var key: String; var label: String; var entries: [Entry] }

    private var groups: [Group] {
        var out: [Group] = []
        let cal = Calendar.current
        for e in app.entries {
            let day = cal.startOfDay(for: e.published)
            let key = ISO8601DateFormatter().string(from: day)
            if out.last?.key == key { out[out.count - 1].entries.append(e) } else {
                out.append(Group(key: key, label: Self.dayLabel(day), entries: [e]))
            }
        }
        return out
    }

    static func dayLabel(_ day: Date) -> String {
        let cal = Calendar.current
        if cal.isDateInToday(day) { return "Today" }
        if cal.isDateInYesterday(day) { return "Yesterday" }
        if let d = cal.dateComponents([.day], from: day, to: cal.startOfDay(for: Date())).day, d < 7 {
            return day.formatted(.dateTime.weekday(.wide))
        }
        let sameYear = cal.component(.year, from: day) == cal.component(.year, from: Date())
        return sameYear ? day.formatted(.dateTime.weekday(.wide).month(.wide).day()) : day.formatted(.dateTime.month(.wide).day().year())
    }
}

/// A day's label on the spine, with its crossbar.
struct DayLabel: View {
    @Environment(\.theme) private var theme
    var label: String
    var body: some View {
        HStack(spacing: 10) {
            Rectangle().fill(theme.ink).frame(width: 12, height: 1.5)
            Text(label).font(Typeface.ui.font(13.5, weight: 600)).foregroundStyle(theme.ink)
            Spacer()
        }
        .padding(.leading, WireView.spineX - 5)
        .padding(.top, 14).padding(.bottom, 6)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
        .background(alignment: .leading) {
            Rectangle().fill(theme.rule).frame(width: 1.5).padding(.leading, WireView.spineX)
        }
        .background(theme.sheet)
    }
}

/// One article on the wire.
struct WireRow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    var entry: Entry
    var feed: Feed?
    var selected: Bool
    /// Which way the selection came (1 down, -1 up, 0 none): the spine's
    /// wave runs that way.
    var arrivedFrom = 0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// Only a step from a neighbour plucks the string, never with Reduce Motion.
    private var pluck: Bool { arrivedFrom != 0 && !reduceMotion }

    /// The web's list density: compact rows sit closer, one line of excerpt, smaller pictures.
    private var compact: Bool { app.prefs.density == "compact" }
    private var pad: CGFloat { compact ? 9 : 14 }

    private var spoken: String {
        let state = [entry.read ? nil : "Unread", entry.starred ? "starred" : nil].compactMap { $0 }.joined(separator: ", ")
        let when = entry.published.formatted(.relative(presentation: .named))
        return [state, entry.title, "\(feed?.title ?? ""), \(when)"].filter { !$0.isEmpty }.joined(separator: ". ")
    }

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            // Gutter: the time, and the seal for a starred article.
            ZStack(alignment: .topTrailing) {
                Text(entry.published.formatted(.dateTime.hour(.twoDigits(amPM: .omitted)).minute(.twoDigits)))
                    .font(Typeface.ui.font(11.5)).monospacedDigit().foregroundStyle(theme.slate)
                    .lineLimit(1).minimumScaleFactor(0.7)
                    .padding(.top, 3).padding(.trailing, 10)
                if entry.starred {
                    Rectangle().fill(theme.seal).frame(width: 7, height: 7).rotationEffect(.degrees(45))
                        .padding(.top, 20).offset(x: 4)
                }
            }
            .padding(.vertical, pad)
            .frame(width: WireView.gutter, alignment: .trailing)

            // The spine and this article's dot.
            ZStack(alignment: .top) {
                // As the selection lands, its spine ripples like a plucked
                // string, as on the web: a wave pinned at both ends that runs
                // the way it moved, swells, then settles straight.
                Color.clear
                    .keyframeAnimator(initialValue: 1.0, trigger: selected) { _, t in
                        WaveSpine(t: selected && pluck ? t : 1, way: arrivedFrom)
                            .stroke(selected ? theme.accent : theme.rule,
                                    style: StrokeStyle(lineWidth: selected ? 3 : 1.5, lineCap: .round, lineJoin: .round))
                            .frame(width: 12)
                    } keyframes: { _ in
                        MoveKeyframe(0.0)
                        LinearKeyframe(1.0, duration: 0.9)
                    }
                Circle()
                    .fill(entry.read ? theme.sheet : theme.accent)
                    .overlay(Circle().stroke(entry.read ? theme.ring : theme.accent, lineWidth: 1.5))
                    .frame(width: 9, height: 9)
                    .padding(.top, pad + 6)
            }
            .frame(width: 18)

            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    FeedIcon(feed: feed, size: 14)
                    Text(feed?.title ?? "").font(Typeface.ui.font(13)).foregroundStyle(theme.slate).lineLimit(1)
                }
                Text(entry.title.isEmpty ? "Untitled" : entry.title)
                    .font(Typeface.read.font(18.5, weight: entry.read ? 400 : 600, opsz: 24))
                    .foregroundStyle(entry.read ? theme.ink.opacity(0.78) : theme.ink)
                    .lineLimit(3)
                if !entry.excerpt.isEmpty {
                    Text(entry.excerpt).font(Typeface.ui.font(14)).foregroundStyle(theme.slate).lineLimit(compact ? 1 : 2)
                }
            }
            .padding(.leading, 12)
            .padding(.vertical, pad)
            .frame(maxWidth: .infinity, alignment: .leading)

            if let url = app.client?.absolute(entry.imageUrl) {
                RemoteImage(url: url, size: CGSize(width: 76, height: 76)) { theme.fog }
                    .frame(width: compact ? 52 : 76, height: compact ? 52 : 76)
                    .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
                    .padding(.leading, 10)
                    .padding(.vertical, pad)
            }
        }
        .padding(.trailing, 16)
        .background(selected ? theme.accent.opacity(0.08) : .clear)
        // One sentence for VoiceOver, with the swipe actions as actions.
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
        .accessibilityAddTraits(selected ? [.isSelected, .isButton] : [.isButton])
        .accessibilityAction(named: entry.read ? "Mark as unread" : "Mark as read") { Task { await app.toggleRead(entry.id) } }
        .accessibilityAction(named: entry.starred ? "Unstar" : "Star") { Task { await app.toggleStar(entry.id) } }
    }
}

/// The selected row's spine, drawn as a string: at `t` 0 to 1 through a
/// pluck it bows into a sine wave pinned at both ends, whose crests travel
/// `way` (1 down, -1 up), swelling fast and settling straight at 1. The
/// web's spinePath in Wire.svelte.
struct WaveSpine: Shape {
    var t: Double
    var way: Int

    func path(in rect: CGRect) -> Path {
        let amp = t < 1 ? 2.6 * sin(.pi * t) * (1 - t * t * 0.5) : 0
        let phase = Double(-way) * t * 2.5 * .pi
        var p = Path()
        let steps = 40
        for i in 0...steps {
            let y = Double(i) / Double(steps)
            let pin = sin(.pi * y) // 0 at both ends
            // The wavelength is fixed in points, so tall rows carry more crests.
            let x = rect.midX + amp * pin * sin(2 * .pi * y * rect.height / 60 + phase)
            let point = CGPoint(x: x, y: rect.minY + y * rect.height)
            if i == 0 { p.move(to: point) } else { p.addLine(to: point) }
        }
        return p
    }
}
