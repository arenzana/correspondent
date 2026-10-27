import SwiftUI
import CorrespondentKit

/// The feeds pane, as the web's sidebar: the wordmark, search, the four
/// views, saved searches, folders with their feeds, and the toolbar.
struct SidebarView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var onSearch: () -> Void
    var onSettings: () -> Void
    /// On iPhone, choosing a list moves on to it.
    var onChoose: () -> Void = {}
    /// On iPad and Mac, hides the sidebar (the list then shows its controls).
    var onCollapse: (() -> Void)? = nil

    @State private var adding = false
    /// Renaming a feed, folder or saved search: what, and the new name.
    @State private var renaming: Renaming?
    @State private var newName = ""
    @State private var leaving: Feed?
    enum Renaming { case feed(Feed), folder(Folder), search(SavedSearch) }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 10) {
                Mark(size: 28)
                Text("Correspondent").font(Typeface.read.font(21, weight: 560, opsz: 48)).foregroundStyle(theme.ink)
                    .lineLimit(1).minimumScaleFactor(0.75)
                Spacer(minLength: 0)
                if let onCollapse {
                    Button(action: onCollapse) { Image(systemName: "sidebar.left") }.iconButton()
                        .accessibilityLabel("Hide the sidebar")
                }
            }
            .padding(.horizontal, 18).padding(.top, 18).padding(.bottom, 14)

            Button(action: onSearch) {
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").font(.system(size: 13))
                    Text("Search or jump to…").font(Typeface.ui.font(14))
                    Spacer()
                }
                .foregroundStyle(theme.slate)
                .padding(.horizontal, 12).frame(height: 36)
                .background(RoundedRectangle(cornerRadius: 6).fill(theme.sheet))
                .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.rule))
            }
            .buttonStyle(.plain)
            .padding(.horizontal, 12).padding(.bottom, 10)

            ScrollView {
                VStack(alignment: .leading, spacing: 1) {
                    row(.unread, "Unread", "tray", app.tree.counts.unread)
                    row(.today, "Today", "sun.max", app.tree.counts.today)
                    row(.starred, "Starred", "star", app.tree.counts.starred)
                    row(.all, "All articles", "square.3.layers.3d", 0)
                    Divider().overlay(theme.rule.opacity(0.6)).padding(.vertical, 8).padding(.horizontal, 12)

                    if !app.tree.searches.isEmpty {
                        group("Searches")
                        ForEach(app.tree.searches) { s in
                            row(.search(s.id), s.name, "magnifyingglass", s.unread)
                                .contextMenu {
                                    Button { startRename(.search(s)) } label: { Label("Rename…", systemImage: "pencil") }
                                    Button(role: .destructive) { Task { await app.deleteSearch(s) } } label: { Label("Remove search", systemImage: "trash") }
                                }
                        }
                        Divider().overlay(theme.rule.opacity(0.6)).padding(.vertical, 8).padding(.horizontal, 12)
                    }

                    ForEach(app.tree.folders.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }) { folder in
                        folderRow(folder)
                            .contextMenu {
                                Button { startRename(.folder(folder)) } label: { Label("Rename…", systemImage: "pencil") }
                                Button(role: .destructive) { Task { await app.deleteFolder(folder) } } label: {
                                    Label("Delete folder (keeps its feeds)", systemImage: "trash")
                                }
                            }
                        if !app.collapsed.contains(folder.id) {
                            ForEach(feeds(in: folder.id)) { f in
                                feedRow(f).contextMenu { feedMenu(f) }
                                    .transition(.opacity.combined(with: .move(edge: .top)))
                            }
                        }
                    }
                    ForEach(feeds(in: nil)) { f in feedRow(f).contextMenu { feedMenu(f) } }
                }
                .padding(.horizontal, 8).padding(.bottom, 12)
            }

            HStack(spacing: 2) {
                Button { adding = true } label: { Image(systemName: "plus") }.iconButton()
                    .accessibilityLabel("Add a feed")
                Spacer()
                Button(action: onSettings) { Image(systemName: "slider.horizontal.3") }.iconButton()
                    .accessibilityLabel("Settings")
            }
            .padding(.horizontal, 10).padding(.vertical, 8)
            .overlay(alignment: .top) { Rectangle().fill(theme.rule.opacity(0.6)).frame(height: 1) }
        }
        .background(theme.fog)
        .onReceive(NotificationCenter.default.publisher(for: .addFeed)) { _ in adding = true }
        .sheet(isPresented: $adding) {
            AddFeedView().environment(\.theme, theme).environment(app)
                #if os(macOS)
                .frame(width: 520, height: 560)
                #endif
        }
        .alert(renameTitle, isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField("Name", text: $newName)
            Button("Rename") { rename() }
            Button("Cancel", role: .cancel) { renaming = nil }
        }
        .confirmationDialog("Unsubscribe from \(leaving?.title ?? "")?", isPresented: Binding(get: { leaving != nil }, set: { if !$0 { leaving = nil } }), titleVisibility: .visible) {
            Button("Unsubscribe", role: .destructive) {
                if let f = leaving { Task { await app.unsubscribe(f) } }
                leaving = nil
            }
        } message: {
            Text("Its articles are deleted, except starred ones.")
        }
    }

    private var renameTitle: String {
        switch renaming {
        case .feed: "Rename the feed"
        case .folder: "Rename the folder"
        case .search: "Rename the search"
        case nil: ""
        }
    }

    private func startRename(_ r: Renaming) {
        switch r {
        case .feed(let f): newName = f.title
        case .folder(let f): newName = f.name
        case .search(let s): newName = s.name
        }
        renaming = r
    }

    private func rename() {
        let name = newName.trimmingCharacters(in: .whitespaces)
        guard !name.isEmpty, let r = renaming else { renaming = nil; return }
        renaming = nil
        Task {
            switch r {
            case .feed(let f): await app.updateFeed(f.id, .init(title: name))
            case .folder(let f): await app.renameFolder(f.id, name)
            case .search(let s): await app.renameSearch(s.id, name)
            }
        }
    }

    @ViewBuilder
    private func feedMenu(_ feed: Feed) -> some View {
        Button { startRename(.feed(feed)) } label: { Label("Rename…", systemImage: "pencil") }
        Menu {
            ForEach(app.tree.folders.filter { $0.id != feed.folderId }) { f in
                Button(f.name) { Task { await app.updateFeed(feed.id, .init(folder: f.name), done: "Moved to \(f.name).") } }
            }
            if feed.folderId != nil {
                Button("No folder") { Task { await app.updateFeed(feed.id, .init(noFolder: true)) } }
            }
        } label: { Label("Move to", systemImage: "folder") }
        Button {
            Task { await app.updateFeed(feed.id, .init(fullText: !feed.fullText), done: feed.fullText ? "Showing the feed's own text." : "Fetching full articles from now on.") }
        } label: { Label(feed.fullText ? "Stop fetching full articles" : "Fetch full articles", systemImage: "doc.text") }
        Button {
            Task { await app.updateFeed(feed.id, .init(paused: !feed.paused), done: feed.paused ? "Updates resumed." : "Updates paused.") }
        } label: { Label(feed.paused ? "Resume updates" : "Pause updates", systemImage: feed.paused ? "play" : "pause") }
        if app.notificationsAvailable {
            Button {
                Task { await app.updateFeed(feed.id, .init(notify: !feed.notify), done: feed.notify ? "Notifications off for \(feed.title)." : "You'll be notified about new articles in \(feed.title).") }
            } label: { Label(feed.notify ? "Stop notifications" : "Notify me about new articles", systemImage: feed.notify ? "bell.slash" : "bell") }
        }
        Button { Task { await app.refreshFeed(feed.id) } } label: { Label("Refresh now", systemImage: "arrow.clockwise") }
        Divider()
        Button(role: .destructive) { leaving = feed } label: { Label("Unsubscribe…", systemImage: "trash") }
    }

    private func feeds(in folder: Int64?) -> [Feed] {
        app.tree.feeds.filter { $0.folderId == folder }.sorted { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending }
    }

    private func group(_ label: String) -> some View {
        Text(label).font(Typeface.ui.font(12.5, weight: 600)).foregroundStyle(theme.slate)
            .padding(.horizontal, 12).padding(.top, 4).padding(.bottom, 2)
    }

    private func row(_ s: Source, _ label: String, _ icon: String, _ count: Int) -> some View {
        Button {
            Task { await app.go(s); onChoose() }
        } label: {
            HStack(spacing: 10) {
                Image(systemName: icon).font(.system(size: 14)).frame(width: 18).foregroundStyle(app.source == s ? theme.accent : theme.slate)
                Text(label).font(Typeface.ui.font(15, weight: app.source == s ? 560 : 450)).foregroundStyle(theme.ink).lineLimit(1)
                Spacer()
                Count(n: count)
            }
            .padding(.horizontal, 12).frame(height: 34)
            .background(RoundedRectangle(cornerRadius: 6).fill(app.source == s ? theme.accent.opacity(0.14) : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(count > 0 ? "\(label), \(count) unread" : label)
        .accessibilityAddTraits(app.source == s ? .isSelected : [])
    }

    private func folderRow(_ folder: Folder) -> some View {
        let open = !app.collapsed.contains(folder.id)
        let s = Source.folder(folder.id)
        return HStack(spacing: 4) {
            Button {
                withAnimation(reduceMotion ? nil : .easeOut(duration: 0.2)) {
                    if open { app.collapsed.insert(folder.id) } else { app.collapsed.remove(folder.id) }
                }
            } label: {
                Image(systemName: "chevron.right").font(.system(size: 10, weight: .semibold))
                    .rotationEffect(.degrees(open ? 90 : 0)).foregroundStyle(theme.slate).frame(width: 18, height: 30)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(open ? "Collapse" : "Expand") \(folder.name)")
            Button {
                Task { await app.go(s); onChoose() }
            } label: {
                HStack {
                    Text(folder.name).font(Typeface.ui.font(15, weight: 520)).foregroundStyle(theme.slate).lineLimit(1)
                    Spacer()
                    Count(n: folder.unread)
                }
                .padding(.horizontal, 8).frame(height: 32)
                .background(RoundedRectangle(cornerRadius: 6).fill(app.source == s ? theme.accent.opacity(0.14) : .clear))
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        }
        .padding(.top, 6)
    }

    private func feedRow(_ feed: Feed) -> some View {
        let s = Source.feed(feed.id)
        return Button {
            Task { await app.go(s); onChoose() }
        } label: {
            HStack(spacing: 10) {
                FeedIcon(feed: feed, size: 16)
                Text(feed.title.isEmpty ? feed.url : feed.title).font(Typeface.ui.font(15))
                    .foregroundStyle(feed.paused || feed.errorCount > 0 ? theme.slate : theme.ink).lineLimit(1)
                if feed.notify {
                    Image(systemName: "bell.fill").font(.system(size: 9)).foregroundStyle(theme.slate.opacity(0.8))
                        .accessibilityLabel("Notifications on")
                }
                Spacer()
                if feed.errorCount > 0 { Image(systemName: "exclamationmark.triangle").font(.system(size: 11)).foregroundStyle(theme.slate) }
                Count(n: feed.unread)
            }
            .padding(.leading, 30).padding(.trailing, 12).frame(height: 32)
            .background(RoundedRectangle(cornerRadius: 6).fill(app.source == s ? theme.accent.opacity(0.14) : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}
