import Foundation
import Testing
@testable import CorrespondentKit

/// Answers requests from a table instead of the network, recording them.
final class FakeServer: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var routes: [String: (Int, String)] = [:]
    nonisolated(unsafe) static var seen: [URLRequest] = []

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        var req = request
        if req.httpBody == nil, let stream = req.httpBodyStream {
            stream.open()
            var data = Data()
            var buf = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable { let n = stream.read(&buf, maxLength: buf.count); if n <= 0 { break }; data.append(buf, count: n) }
            req.httpBody = data
        }
        Self.seen.append(req)
        let key = "\(request.httpMethod ?? "GET") \(request.url!.path())"
        let (status, body) = Self.routes[key] ?? (404, #"{"error":"No such route."}"#)
        let resp = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: resp, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}

    static func session() -> URLSession {
        let c = URLSessionConfiguration.ephemeral
        c.protocolClasses = [FakeServer.self]
        return URLSession(configuration: c)
    }
}

@Suite(.serialized) struct ClientTests {
    let server = URL(string: "https://rss.example.org")!

    @Test func signsInAndSendsTheToken() async throws {
        FakeServer.routes = [
            "POST /api/v1/tokens/login": (200, #"{"token":"corr_abc","username":"isma"}"#),
            "GET /api/v1/tree": (200, #"{"folders":[{"id":1,"name":"Apple","unread":3}],"feeds":[{"id":7,"folderId":1,"title":"Daring Fireball","unread":3}],"counts":{"unread":3,"starred":1,"today":2,"newest":"99"}}"#),
        ]
        FakeServer.seen = []
        let (token, user) = try await Client.signIn(server: server, username: "isma", password: "pw", device: "iPhone", session: FakeServer.session())
        #expect(token == "corr_abc" && user == "isma")
        let login = try #require(FakeServer.seen.first)
        let body = try JSONSerialization.jsonObject(with: login.httpBody ?? Data()) as? [String: String]
        #expect(body?["name"] == "iPhone" && body?["scope"] == "write")
        #expect(login.value(forHTTPHeaderField: "Authorization") == nil)

        let c = Client(server: server, token: token, session: FakeServer.session())
        let tree = try await c.tree()
        #expect(tree.feeds.first?.title == "Daring Fireball" && tree.counts.newest == "99" && tree.searches.isEmpty)
        #expect(FakeServer.seen.last?.value(forHTTPHeaderField: "Authorization") == "Bearer corr_abc")
    }

    @Test func listsAndExplainsRefusals() async throws {
        FakeServer.routes = [
            "GET /api/v1/entries": (200, #"{"entries":[{"id":"1793","feedId":7,"title":"Hello","publishedAt":1790000000,"read":false}],"cursor":"c1"}"#),
            "POST /api/v1/entries/state": (401, #"{"error":"Sign in again."}"#),
        ]
        FakeServer.seen = []
        let c = Client(server: server, token: "t", session: FakeServer.session())
        let page = try await c.entries(.init(view: "unread", search: "duo phone", limit: 30))
        #expect(page.entries.first?.title == "Hello" && page.cursor == "c1" && page.entries.first?.content == nil)
        let q = URLComponents(url: FakeServer.seen[0].url!, resolvingAgainstBaseURL: false)!.queryItems ?? []
        #expect(q.contains(URLQueryItem(name: "q", value: "duo phone")) && q.contains(URLQueryItem(name: "view", value: "unread")))

        await #expect(throws: APIError.self) { try await c.setState("read", ["1793"]) }
        do { try await c.setState("read", ["1793"]) } catch let e as APIError {
            #expect(e.signedOut && e.message == "Sign in again.")
        }
    }

    @Test func savesPrefsWithoutLosingTheWebsOwn() async throws {
        FakeServer.routes = [
            "GET /api/v1/me": (200, #"{"username":"isma","prefs":{"theme":"night","navWidth":280,"place":{"lat":40.4,"lon":-3.7}}}"#),
            "PUT /api/v1/prefs": (204, ""),
        ]
        FakeServer.seen = []
        let c = Client(server: server, token: "t", session: FakeServer.session())
        let p = try await c.savePrefs(["readSize": 21, "theme": "evening"])
        #expect(p.theme == "evening" && p.readSize == 21 && p.unreadOnly)
        let put = try #require(FakeServer.seen.last)
        let sent = try JSONSerialization.jsonObject(with: put.httpBody ?? Data()) as? [String: Any]
        #expect(sent?["navWidth"] as? Int == 280 && sent?["place"] != nil && sent?["theme"] as? String == "evening")
    }

        @Test func normalizesTheServerAddress() {
        #expect(Client.serverURL("rss.arenzana.org/")?.absoluteString == "https://rss.arenzana.org")
        #expect(Client.serverURL("http://localhost:8080")?.absoluteString == "http://localhost:8080")
        #expect(Client.serverURL("ftp://x") == nil && Client.serverURL("  ") == nil)
        let c = Client(server: server, token: "")
        #expect(c.absolute("/img/abc")?.absoluteString == "https://rss.example.org/img/abc")
        #expect(c.absolute("https://cdn.example/x.png")?.absoluteString == "https://cdn.example/x.png")
    }
}

@Test func parsesLiveEvents() {
    var p = EventParser()
    var got: [LiveEvent] = []
    for line in [": keepalive", "event: state", #"data: {"action":"read","ids":["1","2"]}"#, "", "event: refresh", "data: {}", ""] {
        if let e = p.feed(line) { got.append(e) }
    }
    #expect(got == [LiveEvent(name: "state", action: "read", ids: ["1", "2"]), LiveEvent(name: "refresh", action: "", ids: [])])
}

@Test func keepsAnOfflineCopy() async throws {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    let store = OfflineStore(account: "isma@rss.example.org", directory: dir)
    let now = Int64(Date().timeIntervalSince1970)
    await store.save(tree: Tree(feeds: [Feed(id: 7, folderId: 1, title: "DF")]), entries: [
        Entry(id: "1", feedId: 7, title: "Duo review", content: "<p>The iPhone Duo folds.</p>", publishedAt: now),
        Entry(id: "2", feedId: 8, title: "Gardens", content: "<p>Roses.</p>", publishedAt: now - 10, read: true, starred: true),
    ])
    // A summary later never drops the text kept.
    await store.save(entries: [Entry(id: "1", feedId: 7, title: "Duo review", publishedAt: now)])
    #expect(await store.entry("1")?.content == "<p>The iPhone Duo folds.</p>")

    let folders: [Int64: Set<Int64>] = [1: [7]]
    #expect(await store.list(.unread, unreadOnly: true, search: nil, folders: folders).map(\.id) == ["1"])
    #expect(await store.list(.starred, unreadOnly: true, search: nil, folders: folders).map(\.id) == ["2"])
    #expect(await store.list(.all, unreadOnly: false, search: "duo folds", folders: folders).map(\.id) == ["1"])
    #expect(await store.list(.folder(1), unreadOnly: true, search: nil, folders: folders).map(\.id) == ["1"])

    await store.patch(["1"], read: true)
    // A fresh store reads the same file back.
    let again = OfflineStore(account: "isma@rss.example.org", directory: dir)
    #expect(await again.entry("1")?.read == true)
    #expect(await again.tree()?.feeds.first?.title == "DF")
    await again.clear()
    #expect(await OfflineStore(account: "isma@rss.example.org", directory: dir).tree() == nil)
}
