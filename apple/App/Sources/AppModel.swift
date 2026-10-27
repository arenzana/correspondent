import SwiftUI
import CorrespondentKit

/// The app's state, as the web app's (ui/src/lib/state.svelte.ts): the
/// account, the tree, the list being read and the open article. Changes
/// show at once and roll back if the server refuses; without a network the
/// app reads from its offline copy.
@MainActor @Observable
final class AppModel {
    // Session
    var account: Account?
    var client: Client?
    var booting = true
    var prefs = Prefs()
    var online = true

    // Sidebar
    var tree = Tree()
    var collapsed: Set<Int64> = Set(UserDefaults.standard.array(forKey: "collapsedFolders") as? [Int64] ?? []) {
        didSet { UserDefaults.standard.set(Array(collapsed), forKey: "collapsedFolders") }
    }

    // The list
    var source: Source = .unread
    var query = ""
    var entries: [Entry] = []
    var cursor = ""
    var loading = false
    var loadError = ""
    /// The newest article ID when the list loaded: the mark-all-read boundary.
    var boundary = "0"
    /// Articles that arrived since the list loaded, offered rather than inserted.
    var pending = 0

    // The article
    var selectedID: EntryID?
    var article: Entry?
    var articleError = ""
    var summaryOpen = false
    var summarizing = false

    var toast: String?

    private var offline: OfflineStore?
    private var liveTask: Task<Void, Never>?
    private var dwell: Task<Void, Never>?
    private var loadSeq = 0

    // MARK: Derived

    var feedsByID: [Int64: Feed] { Dictionary(uniqueKeysWithValues: tree.feeds.map { ($0.id, $0) }) }

    var title: String {
        switch source {
        case .unread: "Unread"
        case .today: "Today"
        case .starred: "Starred"
        case .all: "All articles"
        case .feed(let id): feedsByID[id]?.title ?? "Feed"
        case .folder(let id): tree.folders.first { $0.id == id }?.name ?? "Folder"
        case .search(let id): tree.searches.first { $0.id == id }?.name ?? "Search"
        }
    }

    var unreadFilter: Bool {
        switch source {
        case .unread: true
        case .today, .starred, .all: false
        default: prefs.unreadOnly
        }
    }

    var subtitle: String {
        if loading && entries.isEmpty { return "Loading…" }
        let unread = entries.filter { !$0.read }.count
        let feeds = Set(entries.map(\.feedId)).count
        if unreadFilter || source == .unread || source == .today {
            return "\(unread) unread" + (feeds > 1 ? " across \(feeds) feeds" : "") + (cursor.isEmpty ? "" : " so far")
        }
        return "\(entries.count)\(cursor.isEmpty ? "" : "+") articles"
    }

    var folderFeeds: [Int64: Set<Int64>] {
        var m: [Int64: Set<Int64>] = [:]
        for f in tree.feeds { if let fo = f.folderId { m[fo, default: []].insert(f.id) } }
        return m
    }

    // MARK: Session

    func boot() async {
        defer { booting = false }
        #if DEBUG
        // Tests and screenshots start signed in: CORR_SERVER and CORR_TOKEN.
        let env = ProcessInfo.processInfo.environment
        if let s = env["CORR_SERVER"].flatMap(Client.serverURL), let t = env["CORR_TOKEN"] {
            start(account: Account(server: s, username: env["CORR_USER"] ?? "reader"), token: t)
            await refreshTree()
            await load()
            if env["CORR_OPEN_FIRST"] != nil, let first = entries.first { await select(first.id) }
            listen()
            return
        }
        #endif
        guard let (account, token) = Account.load() else { return }
        start(account: account, token: token)
        await refreshTree()
        await load()
        listen()
        Task { await syncOffline() }
    }

    func signIn(server typed: String, username: String, password: String) async throws {
        guard let server = Client.serverURL(typed) else {
            throw APIError(status: 0, message: "Type the address of your Correspondent server, like rss.example.org.")
        }
        let (token, user) = try await Client.signIn(server: server, username: username, password: password, device: Self.deviceName)
        let account = Account(server: server, username: user)
        try account.save(token: token)
        start(account: account, token: token)
        await refreshTree()
        await load()
        listen()
        Task { await syncOffline() }
    }

    func signOut() async {
        try? await client?.signOut()
        account?.forget()
        await offline?.clear()
        liveTask?.cancel()
        account = nil
        client = nil
        tree = Tree()
        entries = []
        article = nil
        selectedID = nil
    }

    private func start(account: Account, token: String) {
        self.account = account
        client = Client(server: account.server, token: token)
        offline = OfflineStore(account: account.key)
        Task {
            if let me = try? await client?.me(), let p = me.prefs {
                prefs = p
                #if os(macOS)
                AppIconChoice(rawValue: p.appIcon)?.apply()
                #endif
            }
        }
    }

    static var deviceName: String {
        #if os(iOS)
        "Correspondent on " + UIDevice.current.model
        #else
        "Correspondent for Mac"
        #endif
    }

    // MARK: Preferences

    /// Changes preferences at once, and saves them with the web app's (only
    /// these keys change). If the server refuses, they go back.
    func setPrefs(_ change: (inout Prefs) -> Void, _ keys: [String: any Sendable]) {
        let before = prefs
        change(&prefs)
        let reload = before.unreadOnly != prefs.unreadOnly
        Task {
            do {
                try await client?.savePrefs(keys)
                if keys["skipSponsored"] as? Bool == true { await refreshTree(); await load() }
            } catch {
                prefs = before
                show("Couldn't save the setting. \(error.localizedDescription)")
            }
        }
        if reload { Task { await load() } }
    }

    // MARK: Loading

    func refreshTree() async {
        guard let client else { return }
        do {
            tree = try await client.tree()
            online = true
            await offline?.save(tree: tree, entries: [])
        } catch {
            if isOffline(error), let saved = await offline?.tree() {
                tree = saved
                online = false
            } else {
                handle(error)
            }
        }
    }

    private func query(for source: Source) -> Client.Query {
        var q = Client.Query(search: query.isEmpty ? nil : query)
        switch source {
        case .unread: q.view = "unread"
        case .today: q.view = "today"
        case .starred: q.view = "starred"
        case .all: break
        case .feed(let id): q.feed = id
        case .folder(let id): q.folder = id
        case .search(let id):
            let words = tree.searches.first { $0.id == id }?.query ?? ""
            q.search = [words, query].filter { !$0.isEmpty }.joined(separator: " ")
        }
        if unreadFilter, q.view == nil { q.view = "unread" }
        return q
    }

    func load() async {
        guard let client else { return }
        loadSeq += 1
        let seq = loadSeq
        loading = true
        loadError = ""
        pending = 0
        defer { if seq == loadSeq { loading = false } }
        do {
            let page = try await client.entries(query(for: source))
            guard seq == loadSeq else { return }
            entries = page.entries
            cursor = page.cursor
            // The boundary: the newest article seen, so mark all read never reaches past it.
            let newest = ([tree.counts.newest] + page.entries.map(\.id)).max { Self.n($0) < Self.n($1) } ?? "0"
            boundary = newest
            online = true
            await offline?.save(entries: page.entries)
        } catch {
            guard seq == loadSeq else { return }
            if isOffline(error), let store = offline {
                online = false
                entries = await store.list(source, unreadOnly: unreadFilter, search: query.isEmpty ? nil : query, folders: folderFeeds)
                cursor = ""
            } else {
                loadError = error.localizedDescription
                handle(error)
            }
        }
    }

    func loadMore() async {
        guard let client, !cursor.isEmpty, !loading else { return }
        loading = true
        defer { loading = false }
        var q = query(for: source)
        q.cursor = cursor
        if let page = try? await client.entries(q) {
            let have = Set(entries.map(\.id))
            entries += page.entries.filter { !have.contains($0.id) }
            cursor = page.cursor
            await offline?.save(entries: page.entries)
        }
    }

    func go(_ s: Source) async {
        source = s
        selectedID = nil
        article = nil
        query = ""
        await load()
    }

    func search(_ text: String) async {
        query = text
        source = .all
        selectedID = nil
        article = nil
        await load()
    }

    // MARK: The article

    func select(_ id: EntryID) async {
        guard let client else { return }
        dwell?.cancel()
        selectedID = id
        summaryOpen = false
        articleError = ""
        if let row = entries.first(where: { $0.id == id }) { article = row }
        do {
            let e = try await client.entry(id)
            guard selectedID == id else { return }
            article = e
            patchRow(id) { $0.imageUrl = e.imageUrl; $0.excerpt = e.excerpt }
            await offline?.save(entries: [e])
        } catch {
            guard selectedID == id else { return }
            if isOffline(error), let saved = await offline?.entry(id), saved.content != nil {
                online = false
                article = saved
            } else {
                articleError = error.localizedDescription
                return
            }
        }
        // Read after a short, cancellable dwell, so skimming doesn't consume articles.
        if prefs.markOnOpen, let e = entries.first(where: { $0.id == id }) ?? article, !e.read {
            dwell = Task {
                try? await Task.sleep(for: .milliseconds(900))
                if !Task.isCancelled, selectedID == id { await setRead([id], true) }
            }
        }
    }

    /// Moves to the next or previous article in the list (j / k).
    func move(_ step: Int) async {
        guard !entries.isEmpty else { return }
        let i = entries.firstIndex { $0.id == selectedID }.map { $0 + step } ?? (step > 0 ? 0 : entries.count - 1)
        guard entries.indices.contains(i) else { return }
        await select(entries[i].id)
        if i >= entries.count - 5 { await loadMore() }
    }

    func closeArticle() {
        dwell?.cancel()
        selectedID = nil
        article = nil
    }

    func summarize() async {
        guard let client, let e = article, !summarizing else { return }
        if summaryOpen { summaryOpen = false; return }
        if e.summary != nil { summaryOpen = true; return }
        summarizing = true
        defer { summarizing = false }
        do {
            let s = try await client.summarize(e.id)
            if article?.id == e.id { article?.summary = s; summaryOpen = true }
        } catch {
            show("Couldn't summarize this article. \(error.localizedDescription)")
        }
    }

    func loadFullArticle() async {
        guard let client, let e = article else { return }
        do {
            let full = try await client.fullArticle(e.id)
            if article?.id == e.id { article = full }
            patchRow(e.id) { $0.fullText = true; $0.imageUrl = full.imageUrl }
        } catch {
            show(error.localizedDescription)
        }
    }

    // MARK: Changes

    func setRead(_ ids: [EntryID], _ read: Bool) async {
        let targets = ids.filter { id in entries.first { $0.id == id }?.read != read }
        guard !targets.isEmpty, let client else { return }
        patch(targets) { $0.read = read }
        await offline?.patch(targets, read: read)
        do {
            try await client.setState(read ? "read" : "unread", targets)
            await refreshTree()
        } catch {
            if isOffline(error) { return }
            patch(targets) { $0.read = !read }
            show("Couldn't update the article. \(error.localizedDescription)")
        }
    }

    func toggleStar(_ id: EntryID) async {
        guard let client, let e = entries.first(where: { $0.id == id }) ?? (article?.id == id ? article : nil) else { return }
        let starred = !e.starred
        patch([id]) { $0.starred = starred }
        await offline?.patch([id], starred: starred)
        do {
            try await client.setState(starred ? "star" : "unstar", [id])
            await refreshTree()
        } catch {
            patch([id]) { $0.starred = !starred }
            show("Couldn't update the star. \(error.localizedDescription)")
        }
    }

    func toggleRead(_ id: EntryID) async {
        guard let e = entries.first(where: { $0.id == id }) ?? article else { return }
        dwell?.cancel()
        await setRead([id], !e.read)
    }

    func markAllRead() async {
        guard let client, source != .starred else { return }
        let q = query(for: source)
        do {
            let ids = try await client.markAllRead(feed: q.feed, folder: q.folder, view: source == .today ? "today" : nil,
                                                   search: q.search, before: boundary)
            if ids.isEmpty { show("Nothing to mark as read."); return }
            patch(ids) { $0.read = true }
            await refreshTree()
            show("Marked \(ids.count) \(ids.count == 1 ? "article" : "articles") as read.")
        } catch {
            show("Couldn't mark as read. \(error.localizedDescription)")
        }
    }

    private func patch(_ ids: [EntryID], _ change: (inout Entry) -> Void) {
        let set = Set(ids)
        for i in entries.indices where set.contains(entries[i].id) { change(&entries[i]) }
        if let a = article, set.contains(a.id) { var a = a; change(&a); article = a }
    }

    private func patchRow(_ id: EntryID, _ change: (inout Entry) -> Void) {
        if let i = entries.firstIndex(where: { $0.id == id }) { change(&entries[i]) }
    }

    // MARK: Live and offline

    /// Follows the server's live events; reconnects after a drop.
    func listen() {
        liveTask?.cancel()
        liveTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self, let client = self.client else { return }
                do {
                    for try await ev in client.events() {
                        await self.apply(ev)
                    }
                } catch {}
                try? await Task.sleep(for: .seconds(5))
                await self.catchUp()
            }
        }
    }

    private func apply(_ ev: LiveEvent) async {
        switch ev.name {
        case "state":
            let read: Bool? = ev.action == "read" ? true : ev.action == "unread" ? false : nil
            let star: Bool? = ev.action == "star" ? true : ev.action == "unstar" ? false : nil
            patch(ev.ids) { if let read { $0.read = read }; if let star { $0.starred = star } }
            await refreshTree()
        case "refresh", "resync":
            await catchUp()
        default: break
        }
    }

    /// After coming back (or a live "refresh"): newer articles than the list
    /// holds are offered as "N new articles", never moved in under the reader.
    func catchUp() async {
        await refreshTree()
        guard let client, Self.n(tree.counts.newest) > Self.n(boundary), source != .starred else { return }
        if let page = try? await client.entries(query(for: source)) {
            let newer = page.entries.filter { Self.n($0.id) > Self.n(boundary) }
            if !newer.isEmpty, entries.isEmpty {
                await load()
            } else {
                pending = newer.count
            }
        }
    }

    /// Keeps unread and starred articles, with their text, for reading offline.
    func syncOffline() async {
        guard let client, let offline else { return }
        for (view, pages) in [("unread", 6), ("starred", 3)] {
            var cursor: String?
            for _ in 0..<pages {
                guard let page = try? await client.entries(.init(view: view, limit: 100, cursor: cursor, content: true)) else { return }
                await offline.save(entries: page.entries)
                if page.cursor.isEmpty { break }
                cursor = page.cursor
            }
        }
        await offline.prune()
    }

    /// Article IDs are numbers (crawl time in µs) sent as strings.
    static func n(_ id: String) -> Int64 { Int64(id) ?? 0 }

    // MARK: Errors

    private func isOffline(_ error: Error) -> Bool {
        (error as? URLError).map { [.notConnectedToInternet, .networkConnectionLost, .cannotConnectToHost, .timedOut, .cannotFindHost].contains($0.code) } ?? false
    }

    private func handle(_ error: Error) {
        if let e = error as? APIError, e.signedOut {
            Task { await signOut() }
            show("You were signed out. Sign in again.")
        }
    }

    func show(_ message: String) {
        toast = message
        let shown = message
        Task {
            try? await Task.sleep(for: .seconds(4))
            if toast == shown { toast = nil }
        }
    }
}
