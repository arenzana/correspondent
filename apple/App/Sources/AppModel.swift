import SwiftUI
import WidgetKit
import Network
import UserNotifications
#if os(iOS)
import BackgroundTasks
#endif
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
    var prefs = AppModel.withDeviceSizes(Prefs()) {
        didSet { Self.applyInterface(prefs) }
    }
    /// Moves when the Sun theme's phase changes (sunrise, before and after sunset), so
    /// views that read it redraw in the new theme.
    private(set) var sunPhase: Sun.Phase?
    /// Whether the reader set up text-to-speech (on the web): articles can be read aloud.
    var speechConfigured = false
    var online = true {
        didSet { if online != oldValue { connectionChanged() } }
    }
    /// The ribbon at the bottom, as YouTube's: shown while offline, and
    /// briefly once the connection is back.
    var ribbon: Ribbon?
    enum Ribbon: Equatable { case offline, backOnline }
    private var ribbonTask: Task<Void, Never>?

    private func connectionChanged() {
        ribbonTask?.cancel()
        if !online { ribbon = .offline; return }
        ribbon = .backOnline
        ribbonTask = Task {
            try? await Task.sleep(for: .seconds(2.5))
            if !Task.isCancelled, online { ribbon = nil }
        }
    }

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
    /// The list is scrolled to its top: arrivals can join it in place.
    var atTop = true

    // The article
    var selectedID: EntryID?
    var article: Entry?
    var articleError = ""
    var summaryOpen = false
    var summarizing = false

    /// Messages at the bottom, newest last, as the web's toasts; some offer an action (Undo).
    var notices: [Notice] = []
    private var noticeActions: [UUID: () async -> Void] = [:]
    /// Changes made offline, waiting to be sent.
    var outboxCount = 0

    private var offline: OfflineStore?
    private var outbox: Outbox?
    private var pathMonitor: NWPathMonitor?
    private var retryTask: Task<Void, Never>?
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
            if env["CORR_RIBBON"] == "offline" { ribbon = .offline } else if env["CORR_RIBBON"] == "back" { ribbon = .backOnline }
            if let theme = env["CORR_THEME"] { setTheme(theme) }
            if let link = env["CORR_OPEN_URL"].flatMap(URL.init(string:)) {
                Task { try? await Task.sleep(for: .seconds(2)); await open(link) } // as a widget, after the first screen
            }
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
        await outbox?.clear()
        outboxCount = 0
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
        outbox = Outbox(account: account.key)
        watchNetwork()
        watchSun()
        Task { await flushOutbox() }
        Task {
            if let me = try? await client?.me() { speechConfigured = me.speech != nil; self.me = me }
            notificationsAvailable = await client?.appleNotificationsAvailable() ?? false
            // Re-register each launch: tokens can change.
            if notificationsOn, notificationsAvailable { _ = await Notifications.shared.enable() }
            if let me = try? await client?.me(), let p = me.prefs {
                prefs = Self.withDeviceSizes(p, seed: true)
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

    /// Checks the Sun theme every minute; a change of phase redraws the app.
    private func watchSun() {
        Task { [weak self] in
            while let self {
                let phase = prefs.theme == "sun" ? Theme.sun(place: prefs.place).phase : nil
                if phase != sunPhase { sunPhase = phase }
                try? await Task.sleep(for: .seconds(60))
            }
        }
    }

    /// The theme in use now, on a device in this colour scheme.
    func theme(_ scheme: ColorScheme) -> Theme {
        _ = sunPhase
        return Theme.resolve(prefs.theme, scheme: scheme, place: prefs.place)
    }

    /// Picks a theme. Sun needs a rough position: asked for once, as on
    /// the web; refusing falls back to one estimated from the time zone
    /// (saved as estimated, so choosing Sun again asks again).
    func setTheme(_ id: String) {
        setPrefs({ $0.theme = id }, ["theme": id])
        sunPhase = id == "sun" ? Theme.sun(place: prefs.place).phase : nil
        guard id == "sun", prefs.place?.estimated ?? true else { return }
        Task {
            let place: Prefs.Place
            if let found = await Locator.place() {
                place = found
            } else if let saved = prefs.place {
                place = saved
            } else {
                let e = Sun.estimate()
                place = Prefs.Place(lat: e.lat, lon: e.lon, estimated: true)
            }
            setPrefs({ $0.place = place }, ["place": ["lat": place.lat, "lon": place.lon, "estimated": place.estimated] as [String: any Sendable]])
            sunPhase = prefs.theme == "sun" ? Theme.sun(place: prefs.place).phase : nil
        }
    }

    /// Sizes belong to the device, not the account: a phone, a laptop and
    /// the web each want their own. They're kept in this device's defaults.
    private static let deviceSizes: [(key: String, path: WritableKeyPath<Prefs, Double>)] = [
        ("readSize", \.readSize), ("measure", \.measure), ("uiScale", \.uiScale),
    ]

    /// The interface's size and typeface, set only when they change (each
    /// change redraws every view that shows text).
    static func applyInterface(_ p: Prefs) {
        if Typeface.uiScale != p.uiScale { Typeface.uiScale = p.uiScale }
        if Typeface.systemUI != (p.uiFont == "system") { Typeface.systemUI = p.uiFont == "system" }
    }

    /// Puts this device's sizes into the account's preferences. With `seed`,
    /// a size this device hasn't chosen yet starts from the account's.
    static func withDeviceSizes(_ p: Prefs, seed: Bool = false) -> Prefs {
        var p = p
        let d = UserDefaults.standard
        for s in deviceSizes {
            if d.object(forKey: "size." + s.key) != nil {
                p[keyPath: s.path] = d.double(forKey: "size." + s.key)
            } else if seed {
                d.set(p[keyPath: s.path], forKey: "size." + s.key)
            }
        }
        // The interface typeface is the account's, but the last one seen is
        // kept here too, so the app starts in it rather than rebuilding
        // itself when the account's preferences arrive.
        if seed { d.set(p.uiFont, forKey: "cache.uiFont") } else if let cached = d.string(forKey: "cache.uiFont") { p.uiFont = cached }
        applyInterface(p)
        return p
    }

    /// Changes not yet sent, gathered so a run of slider or stepper steps
    /// is one save (as the web waits 400 ms).
    private var unsaved: [String: any Sendable] = [:]
    private var saving: Task<Void, Never>?

    /// Changes preferences at once. Sizes stay on this device; the rest is
    /// saved with the account, sending only the keys that changed.
    func setPrefs(_ change: (inout Prefs) -> Void, _ keys: [String: any Sendable]) {
        let before = prefs
        change(&prefs)
        var keys = keys
        for s in Self.deviceSizes where keys[s.key] != nil {
            UserDefaults.standard.set(prefs[keyPath: s.path], forKey: "size." + s.key)
            keys[s.key] = nil
        }
        if before.unreadOnly != prefs.unreadOnly { Task { await load() } }
        guard !keys.isEmpty else { return }
        unsaved.merge(keys) { _, new in new }
        saving?.cancel()
        saving = Task {
            try? await Task.sleep(for: .milliseconds(400))
            guard !Task.isCancelled else { return }
            let changes = unsaved
            unsaved = [:]
            do {
                try await client?.savePrefs(changes)
                if changes["skipSponsored"] as? Bool == true { await refreshTree(); await load() }
            } catch {
                show("Couldn't save the setting. \(error.localizedDescription)")
            }
        }
    }

    // MARK: Loading

    func refreshTree() async {
        guard let client else { return }
        do {
            tree = try await client.tree()
            online = true
            await offline?.save(tree: tree, entries: [])
            await updateBadge()
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

    /// Which way the last selection moved through the list: 1 down, -1 up,
    /// 0 when it came from nowhere (the spine's wave runs that way).
    var selectionStep = 0

    func select(_ id: EntryID) async {
        guard let client else { return }
        dwell?.cancel()
        let from = selectedID.flatMap { old in entries.firstIndex { $0.id == old } }
        let to = entries.firstIndex { $0.id == id }
        selectionStep = from.flatMap { a in to.map { b in b == a ? 0 : (b > a ? 1 : -1) } } ?? 0
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
    /// The next or previous article; with `unread`, the next or previous unread one (n and p).
    func move(_ step: Int, unread: Bool = false) async {
        guard !entries.isEmpty else { return }
        var i = entries.firstIndex { $0.id == selectedID } ?? (step > 0 ? -1 : entries.count)
        repeat { i += step } while unread && entries.indices.contains(i) && entries[i].read
        guard entries.indices.contains(i) else {
            if step > 0 { await loadMore() }
            return
        }
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

    var fetchingFull = false

    func loadFullArticle() async {
        guard let client, let e = article, !fetchingFull else { return }
        fetchingFull = true
        defer { fetchingFull = false }
        do {
            let full = try await client.fullArticle(e.id)
            if article?.id == e.id { article = full }
            patchRow(e.id) { $0.fullText = true; $0.fullError = nil; $0.imageUrl = full.imageUrl }
        } catch {
            show(error.localizedDescription)
            if article?.id == e.id { article?.fullError = error.localizedDescription }
        }
    }

    /// Back to the text the feed published.
    func showFeedText() async {
        guard let client, let e = article else { return }
        do {
            let feedText = try await client.feedText(e.id)
            if article?.id == e.id { article = feedText }
            patchRow(e.id) { $0.fullText = false }
        } catch {
            show("Couldn't switch back. \(error.localizedDescription)")
        }
    }

    /// Full articles for everything new from a feed, starting with the open one.
    func fullTextForFeed(_ feed: Feed) async {
        await updateFeed(feed.id, .init(fullText: true), done: "Fetching full articles for \(feed.title). Recent ones are on their way.")
        if article?.feedId == feed.id, article?.fullText == false { await loadFullArticle() }
    }

    /// The question on a sponsored post: skip them from now on, or keep them.
    func answerSponsored(skip: Bool) {
        guard skip else { setPrefs({ $0.sponsoredAsked = true }, ["sponsoredAsked": true]); return }
        setPrefs({ $0.skipSponsored = true; $0.sponsoredAsked = true }, ["skipSponsored": true, "sponsoredAsked": true])
        if let e = article, !e.read { Task { await setRead([e.id], true) } }
        show("Sponsored posts will arrive already read. Change this in Settings → Reading.")
    }

    // MARK: Subscriptions and saved searches

    /// Runs a change to subscriptions, refreshes the sidebar, and says how it went.
    private func manage(_ done: String?, _ failed: String, _ work: (Client) async throws -> Void) async -> Bool {
        guard let client else { return false }
        do {
            try await work(client)
            await refreshTree()
            if let done { show(done) }
            return true
        } catch {
            show("\(failed) \(error.localizedDescription)")
            return false
        }
    }

    func subscribe(_ c: Candidate, folder: String?) async -> Bool {
        var id: Int64 = 0
        let ok = await manage("Subscribed to \(c.title.isEmpty ? c.url : c.title).", "Couldn't subscribe.") {
            id = try await $0.subscribe(c, folder: folder)
        }
        if ok { await go(.feed(id)) }
        return ok
    }

    func updateFeed(_ id: Int64, _ change: Client.FeedChange, done: String? = nil) async {
        _ = await manage(done, "Couldn't change the feed.") { try await $0.updateFeed(id, change) }
    }

    func refreshFeed(_ id: Int64) async {
        _ = await manage("Checking for new articles.", "Couldn't refresh the feed.") { try await $0.refreshFeed(id) }
    }

    func unsubscribe(_ feed: Feed) async {
        if await manage("Unsubscribed from \(feed.title).", "Couldn't unsubscribe.", { try await $0.unsubscribe(feed.id) }),
           source == .feed(feed.id) {
            await go(.unread)
        }
    }

    // Several feeds at once, from Manage subscriptions.

    /// Moves feeds into a folder by name (made if new), or out of folders with nil.
    func moveFeeds(_ ids: [Int64], to folder: String?) async {
        let change = folder.map { Client.FeedChange(folder: $0) } ?? Client.FeedChange(noFolder: true)
        let what = ids.count == 1 ? "the feed" : "\(ids.count) feeds"
        _ = await manage("Moved \(what) \(folder.map { "to \($0)" } ?? "out of folders").", "Couldn't move the feeds.") { client in
            for id in ids { try await client.updateFeed(id, change) }
        }
    }

    func setPaused(_ ids: [Int64], _ paused: Bool) async {
        let what = ids.count == 1 ? "the feed" : "\(ids.count) feeds"
        _ = await manage("\(paused ? "Paused" : "Resumed") \(what).", "Couldn't \(paused ? "pause" : "resume") the feeds.") { client in
            for id in ids { try await client.updateFeed(id, .init(paused: paused)) }
            if !paused { for id in ids { try? await client.refreshFeed(id) } }
        }
    }

    /// Unsubscribes; the feeds' articles go with them.
    func deleteFeeds(_ ids: [Int64]) async {
        let names = ids.map { feedsByID[$0]?.title ?? "the feed" }
        let gone = await manage(ids.count == 1 ? "Deleted \(names[0])." : "Deleted \(ids.count) feeds.", "Couldn't delete.") { client in
            for id in ids { try await client.unsubscribe(id) }
        }
        if gone, case .feed(let id) = source, ids.contains(id) { await go(.unread) }
    }

    /// A feed's new address; nil when it was saved, else why not.
    func setFeedURL(_ id: Int64, _ url: String) async -> String? {
        guard let client else { return "Not signed in." }
        do {
            try await client.updateFeed(id, .init(url: url.trimmingCharacters(in: .whitespaces)))
            try? await client.refreshFeed(id)
            await refreshTree()
            let f = feedsByID[id]
            show((f?.errorCount ?? 0) > 0 ? "Saved, but the new address failed too: \(f?.lastError ?? "")" : "Address saved and the feed updated.")
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    func createFolder(_ name: String) async {
        let name = name.trimmingCharacters(in: .whitespaces)
        guard !name.isEmpty else { return }
        _ = await manage(nil, "Couldn't create the folder.") { try await $0.createFolder(name) }
    }

    func rebuildFeed(_ feed: Feed) async {
        _ = await manage("Rebuilding \(feed.title). New articles will follow.", "Couldn't rebuild it.") { try await $0.rebuildFeed(feed.id) }
    }

    /// Checks a feed now, then says whether it worked.
    func updateNow(_ id: Int64) async {
        guard await manage(nil, "Couldn't update the feed.", { try await $0.refreshFeed(id) }) else { return }
        let f = feedsByID[id]
        show((f?.errorCount ?? 0) > 0 ? "Still failing: \(f?.lastError ?? "")" : "Updated \(f?.title ?? "the feed").")
    }

    func renameFolder(_ id: Int64, _ name: String) async {
        _ = await manage(nil, "Couldn't rename the folder.") { try await $0.renameFolder(id, name) }
    }

    func deleteFolder(_ folder: Folder) async {
        if await manage("Deleted the folder \(folder.name). Its feeds are kept.", "Couldn't delete the folder.", { try await $0.deleteFolder(folder.id) }),
           source == .folder(folder.id) {
            await go(.unread)
        }
    }

    /// Keeps the current search in the sidebar, and shows it from there.
    func saveSearch() async {
        let words = query.trimmingCharacters(in: .whitespaces)
        guard !words.isEmpty else { return }
        var saved: SavedSearch?
        if await manage(nil, "Couldn't save the search.", { saved = try await $0.saveSearch(words) }), let saved {
            await go(.search(saved.id))
            show("Saved the search “\(saved.name)”. It's in the sidebar.")
        }
    }

    func renameSearch(_ id: Int64, _ name: String) async {
        _ = await manage(nil, "Couldn't rename the search.") { try await $0.renameSearch(id, name) }
    }

    func deleteSearch(_ s: SavedSearch) async {
        guard await manage(nil, "Couldn't remove the search.", { try await $0.deleteSearch(s.id) }) else { return }
        if source == .search(s.id) { await go(.unread) }
        show("Removed the search “\(s.name)”.", action: "Undo") { [weak self] in
            guard let self, let client = self.client else { return }
            do {
                let again = try await client.saveSearch(s.query, name: s.name)
                await self.refreshTree()
                await self.go(.search(again.id))
            } catch {
                self.show("Couldn't bring the search back. \(error.localizedDescription)")
            }
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
            if isOffline(error) {
                await queue(.state(action: read ? "read" : "unread", ids: targets))
                return
            }
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
            if isOffline(error) {
                await queue(.state(action: starred ? "star" : "unstar", ids: [id]))
                return
            }
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
        let view = source == .today ? "today" : nil
        do {
            let ids = try await client.markAllRead(feed: q.feed, folder: q.folder, view: view, search: q.search, before: boundary)
            if ids.isEmpty { show("Nothing to mark as read."); return }
            patch(ids) { $0.read = true }
            await refreshTree()
            show("Marked \(ids.count) \(ids.count == 1 ? "article" : "articles") as read.", action: "Undo") { [weak self] in
                guard let self, let client = self.client else { return }
                do {
                    for start in stride(from: 0, to: ids.count, by: 1000) {
                        _ = try await client.setState("unread", Array(ids[start..<min(start + 1000, ids.count)]))
                    }
                    self.patch(ids) { $0.read = false }
                    await self.refreshTree()
                    self.show("Restored as unread.")
                } catch {
                    self.show("Couldn't restore them. \(error.localizedDescription)")
                }
            }
        } catch {
            if isOffline(error) {
                // Mark what's here now; the server applies the same boundary when it's sent.
                let ids = entries.filter { !$0.read }.map(\.id)
                patch(ids) { $0.read = true }
                await offline?.patch(ids, read: true)
                await queue(.markAll(feed: q.feed, folder: q.folder, view: view, search: q.search, before: boundary))
                show("Marked as read. It will sync when you're back online.")
                return
            }
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

    /// Queues a change made without a network.
    private func queue(_ op: OutboxOp) async {
        online = false
        await outbox?.enqueue(op)
        outboxCount = await outbox?.count ?? 0
        retryWhileWaiting()
    }

    /// Sends the changes made offline; says so once they're through.
    func flushOutbox() async {
        guard let client, let outbox, await outbox.count > 0 else { outboxCount = 0; return }
        let r = await outbox.flush(client)
        outboxCount = await outbox.count
        switch r.stopped {
        case "offline": online = false
        case "signedOut": handle(APIError(status: 401, message: ""))
        default:
            if r.left == 0 && r.sent > 0 {
                online = true
                show(r.sent == 1 ? "Synced your offline change." : "Synced your offline changes.")
                await refreshTree()
            }
        }
    }

    /// iOS has no background sync for this: while changes wait, keep trying.
    private func retryWhileWaiting() {
        guard retryTask == nil else { return }
        retryTask = Task { [weak self] in
            while let self, self.outboxCount > 0, !Task.isCancelled {
                try? await Task.sleep(for: .seconds(30))
                await self.flushOutbox()
            }
            self?.retryTask = nil
        }
    }

    /// Notices the network coming back, and sends what waited.
    private func watchNetwork() {
        pathMonitor?.cancel()
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            let up = path.status == .satisfied
            let constrained = path.isConstrained || path.isExpensive
            Task { @MainActor in self?.constrained = constrained }
            Task { @MainActor in
                guard let self else { return }
                // Losing the network shows at once, not at the next failed request.
                guard up else { self.online = false; return }
                await self.flushOutbox()
                // Back: online once the server answers (refreshTree sets it).
                if !self.online { await self.refreshTree(); await self.load() }
            }
        }
        monitor.start(queue: DispatchQueue(label: "network"))
        pathMonitor = monitor
    }

    /// The app came back to the front.
    func resumed() async {
        await flushOutbox()
        await catchUp()
    }

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

    /// After coming back (or a live "refresh"): articles newer than the
    /// list holds join it in place when it's at the top, as on the web;
    /// further down they're offered as "N new articles", never moved in
    /// under the reader.
    func catchUp() async {
        await refreshTree()
        guard let client, Self.n(tree.counts.newest) > Self.n(boundary), source != .starred else { return }
        guard let page = try? await client.entries(query(for: source)) else { return }
        let newer = page.entries.filter { Self.n($0.id) > Self.n(boundary) }
        if newer.isEmpty { return }
        if entries.isEmpty { await load(); return }
        guard atTop, query.isEmpty else { pending = newer.count; return }
        let have = Set(entries.map(\.id))
        let inPage = Set(page.entries.map(\.id))
        // The fresh page in its order, then anything loaded beyond it.
        entries = page.entries.filter { have.contains($0.id) || Self.n($0.id) > Self.n(boundary) }
            + entries.filter { !inPage.contains($0.id) }
        boundary = newer.map(\.id).max { Self.n($0) < Self.n($1) } ?? boundary
        pending = 0
    }

    /// Keeps unread and starred articles, with their text, for reading offline.
    func syncOffline() async {
        guard let client, let offline else { return }
        var withAudio: [Entry] = []
        for (view, pages) in [("unread", 6), ("starred", 3)] {
            var cursor: String?
            for _ in 0..<pages {
                guard let page = try? await client.entries(.init(view: view, limit: 100, cursor: cursor, content: true)) else { return }
                await offline.save(entries: page.entries)
                withAudio += page.entries.filter(\.audio)
                if page.cursor.isEmpty { break }
                cursor = page.cursor
            }
        }
        await offline.prune()
        if offlineAudio, !constrained { await keepAudio(withAudio) }
    }

    // MARK: Offline audio

    /// The reader's account details for Settings (setups, never keys).
    var me: Me?

    /// Keep read-aloud audio on this device for listening offline, as the
    /// web's setting (here, per device).
    var offlineAudio: Bool = UserDefaults.standard.bool(forKey: "offlineAudio") {
        didSet {
            UserDefaults.standard.set(offlineAudio, forKey: "offlineAudio")
            if offlineAudio { Task { await syncOffline() } }
        }
    }
    /// Low Data Mode or an expensive network: no audio downloads then.
    private(set) var constrained = false

    /// Downloads the audio of the newest articles that have it, up to what
    /// the player keeps (Player.kept), skipping files already here.
    private func keepAudio(_ entries: [Entry]) async {
        guard let client else { return }
        let newest = entries.sorted { $0.publishedAt > $1.publishedAt }.prefix(Player.kept)
        for e in newest {
            let file = Player.folder.appendingPathComponent("\(e.id).mp3")
            if FileManager.default.fileExists(atPath: file.path) { continue }
            guard let data = try? await client.audio(e.id) else { continue }
            try? data.write(to: file, options: .atomic)
        }
    }

    /// Article IDs are numbers (crawl time in µs) sent as strings.
    static func n(_ id: String) -> Int64 { Int64(id) ?? 0 }

    // MARK: Notifications

    /// Whether the server can notify the Apple apps (it has an APNs key).
    var notificationsAvailable = false
    var notificationsOn: Bool = UserDefaults.standard.bool(forKey: "notifications") {
        didSet { UserDefaults.standard.set(notificationsOn, forKey: "notifications") }
    }

    func setNotifications(_ on: Bool) async {
        if on {
            guard await Notifications.shared.enable() else {
                show("Allow notifications for Correspondent in the Settings app first.")
                return
            }
            notificationsOn = true
            if !tree.feeds.contains(where: \.notify) { show("Now choose feeds: long-press one and turn on its bell.") }
        } else {
            await Notifications.shared.disable()
            notificationsOn = false
        }
    }

    func testNotification() async {
        guard let client else { return }
        do {
            let n = try await client.testNotification()
            show(n > 0 ? "Sent. It should arrive in a few seconds." : "No device is registered for notifications.")
        } catch {
            show(error.localizedDescription)
        }
    }

    // MARK: Background refresh and the badge

    static let refreshTask = "org.arenzana.correspondent.refresh"

    /// The unread count on the app icon (iOS asks first) or the Dock.
    private var lastWidgetCount = -1

    var showBadge: Bool = UserDefaults.standard.bool(forKey: "showBadge") {
        didSet {
            UserDefaults.standard.set(showBadge, forKey: "showBadge")
            Task { await updateBadge() }
        }
    }

    func enableBadge(_ on: Bool) async {
        #if os(iOS)
        if on {
            let granted = (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.badge])) ?? false
            if !granted { show("Allow badges for Correspondent in the Settings app to show the unread count."); return }
        }
        #endif
        showBadge = on
    }

    /// Opens what a widget points at: correspondent://unread or /entry/ID.
    func open(_ url: URL) async {
        guard url.scheme == "correspondent" else { return }
        if url.host() == "entry", let id = url.pathComponents.last, id != "/" {
            if source != .unread { await go(.unread) }
            await select(id)
            openRequest = id
        } else {
            await go(.unread)
        }
    }

    /// An article a link asked to open (the iPhone layout pushes it).
    var openRequest: EntryID?

    func updateBadge() async {
        let n = showBadge ? tree.counts.unread : 0
        if n != lastWidgetCount {
            lastWidgetCount = n
            WidgetCenter.shared.reloadAllTimelines()
        }
        #if os(iOS)
        try? await UNUserNotificationCenter.current().setBadgeCount(n)
        #else
        NSApplication.shared.dockTile.badgeLabel = n > 0 ? "\(n)" : nil
        #endif
    }

    /// iOS's few seconds in the background: send what waited, refresh the
    /// counts and the badge, and keep the offline copy fresh.
    func backgroundRefresh() async {
        if client == nil, let (account, token) = Account.load() {
            start(account: account, token: token)
        }
        await flushOutbox()
        await refreshTree()
        await syncOffline()
        scheduleRefresh()
    }

    /// Asks iOS to wake the app again in a while.
    func scheduleRefresh() {
        #if os(iOS)
        let request = BGAppRefreshTaskRequest(identifier: Self.refreshTask)
        request.earliestBeginDate = Date(timeIntervalSinceNow: 20 * 60)
        try? BGTaskScheduler.shared.submit(request)
        #endif
    }

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

    /// Shows a message for a few seconds; with an action (Undo), a little longer.
    func show(_ message: String, action: String? = nil, run: (() async -> Void)? = nil) {
        let n = Notice(message: message, action: run == nil ? nil : action)
        notices.append(n)
        if notices.count > 3 { dismiss(notices[0].id) }
        if let run { noticeActions[n.id] = run }
        Task {
            try? await Task.sleep(for: .seconds(n.action == nil ? 4 : 7))
            dismiss(n.id)
        }
    }

    func dismiss(_ id: UUID) {
        notices.removeAll { $0.id == id }
        noticeActions[id] = nil
    }

    /// The notice's button: runs its action once.
    func act(_ n: Notice) {
        guard let run = noticeActions[n.id] else { return }
        dismiss(n.id)
        Task { await run() }
    }
}

/// A message at the bottom of the window, maybe with an action.
struct Notice: Identifiable, Equatable {
    let id = UUID()
    var message: String
    var action: String?
}
