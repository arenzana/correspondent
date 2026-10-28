import Foundation

/// The offline copy, like the web app's (ui/src/lib/offline): the feed tree
/// and articles with their text, kept as JSON in Application Support, one
/// file per account. Unlike a browser's, iOS never evicts it.
public actor OfflineStore {
    private let url: URL
    private var snapshot = Snapshot()
    private var loaded = false

    struct Snapshot: Codable {
        var tree = Tree()
        var entries: [EntryID: Entry] = [:]
        var savedAt: Date?
    }

    /// The store for one account (server and username).
    public init(account: String, directory: URL? = nil) {
        let dir = directory ?? FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("Correspondent", isDirectory: true)
        let safe = account.map { $0.isLetter || $0.isNumber ? $0 : "_" }
        url = dir.appendingPathComponent(String(safe) + ".json")
    }

    private func load() {
        guard !loaded else { return }
        loaded = true
        if let data = try? Data(contentsOf: url), let s = try? JSONDecoder().decode(Snapshot.self, from: data) {
            snapshot = s
        }
    }

    /// Writes the copy a moment later: the whole file is rewritten, text and
    /// all, so a burst of changes (opening an article saves, marks it read,
    /// refreshes the counts) becomes one write. flush() writes at once.
    private var writing: Task<Void, Never>?
    private func persist() {
        guard writing == nil else { return }
        writing = Task { [weak self] in
            try? await Task.sleep(for: .seconds(2))
            if Task.isCancelled { return }
            await self?.write()
        }
    }

    /// Writes now anything not yet written (the app is going to the background).
    public func flush() {
        guard writing != nil else { return }
        write()
    }

    private func write() {
        writing?.cancel()
        writing = nil
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        if let data = try? JSONEncoder().encode(snapshot) {
            try? data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        }
    }

    public var savedAt: Date? { load(); return snapshot.savedAt }

    public func tree() -> Tree? { load(); return snapshot.savedAt == nil ? nil : snapshot.tree }

    public func entry(_ id: EntryID) -> Entry? { load(); return snapshot.entries[id] }

    /// Saves the tree and articles; a summary never replaces text already kept.
    public func save(tree: Tree? = nil, entries: [Entry]) {
        load()
        if let tree { snapshot.tree = tree }
        for e in entries {
            var e = e
            if e.content == nil { e.content = snapshot.entries[e.id]?.content }
            snapshot.entries[e.id] = e
        }
        snapshot.savedAt = Date()
        persist()
    }

    /// Applies a change made while offline or announced live.
    public func patch(_ ids: [EntryID], read: Bool? = nil, starred: Bool? = nil) {
        load()
        for id in ids {
            if let read { snapshot.entries[id]?.read = read }
            if let starred { snapshot.entries[id]?.starred = starred }
        }
        persist()
    }

    /// Keeps unread and starred articles, and the newest `keep` of the rest.
    public func prune(keep: Int = 1500) {
        load()
        let removable = snapshot.entries.values.filter { $0.read && !$0.starred }.sorted { $0.publishedAt > $1.publishedAt }
        for e in removable.dropFirst(keep) { snapshot.entries[e.id] = nil }
        persist()
    }

    /// The kept articles a list would show, newest first.
    public func list(_ source: Source, unreadOnly: Bool, search: String?, folders: [Int64: Set<Int64>]) -> [Entry] {
        load()
        let needle = search?.trimmingCharacters(in: .whitespaces).lowercased() ?? ""
        let startOfDay = Calendar.current.startOfDay(for: Date())
        return snapshot.entries.values.filter { e in
            switch source {
            case .unread: if e.read { return false }
            case .starred: if !e.starred { return false }
            case .today: if e.published < startOfDay { return false }
            case .all, .search: if unreadOnly && e.read { return false }
            case .feed(let id): if e.feedId != id || (unreadOnly && e.read) { return false }
            case .folder(let id): if !(folders[id]?.contains(e.feedId) ?? false) || (unreadOnly && e.read) { return false }
            }
            if !needle.isEmpty {
                let text = "\(e.title) \(e.excerpt) \(e.author) \(e.content ?? "")".lowercased()
                if !needle.split(separator: " ").allSatisfy({ text.contains($0) }) { return false }
            }
            return true
        }.sorted { ($0.publishedAt, $0.id) > ($1.publishedAt, $1.id) }
    }

    /// Forget everything (sign-out).
    public func clear() {
        writing?.cancel()
        writing = nil
        snapshot = Snapshot()
        try? FileManager.default.removeItem(at: url)
    }
}
