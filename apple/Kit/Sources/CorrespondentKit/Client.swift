import Foundation

/// A refusal from the server, with its message for the reader.
public struct APIError: Error, LocalizedError, Sendable {
    public var status: Int
    public var message: String
    public var errorDescription: String? { message }

    /// The token was revoked or the account disabled: sign in again.
    public var signedOut: Bool { status == 401 }

    public init(status: Int, message: String) {
        self.status = status
        self.message = message
    }
}

/// Talks to one Correspondent server with a personal API token, through
/// the same /api/v1 the web app, corr and Reeder's companion use.
public final class Client: Sendable {
    public let server: URL
    public let token: String
    private let session: URLSession

    public init(server: URL, token: String, session: URLSession = .shared) {
        self.server = server
        self.token = token
        self.session = session
    }

    /// Exchanges a username and password for a token named after this
    /// device; the password is never kept.
    public static func signIn(server: URL, username: String, password: String, device: String,
                              session: URLSession = .shared) async throws -> (token: String, username: String) {
        struct Out: Decodable { var token: String; var username: String }
        let anonymous = Client(server: server, token: "", session: session)
        let out: Out = try await anonymous.call("POST", "/tokens/login", body: [
            "username": username, "password": password, "name": device, "scope": "write",
        ])
        return (out.token, out.username)
    }

    /// Normalizes what a reader types as the server address.
    public static func serverURL(_ typed: String) -> URL? {
        var s = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.isEmpty { return nil }
        if !s.contains("://") { s = "https://" + s }
        while s.hasSuffix("/") { s.removeLast() }
        guard let u = URL(string: s), let scheme = u.scheme, ["https", "http"].contains(scheme), u.host != nil else { return nil }
        return u
    }

    // MARK: Calls

    public func me() async throws -> Me { try await call("GET", "/me") }

    public func tree() async throws -> Tree { try await call("GET", "/tree") }

    public struct Query: Sendable, Hashable {
        public var view: String?
        public var feed: Int64?
        public var folder: Int64?
        public var search: String?
        public var limit = 60
        public var cursor: String?
        public var content = false
        public init(view: String? = nil, feed: Int64? = nil, folder: Int64? = nil, search: String? = nil,
                    limit: Int = 60, cursor: String? = nil, content: Bool = false) {
            self.view = view
            self.feed = feed
            self.folder = folder
            self.search = search
            self.limit = limit
            self.cursor = cursor
            self.content = content
        }
    }

    public func entries(_ q: Query) async throws -> Page {
        var items: [URLQueryItem] = [URLQueryItem(name: "limit", value: String(q.limit))]
        if let v = q.view { items.append(URLQueryItem(name: "view", value: v)) }
        if let f = q.feed { items.append(URLQueryItem(name: "feed", value: String(f))) }
        if let f = q.folder { items.append(URLQueryItem(name: "folder", value: String(f))) }
        if let s = q.search, !s.isEmpty { items.append(URLQueryItem(name: "q", value: s)) }
        if let c = q.cursor, !c.isEmpty { items.append(URLQueryItem(name: "cursor", value: c)) }
        if q.content { items.append(URLQueryItem(name: "content", value: "1")) }
        var page: Page = try await call("GET", "/entries", query: items)
        page.entries = page.entries.map { e in var e = e; e.content = e.content ?? (q.content ? "" : nil); return e }
        return page
    }

    public func entry(_ id: EntryID) async throws -> Entry {
        struct Out: Decodable { var entry: Entry }
        let out: Out = try await call("GET", "/entries/\(id)")
        var e = out.entry
        e.content = e.content ?? ""
        return e
    }

    /// read, unread, star or unstar; returns how many changed.
    @discardableResult
    public func setState(_ action: String, _ ids: [EntryID]) async throws -> Int {
        struct Out: Decodable { var changed: Int }
        let out: Out = try await call("POST", "/entries/state", body: ["action": action, "ids": ids] as [String: any Sendable])
        return out.changed
    }

    /// Marks a list read up to the newest article the reader saw (before).
    public func markAllRead(feed: Int64? = nil, folder: Int64? = nil, view: String? = nil, search: String? = nil,
                            before: String) async throws -> [EntryID] {
        struct Out: Decodable { var ids: [EntryID] }
        var body: [String: String] = ["before": before]
        if let feed { body["feed"] = String(feed) }
        if let folder { body["folder"] = String(folder) }
        if let view { body["view"] = view }
        if let search, !search.isEmpty { body["q"] = search }
        let out: Out = try await call("POST", "/entries/mark-all-read", body: body)
        return out.ids
    }

    public func fullArticle(_ id: EntryID) async throws -> Entry {
        struct Out: Decodable { var entry: Entry }
        let out: Out = try await call("POST", "/entries/\(id)/full")
        return out.entry
    }

    /// The article's short summary: the shared one, or written now by the reader's AI.
    public func summarize(_ id: EntryID) async throws -> String {
        struct Out: Decodable { var summary: String }
        let out: Out = try await call("POST", "/entries/\(id)/summary")
        return out.summary
    }

    /// Changes some preferences. The server keeps them as one document the
    /// web app also writes (pane widths, the Sun theme's place…), so this
    /// reads the current one, changes only these keys, and writes it back.
    @discardableResult
    public func savePrefs(_ changes: [String: any Sendable]) async throws -> Prefs {
        let me = try await data("GET", "/me")
        let root = (try JSONSerialization.jsonObject(with: me)) as? [String: Any]
        var prefs = root?["prefs"] as? [String: Any] ?? [:]
        for (k, v) in changes { prefs[k] = v }
        let body = try JSONSerialization.data(withJSONObject: prefs)
        _ = try await data("PUT", "/prefs", raw: body)
        return try JSONDecoder().decode(Prefs.self, from: body)
    }

    /// Revokes this device's token.
    public func signOut() async throws {
        let _: Empty = try await call("DELETE", "/tokens/current")
    }

    /// An address the server gave (icons, images), made absolute.
    public func absolute(_ path: String) -> URL? {
        if path.isEmpty { return nil }
        if let u = URL(string: path), u.scheme != nil { return u }
        return URL(string: path, relativeTo: server.appendingPathComponent("/"))?.absoluteURL
    }

    // MARK: Plumbing

    struct Empty: Decodable {}
    struct Failure: Decodable { var error: String }

    func request(_ method: String, _ path: String, query: [URLQueryItem] = [], body: (any Sendable)? = nil) throws -> URLRequest {
        var comps = URLComponents(url: server.appendingPathComponent("api/v1" + path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { comps.queryItems = query }
        var req = URLRequest(url: comps.url!)
        req.httpMethod = method
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        if !token.isEmpty { req.setValue("Bearer " + token, forHTTPHeaderField: "Authorization") }
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        req.timeoutInterval = 60
        return req
    }

    /// A call whose body is sent and returned as is.
    func data(_ method: String, _ path: String, raw: Data? = nil) async throws -> Data {
        var req = try request(method, path)
        if let raw {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = raw
        }
        let (data, resp) = try await session.data(for: req)
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            let message = (try? JSONDecoder().decode(Failure.self, from: data))?.error ?? "The server answered \(status)."
            throw APIError(status: status, message: message)
        }
        return data
    }

    func call<T: Decodable>(_ method: String, _ path: String, query: [URLQueryItem] = [], body: (any Sendable)? = nil) async throws -> T {
        let (data, resp) = try await session.data(for: request(method, path, query: query, body: body))
        let status = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            let message = (try? JSONDecoder().decode(Failure.self, from: data))?.error ?? "The server answered \(status)."
            throw APIError(status: status, message: message)
        }
        if status == 204 || data.isEmpty, let empty = Empty() as? T { return empty }
        return try JSONDecoder().decode(T.self, from: data)
    }
}
