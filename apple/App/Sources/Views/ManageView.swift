import SwiftUI
import CorrespondentKit

/// Managing subscriptions, as the web's page (Manage.svelte): every feed
/// with its health, grouped by folder, to find, rename, move, fix, pause,
/// notify and delete, one at a time or several together.
struct ManageView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss

    enum Filter: String, CaseIterable { case all = "All", failing = "Failing", paused = "Paused", quiet = "Quiet" }
    /// A feed is quiet when nothing has arrived for this long.
    static let quietDays = 90

    @State private var query = ""
    @State private var filter = Filter.all
    @State private var selection = Set<Int64>()
    @State private var renaming: Feed?
    @State private var addressFor: Feed?
    @State private var text = ""
    @State private var addressError = ""
    @State private var confirmDelete: [Int64]?
    @State private var naming: NewFolder?
    enum NewFolder: Identifiable { case empty, moving([Int64]); var id: String { "\(self)" } }
    #if os(iOS)
    @State private var editMode = EditMode.inactive
    #endif

    var body: some View {
        VStack(spacing: 0) {
            header
            controls
            List(selection: $selection) {
                ForEach(groups, id: \.title) { group in
                    Section(group.title) {
                        ForEach(group.feeds) { f in
                            row(f).tag(f.id)
                                .contextMenu { menu(for: f) }
                                .swipeActions(edge: .trailing) {
                                    Button(role: .destructive) { confirmDelete = [f.id] } label: { Label("Delete", systemImage: "trash") }
                                    Button { Task { await app.setPaused([f.id], !f.paused) } } label: {
                                        Label(f.paused ? "Resume" : "Pause", systemImage: f.paused ? "play" : "pause")
                                    }
                                }
                        }
                    }
                }
                if groups.isEmpty {
                    Text(query.isEmpty ? "No feeds here." : "No feeds match “\(query)”.")
                        .font(Typeface.ui.font(14.5)).foregroundStyle(theme.slate)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
            #if os(iOS)
            .environment(\.editMode, $editMode)
            #endif
            if !selected.isEmpty { bulkBar }
        }
        .background(theme.sheet)
        .tint(theme.accent)
        .alert("Rename feed", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField("Name", text: $text)
            Button("Rename") { if let f = renaming { Task { await app.updateFeed(f.id, .init(title: text)) } } }
            Button("Cancel", role: .cancel) {}
        }
        .alert(naming.map { if case .moving = $0 { "Move to a new folder" } else { "New folder" } } ?? "",
               isPresented: Binding(get: { naming != nil }, set: { if !$0 { naming = nil } })) {
            TextField("Folder name", text: $text)
            Button(naming.map { if case .moving = $0 { "Move" } else { "Create" } } ?? "") {
                let name = text
                if case .moving(let ids) = naming { Task { await app.moveFeeds(ids, to: name) } } else { Task { await app.createFolder(name) } }
            }
            Button("Cancel", role: .cancel) {}
        }
        .sheet(item: $addressFor) { f in addressSheet(f) }
        .confirmationDialog(deleteTitle, isPresented: Binding(get: { confirmDelete != nil }, set: { if !$0 { confirmDelete = nil } }),
                            titleVisibility: .visible) {
            Button("Delete", role: .destructive) {
                if let ids = confirmDelete { selection.subtract(ids); Task { await app.deleteFeeds(ids) } }
            }
        } message: {
            Text(deleteMessage)
        }
        #if os(macOS)
        .frame(minWidth: 640, minHeight: 560)
        #else
        .presentationSizing(.page)
        #endif
    }

    // MARK: Parts

    private var header: some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Subscriptions").font(Typeface.read.font(23, weight: 560, opsz: 36)).foregroundStyle(theme.ink)
                Text("\(app.tree.feeds.count) feeds in \(app.tree.folders.count) folders").font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate)
            }
            Spacer()
            Button { text = ""; naming = .empty } label: { Image(systemName: "folder.badge.plus") }
                .iconButton().accessibilityLabel("New folder")
            #if os(iOS)
            Button(editMode.isEditing ? "Done selecting" : "Select") {
                withAnimation { editMode = editMode.isEditing ? .inactive : .active }
                if !editMode.isEditing { selection = [] }
            }
            .font(Typeface.ui.font(15, weight: 560))
            #endif
            Button("Done") { dismiss() }.font(Typeface.ui.font(15, weight: 560)).keyboardShortcut(.cancelAction)
        }
        .padding(.horizontal, 18).padding(.vertical, 12)
        .overlay(alignment: .bottom) { Rectangle().fill(theme.rule).frame(height: 1) }
    }

    private var controls: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass").font(.system(size: 13)).foregroundStyle(theme.slate)
                TextField("Find a feed by name or address", text: $query)
                    .textFieldStyle(.plain).font(Typeface.ui.font(14)).foregroundStyle(theme.ink).autocorrectionDisabled()
            }
            .padding(.horizontal, 12).frame(height: 34)
            .background(RoundedRectangle(cornerRadius: 6).fill(theme.fog))
            Picker("Show", selection: $filter) {
                ForEach(Filter.allCases, id: \.self) { f in Text("\(f.rawValue) \(count(f))").tag(f) }
            }
            .pickerStyle(.segmented).labelsHidden()
        }
        .padding(.horizontal, 18).padding(.vertical, 10)
    }

    private func row(_ f: Feed) -> some View {
        let status = status(f)
        return HStack(alignment: .top, spacing: 10) {
            FeedIcon(feed: f, size: 18).padding(.top, 2)
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(f.title.isEmpty ? f.url : f.title).font(Typeface.ui.font(15, weight: 560)).foregroundStyle(theme.ink).lineLimit(1)
                    if f.notify { Image(systemName: "bell.fill").font(.system(size: 9)).foregroundStyle(theme.slate) }
                    if f.fullText { Image(systemName: "doc.text").font(.system(size: 10)).foregroundStyle(theme.slate) }
                }
                Text(URL(string: f.url)?.host() ?? f.url).font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate).lineLimit(1)
                Text(status.text).font(Typeface.ui.font(12.5)).foregroundStyle(status.bad ? theme.danger : theme.slate).lineLimit(2)
                if f.page {
                    Button("Rebuild its parser") { Task { await app.rebuildFeed(f) } }
                        .buttonStyle(.plain).font(Typeface.ui.font(12.5, weight: 560)).foregroundStyle(theme.accent)
                }
            }
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 4) {
                if !f.cadence.isEmpty { Pulse(days: f.cadence) }
                Text(perWeek(f)).font(Typeface.ui.font(11.5)).foregroundStyle(theme.slate)
            }
        }
        .padding(.vertical, 4)
        .opacity(f.paused ? 0.6 : 1)
    }

    @ViewBuilder private func menu(for f: Feed) -> some View {
        Button { text = f.title; renaming = f } label: { Label("Rename…", systemImage: "pencil") }
        moveMenu([f.id])
        Button { text = f.url; addressError = ""; addressFor = f } label: { Label("Change address…", systemImage: "link") }
        Button { Task { await app.updateNow(f.id) } } label: { Label("Update now", systemImage: "arrow.clockwise") }
        Button { Task { await app.setPaused([f.id], !f.paused) } } label: { Label(f.paused ? "Resume updates" : "Pause updates", systemImage: f.paused ? "play" : "pause") }
        Button { Task { await app.updateFeed(f.id, .init(notify: !f.notify)) } } label: {
            Label(f.notify ? "Stop notifications" : "Notify me about new articles", systemImage: f.notify ? "bell.slash" : "bell")
        }
        Button { Task { await app.updateFeed(f.id, .init(fullText: !f.fullText)) } } label: {
            Label(f.fullText ? "Stop fetching full articles" : "Fetch full articles", systemImage: "doc.text")
        }
        if f.page { Button { Task { await app.rebuildFeed(f) } } label: { Label("Rebuild its parser", systemImage: "hammer") } }
        Divider()
        Button(role: .destructive) { confirmDelete = [f.id] } label: { Label("Delete…", systemImage: "trash") }
    }

    private func moveMenu(_ ids: [Int64]) -> some View {
        Menu {
            ForEach(app.tree.folders) { folder in
                Button(folder.name) { Task { await app.moveFeeds(ids, to: folder.name) } }
            }
            Button("Out of folders") { Task { await app.moveFeeds(ids, to: nil) } }
            Divider()
            Button("New folder…") { text = ""; naming = .moving(ids) }
        } label: { Label("Move to", systemImage: "folder") }
    }

    private var bulkBar: some View {
        HStack(spacing: 10) {
            Text("\(selected.count) selected").font(Typeface.ui.font(14, weight: 560)).foregroundStyle(theme.ink)
            Spacer()
            moveMenu(selected).fixedSize()
            Button("Pause") { Task { await app.setPaused(selected, true) } }
            Button("Resume") { Task { await app.setPaused(selected, false) } }
            Button("Delete…", role: .destructive) { confirmDelete = selected }
        }
        .font(Typeface.ui.font(14))
        .buttonStyle(QuietButton())
        .padding(.horizontal, 18).padding(.vertical, 10)
        .background(theme.fog)
        .overlay(alignment: .top) { Rectangle().fill(theme.rule).frame(height: 1) }
    }

    private func addressSheet(_ f: Feed) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Change the address of \(f.title)").font(Typeface.read.font(19, weight: 560)).foregroundStyle(theme.ink)
            Text("If the site moved its feed, paste the new address. The feed keeps its articles.")
                .font(Typeface.ui.font(13.5)).foregroundStyle(theme.slate).fixedSize(horizontal: false, vertical: true)
            TextField("https://", text: $text)
                .textFieldStyle(.roundedBorder).autocorrectionDisabled()
                #if os(iOS)
                .textInputAutocapitalization(.never).keyboardType(.URL)
                #endif
            if !addressError.isEmpty { Text(addressError).font(Typeface.ui.font(13)).foregroundStyle(theme.danger) }
            HStack {
                Spacer()
                Button("Cancel") { addressFor = nil }.keyboardShortcut(.cancelAction)
                Button("Save") {
                    Task {
                        if let error = await app.setFeedURL(f.id, text) { addressError = error } else { addressFor = nil }
                    }
                }
                .keyboardShortcut(.defaultAction).buttonStyle(.borderedProminent)
            }
        }
        .padding(22)
        .background(theme.sheet)
        .presentationDetents([.height(300)])
        #if os(macOS)
        .frame(width: 460)
        #endif
    }

    // MARK: What's shown

    private var selected: [Int64] { app.tree.feeds.map(\.id).filter(selection.contains) }

    private func quiet(_ f: Feed) -> Bool {
        f.latestAt == 0 || Date().timeIntervalSince1970 - TimeInterval(f.latestAt) > TimeInterval(Self.quietDays * 86_400)
    }

    private func matches(_ f: Feed, _ filter: Filter) -> Bool {
        switch filter {
        case .all: true
        case .failing: !f.paused && f.errorCount > 0
        case .paused: f.paused
        case .quiet: quiet(f)
        }
    }

    private func count(_ filter: Filter) -> Int { app.tree.feeds.filter { matches($0, filter) }.count }

    private var visible: [Feed] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return app.tree.feeds.filter { f in
            matches(f, filter) && (q.isEmpty || "\(f.title) \(f.url) \(f.siteUrl)".lowercased().contains(q))
        }
        .sorted { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending }
    }

    /// Folders in the sidebar's order, then the feeds outside any; while
    /// finding or filtering, only groups with something to show.
    private var groups: [(title: String, feeds: [Feed])] {
        let feeds = visible
        var out = app.tree.folders.map { folder in (title: folder.name, feeds: feeds.filter { $0.folderId == folder.id }) }
        out.append((title: "Not in a folder", feeds: feeds.filter { $0.folderId == nil }))
        let narrowed = filter != .all || !query.trimmingCharacters(in: .whitespaces).isEmpty
        return out.filter { !$0.feeds.isEmpty || (!narrowed && $0.title != "Not in a folder") }
    }

    private func status(_ f: Feed) -> (text: String, bad: Bool) {
        if f.paused { return ("Paused", false) }
        if f.errorCount > 0 { return ("Failing: \(f.lastError)", true) }
        if f.lastFetchAt == 0 { return ("Not checked yet", false) }
        let when = Date(timeIntervalSince1970: TimeInterval(f.lastFetchAt)).formatted(.relative(presentation: .named))
        return ("Checked \(when)", false)
    }

    private func perWeek(_ f: Feed) -> String {
        let n = Double(f.cadence.reduce(0, +)) / 2
        return n == 0 ? "none in 2 weeks" : n < 1 ? "about 1 a fortnight" : "\(Int(n.rounded())) a week"
    }

    private var deleteTitle: String {
        guard let ids = confirmDelete else { return "" }
        return ids.count == 1 ? "Delete \(app.feedsByID[ids[0]]?.title ?? "this feed")?" : "Delete \(ids.count) feeds?"
    }

    private var deleteMessage: String {
        let feeds = (confirmDelete ?? []).compactMap { app.feedsByID[$0] }
        let articles = feeds.reduce(0) { $0 + $1.total }, starred = feeds.reduce(0) { $0 + $1.starred }
        return "Their \(articles) \(articles == 1 ? "article goes" : "articles go") with them" + (starred > 0 ? ", \(starred) starred." : ".")
    }
}
