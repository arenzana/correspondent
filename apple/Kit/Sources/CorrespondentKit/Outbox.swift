import Foundation

/// A change made without a network, waiting to be sent: as the web app's
/// outbox (ui/src/lib/offline/outbox.ts), with the same rules.
public enum OutboxOp: Codable, Equatable, Sendable {
    /// read, unread, star or unstar.
    case state(action: String, ids: [EntryID])
    /// Mark a list read up to `before`; `search` limits it to a search's matches.
    case markAll(feed: Int64?, folder: Int64?, view: String?, search: String?, before: String)
}

public struct FlushResult: Equatable, Sendable {
    public var sent: Int
    public var left: Int
    /// Why it stopped early: offline (try later), signedOut, or server (try later).
    public var stopped: String?
}

/// The queue, kept in a file per account so it survives quitting the app.
public actor Outbox {
    private let url: URL
    private var ops: [OutboxOp] = []
    private var loaded = false
    private var flushing: Task<FlushResult, Never>?

    public init(account: String, directory: URL? = nil) {
        let dir = directory ?? FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("Correspondent", isDirectory: true)
        let safe = account.map { $0.isLetter || $0.isNumber ? $0 : "_" }
        url = dir.appendingPathComponent(String(safe) + ".outbox.json")
    }

    private func load() {
        guard !loaded else { return }
        loaded = true
        if let data = try? Data(contentsOf: url), let saved = try? JSONDecoder().decode([OutboxOp].self, from: data) { ops = saved }
    }

    private func persist() {
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        if ops.isEmpty { try? FileManager.default.removeItem(at: url); return }
        if let data = try? JSONEncoder().encode(ops) { try? data.write(to: url, options: .atomic) }
    }

    public var count: Int { load(); return ops.count }

    public func enqueue(_ op: OutboxOp) {
        load()
        ops.append(op)
        persist()
    }

    public func clear() {
        ops = []
        loaded = true
        persist()
    }

    /// Merges runs of state changes so a burst of offline actions becomes a
    /// few requests: within a run, the last action per article and axis
    /// (read, star) wins. Mark-all operations keep their place.
    public static func plan(_ ops: [OutboxOp]) -> [OutboxOp] {
        var out: [OutboxOp] = []
        var read: [(EntryID, Bool)] = [], star: [(EntryID, Bool)] = []
        func set(_ list: inout [(EntryID, Bool)], _ id: EntryID, _ v: Bool) {
            list.removeAll { $0.0 == id }
            list.append((id, v))
        }
        func close() {
            for (axis, yes, no) in [(read, "read", "unread"), (star, "star", "unstar")] {
                let on = axis.filter(\.1).map(\.0), off = axis.filter { !$0.1 }.map(\.0)
                if !on.isEmpty { out.append(.state(action: yes, ids: on)) }
                if !off.isEmpty { out.append(.state(action: no, ids: off)) }
            }
            read = []; star = []
        }
        for op in ops {
            switch op {
            case .markAll:
                close()
                out.append(op)
            case .state(let action, let ids):
                for id in ids {
                    switch action {
                    case "read": set(&read, id, true)
                    case "unread": set(&read, id, false)
                    case "star": set(&star, id, true)
                    case "unstar": set(&star, id, false)
                    default: break
                    }
                }
            }
        }
        close()
        return out
    }

    /// Sends everything queued, in order. Concurrent calls share one run.
    public func flush(_ client: Client) async -> FlushResult {
        if let running = flushing { return await running.value }
        let task = Task { await run(client) }
        flushing = task
        let result = await task.value
        flushing = nil
        return result
    }

    private func run(_ client: Client) async -> FlushResult {
        load()
        let queued = ops
        guard !queued.isEmpty else { return FlushResult(sent: 0, left: 0) }
        var sent = 0
        for step in Self.plan(queued) {
            do {
                switch step {
                case .state(let action, let ids):
                    // The server takes up to 1000 IDs at a time.
                    for start in stride(from: 0, to: ids.count, by: 1000) {
                        try await client.setState(action, Array(ids[start..<min(start + 1000, ids.count)]))
                    }
                case .markAll(let feed, let folder, let view, let search, let before):
                    _ = try await client.markAllRead(feed: feed, folder: folder, view: view, search: search, before: before)
                }
                sent += 1
            } catch let e as APIError {
                if e.status == 401 { return FlushResult(sent: sent, left: queued.count, stopped: "signedOut") }
                if e.status >= 500 || e.status == 429 { return FlushResult(sent: sent, left: queued.count, stopped: "server") }
                // Other refusals (an article that no longer exists): retrying can't help.
                sent += 1
            } catch {
                return FlushResult(sent: sent, left: queued.count, stopped: "offline")
            }
        }
        // Everything planned went through: clear exactly what was read, keeping
        // anything queued meanwhile.
        ops.removeFirst(min(queued.count, ops.count))
        persist()
        return FlushResult(sent: sent, left: 0)
    }
}
