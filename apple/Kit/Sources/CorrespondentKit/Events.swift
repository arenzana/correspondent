import Foundation

/// A change the server announces: new articles arrived ("refresh"), or
/// articles were read or starred anywhere ("state"); after a gap the
/// server asks for a "resync".
public struct LiveEvent: Sendable, Equatable {
    public var name: String
    public var action: String
    public var ids: [EntryID]
}

extension Client {
    /// Follows /api/v1/events (server-sent events) until the task is
    /// cancelled or the connection drops; the caller reconnects.
    public func events() -> AsyncThrowingStream<LiveEvent, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    var req = try request("GET", "/events")
                    req.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    req.timeoutInterval = 24 * 3600
                    let (bytes, resp) = try await URLSession.shared.bytes(for: req)
                    let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
                    guard status == 200 else { throw APIError(status: status, message: "Live updates are unavailable (\(status)).") }
                    var parser = EventParser()
                    for try await line in bytes.lines {
                        if let ev = parser.feed(line) { continuation.yield(ev) }
                    }
                    // bytes.lines drops the blank line that ends the last event.
                    if let ev = parser.feed("") { continuation.yield(ev) }
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}

/// Reads the text/event-stream format one line at a time.
public struct EventParser: Sendable {
    private var name = "message"
    private var data = ""

    public init() {}

    public mutating func feed(_ line: String) -> LiveEvent? {
        if line.isEmpty {
            defer { name = "message"; data = "" }
            if data.isEmpty && name == "message" { return nil }
            struct Payload: Decodable { var action: String?; var ids: [EntryID]? }
            let p = (try? JSONDecoder().decode(Payload.self, from: Data(data.utf8)))
            return LiveEvent(name: name, action: p?.action ?? "", ids: p?.ids ?? [])
        }
        if line.hasPrefix(":") { return nil } // a comment: keepalive
        let (field, value) = line.firstIndex(of: ":").map {
            (String(line[..<$0]), String(line[line.index(after: $0)...]).trimmingPrefix(" "))
        } ?? (line, "")
        switch field {
        case "event": name = String(value)
        case "data": data += data.isEmpty ? String(value) : "\n" + value
        default: break
        }
        return nil
    }
}
