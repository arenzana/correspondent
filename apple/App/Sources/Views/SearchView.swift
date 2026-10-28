import SwiftUI
import CorrespondentKit

/// Search or jump to, as the web's command palette (Palette.svelte): every
/// list, folder, saved search and feed by name, the app's actions, and
/// articles by their text as you type. Lists and actions are ranked by the
/// web's subsequence score; ↑ and ↓ move, Return opens.
struct SearchView: View {
    @Environment(AppModel.self) private var app
    @Environment(Player.self) private var player
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss
    @State private var text = ""
    @State private var found: [Entry] = []
    @State private var active = 0
    @FocusState private var focused: Bool
    var onOpen: (EntryID) -> Void = { _ in }
    var onList: () -> Void = {}
    /// Adding a feed, where the sidebar that usually shows it isn't built
    /// (the iPhone's Search tab); else the sidebar is asked.
    var onAddFeed: (() -> Void)? = nil
    /// In the iPhone's Search tab rather than a sheet: nothing to dismiss,
    /// and no sidebar or window-wide actions.
    var inTab = false

    struct Item: Identifiable {
        var id: String
        var label: String
        var hint: String? = nil
        var symbol: String? = nil
        var feed: Feed? = nil
        var group: String
        var run: () -> Void
    }

    var body: some View {
        let items = results
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "magnifyingglass").foregroundStyle(theme.slate)
                TextField("Search articles, or jump to anything", text: $text)
                    .font(Typeface.ui.font(17)).foregroundStyle(theme.ink).textFieldStyle(.plain)
                    .focused($focused).autocorrectionDisabled()
                    .onSubmit { if items.indices.contains(active) { items[active].run() } }
                    .onKeyPress(.downArrow) { active = min(active + 1, max(items.count - 1, 0)); return .handled }
                    .onKeyPress(.upArrow) { active = max(active - 1, 0); return .handled }
                if !inTab {
                    Button("Done") { dismiss() }.font(Typeface.ui.font(15, weight: 560)).tint(theme.accent).keyboardShortcut(.cancelAction)
                }
            }
            .padding(.horizontal, 16).frame(height: 56)
            .overlay(alignment: .bottom) { Rectangle().fill(theme.rule).frame(height: 1) }

            ScrollViewReader { proxy in
                List {
                    ForEach(groups(items), id: \.0) { group, rows in
                        Section(group) {
                            ForEach(rows, id: \.1.id) { index, item in
                                row(item, on: index == active)
                                    .id(item.id)
                                    .listRowBackground(index == active ? theme.accent.opacity(0.1) : Color.clear)
                            }
                        }
                    }
                }
                .listStyle(.plain)
                .scrollContentBackground(.hidden)
                .onChange(of: active) { _, i in if items.indices.contains(i) { proxy.scrollTo(items[i].id) } }
            }
        }
        .background(theme.sheet)
        .onAppear { focused = true }
        .onChange(of: text) { active = 0 }
        .task(id: text) {
            let term = text.trimmingCharacters(in: .whitespaces)
            guard term.count >= 2, let client = app.client else { found = []; return }
            try? await Task.sleep(for: .milliseconds(180))
            if Task.isCancelled { return }
            found = (try? await client.entries(.init(search: term, limit: 8)).entries) ?? []
        }
    }

    private func row(_ item: Item, on: Bool) -> some View {
        Button(action: item.run) {
            HStack(spacing: 10) {
                if let feed = item.feed {
                    FeedIcon(feed: feed, size: 16)
                } else if let symbol = item.symbol {
                    Image(systemName: symbol).font(.system(size: 14)).foregroundStyle(theme.slate).frame(width: 18)
                }
                Text(item.label).font(item.group == "Articles" ? Typeface.read.font(16, weight: 560) : Typeface.ui.font(15))
                    .foregroundStyle(item.id == "search" ? theme.accent : theme.ink).lineLimit(2)
                Spacer(minLength: 8)
                if let hint = item.hint {
                    Text(hint).font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate).lineLimit(1)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// Items in their groups, in order, with each one's place in the whole list.
    private func groups(_ items: [Item]) -> [(String, [(Int, Item)])] {
        var out: [(String, [(Int, Item)])] = []
        for (i, item) in items.enumerated() {
            if out.last?.0 == item.group { out[out.count - 1].1.append((i, item)) } else { out.append((item.group, [(i, item)])) }
        }
        return out
    }

    // MARK: What's offered

    /// Closes the palette, then does it (a list also moves on, on iPhone).
    private func then(_ list: Bool = false, _ work: @escaping () -> Void) -> () -> Void {
        { work(); if list { onList() }; if !inTab { dismiss() } }
    }

    private func go(_ s: Source) -> () -> Void { then(true) { Task { await app.go(s) } } }

    private func post(_ name: Notification.Name) -> () -> Void { then { NotificationCenter.default.post(name: name, object: nil) } }

    private var items: [Item] {
        let unread = { (n: Int) in n > 0 ? "\(n) unread" : nil }
        var out: [Item] = [
            Item(id: "v-unread", label: "Unread", hint: "g u", symbol: "tray", group: "Go to", run: go(.unread)),
            Item(id: "v-today", label: "Today", hint: "g t", symbol: "sun.max", group: "Go to", run: go(.today)),
            Item(id: "v-starred", label: "Starred", hint: "g s", symbol: "star", group: "Go to", run: go(.starred)),
            Item(id: "v-all", label: "All articles", hint: "g a", symbol: "square.3.layers.3d", group: "Go to", run: go(.all)),
        ]
        out += app.tree.searches.map { s in Item(id: "s\(s.id)", label: s.name, hint: unread(s.unread), symbol: "magnifyingglass", group: "Searches", run: go(.search(s.id))) }
        out += app.tree.folders.map { f in Item(id: "f\(f.id)", label: f.name, hint: unread(f.unread), symbol: "folder", group: "Folders", run: go(.folder(f.id))) }
        out += app.tree.feeds.map { f in Item(id: "d\(f.id)", label: f.title.isEmpty ? f.url : f.title, hint: unread(f.unread), feed: f, group: "Feeds", run: go(.feed(f.id))) }

        let scope = app.title
        out.append(Item(id: "a-mark", label: "Mark all as read in \(scope)", hint: "⇧A", symbol: "checkmark.circle", group: "Actions", run: then { Task { await app.markAllRead() } }))
        if let e = app.article {
            out.append(Item(id: "a-summary", label: "Summary of this article", hint: "⇧S", symbol: "text.alignleft", group: "Actions", run: then { Task { await app.summarize() } }))
            if !e.fullText {
                out.append(Item(id: "a-full-one", label: "Load the full article", symbol: "doc.text", group: "Actions", run: then { Task { await app.loadFullArticle() } }))
            }
        }
        out.append(Item(id: "a-listen", label: "Listen to unread with audio in \(scope)", hint: "⇧L", symbol: "headphones", group: "Actions",
                        run: then { Task { await player.playList(app.entries, from: app.selectedID) } }))
        out.append(Item(id: "a-add", label: "Add feed…", hint: "a", symbol: "plus", group: "Actions", run: onAddFeed.map { add in then { add() } } ?? post(.addFeed)))
        out.append(Item(id: "a-manage", label: "Manage subscriptions", hint: "g m", symbol: "list.bullet.rectangle", group: "Actions", run: post(.manage)))
        if case .feed(let id) = app.source, let feed = app.feedsByID[id] {
            out.append(Item(id: "a-full", label: feed.fullText ? "Stop fetching full articles for \(feed.title)" : "Fetch full articles for \(feed.title)",
                            symbol: "doc.text", group: "Actions", run: then { Task { await app.updateFeed(id, .init(fullText: !feed.fullText)) } }))
            out.append(Item(id: "a-notify", label: feed.notify ? "Stop notifications for \(feed.title)" : "Notify me about \(feed.title)",
                            symbol: feed.notify ? "bell.slash" : "bell", group: "Actions", run: then { Task { await app.updateFeed(id, .init(notify: !feed.notify)) } }))
        }
        if !inTab {
            out.append(Item(id: "a-sidebar", label: "Show or hide the sidebar", hint: "[", symbol: "sidebar.left", group: "Actions", run: post(.sidebar)))
            out.append(Item(id: "a-focus", label: "Focused reading", hint: "f", symbol: "arrow.up.left.and.arrow.down.right", group: "Actions", run: post(.focus)))
            out.append(Item(id: "a-keys", label: "Keyboard shortcuts", hint: "?", symbol: "keyboard", group: "Actions", run: post(.shortcuts)))
            out.append(Item(id: "a-settings", label: "Settings", hint: ",", symbol: "slider.horizontal.3", group: "Actions", run: post(.openSettings)))
        }
        for (id, label) in [("day", "Switch to Day"), ("night", "Switch to Night desk"), ("evening", "Switch to Evening"),
                            ("auto", "Match system theme"), ("sun", "Follow sunrise and sunset")] {
            out.append(Item(id: "a-\(id)", label: label, symbol: "circle.lefthalf.filled", group: "Actions", run: then { app.setTheme(id) }))
        }
        return out
    }

    /// The web's ranking: a prefix beats a match inside, which beats the
    /// letters in order with gaps; no match is left out.
    static func score(_ label: String, _ query: String) -> Double {
        let l = label.lowercased()
        let s = query.lowercased().trimmingCharacters(in: .whitespaces)
        if s.isEmpty { return 1 }
        if l.hasPrefix(s) { return 100 - Double(l.count) / 100 }
        if let r = l.range(of: s) { return 60 - Double(l.distance(from: l.startIndex, to: r.lowerBound)) }
        var want = s.startIndex, gaps = 0.0
        for ch in l {
            if ch == s[want] { want = s.index(after: want) } else if want > s.startIndex { gaps += 1 }
            if want == s.endIndex { return 20 - gaps / 10 }
        }
        return -1
    }

    private var results: [Item] {
        let term = text.trimmingCharacters(in: .whitespaces)
        var ranked = items.map { ($0, Self.score($0.label, term)) }.filter { $0.1 > 0 }
        if !term.isEmpty { ranked.sort { $0.1 > $1.1 } }
        var list = ranked.prefix(term.isEmpty ? 40 : (found.isEmpty ? 12 : 5)).map(\.0)
        if !term.isEmpty {
            list += found.map { e in
                Item(id: "e\(e.id)", label: e.title.isEmpty ? "Untitled" : e.title, hint: app.feedsByID[e.feedId]?.title, feed: app.feedsByID[e.feedId], group: "Articles",
                     run: { Task { await app.search(term); await app.select(e.id) }; onOpen(e.id); if !inTab { dismiss() } })
            }
            list.append(Item(id: "search", label: "All articles matching “\(term)”", symbol: "magnifyingglass", group: "Articles",
                             run: { Task { await app.search(term) }; onList(); if !inTab { dismiss() } }))
        }
        return list
    }
}
