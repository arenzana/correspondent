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
        if status == 0 { // a dropped connection
            client?.urlProtocol(self, didFailWithError: URLError(.notConnectedToInternet))
            return
        }
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
        // Only the keys changed go to the server, as a patch.
        FakeServer.routes = ["PATCH /api/v1/prefs": (204, "")]
        FakeServer.seen = []
        let c = Client(server: server, token: "t", session: FakeServer.session())
        try await c.savePrefs(["theme": "evening"])
        let patch = try #require(FakeServer.seen.last)
        #expect(patch.httpMethod == "PATCH")
        #expect((try JSONSerialization.jsonObject(with: patch.httpBody ?? Data()) as? [String: Any])?.keys.sorted() == ["theme"])

        // An older server: read, change these keys, write the whole document back.
        FakeServer.routes = [
            "GET /api/v1/me": (200, #"{"username":"isma","prefs":{"theme":"night","navWidth":280,"place":{"lat":40.4,"lon":-3.7}}}"#),
            "PUT /api/v1/prefs": (204, ""),
        ]
        try await c.savePrefs(["theme": "evening"])
        let put = try #require(FakeServer.seen.last)
        let sent = try JSONSerialization.jsonObject(with: put.httpBody ?? Data()) as? [String: Any]
        #expect(put.httpMethod == "PUT" && sent?["navWidth"] as? Int == 280 && sent?["place"] != nil && sent?["theme"] as? String == "evening")
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

/// The raw stream, as the server sends it: events end at a blank line,
/// which the reader must keep (bytes.lines drops it), CRLF or LF alike.
@Test func readsLiveEventsFromBytes() async throws {
    let raw = ": ok\n\nevent: state\ndata: {\"action\":\"star\",\"ids\":[\"9\"]}\n\nevent: refresh\r\ndata: {}\r\n\r\n"
    let bytes = AsyncStream<UInt8> { c in
        for b in raw.utf8 { c.yield(b) }
        c.finish()
    }
    var p = EventParser()
    var got: [LiveEvent] = []
    for try await line in EventParser.lines(bytes) {
        if let e = p.feed(line) { got.append(e) }
        if got.count == 2 { break } // both arrive while the stream is still open
    }
    #expect(got == [LiveEvent(name: "state", action: "star", ids: ["9"]), LiveEvent(name: "refresh", action: "", ids: [])])
}

/// Arrivals go into the list by its order; rows read here, which the
/// unread page leaves out, stay where they were.
@Test func placesArrivalsInTheList() {
    func e(_ id: Int64, read: Bool = false) -> Entry { Entry(id: String(id), feedId: 1, title: "T\(id)", publishedAt: 1_790_000_000 + id, read: read) }
    let list = [e(3, read: true), e(2, read: true), e(1)]
    #expect(placeArrivals(list, page: [e(5), e(1)], added: [e(5)]).map(\.id) == ["5", "3", "2", "1"])
    #expect(placeArrivals([e(9), e(6)], page: [e(9), e(7), e(6)], added: [e(7)]).map(\.id) == ["9", "7", "6"])
    #expect(placeArrivals([e(2)], page: [e(2, read: true)], added: []).first?.read == true) // refreshed
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
    // Writes wait a moment, so a burst is one write; flush writes now.
    #expect(await OfflineStore(account: "isma@rss.example.org", directory: dir).entry("1") == nil)
    await store.flush()
    // A fresh store reads the same file back.
    let again = OfflineStore(account: "isma@rss.example.org", directory: dir)
    #expect(await again.entry("1")?.read == true)
    #expect(await again.tree()?.feeds.first?.title == "DF")
    await again.clear()
    #expect(await OfflineStore(account: "isma@rss.example.org", directory: dir).tree() == nil)
}

extension ClientTests {
    @Test func managesSubscriptions() async throws {
        FakeServer.routes = [
            "POST /api/v1/discover": (200, #"{"candidates":[{"url":"https://sixcolors.com/feed/","title":"Six Colors","itemCount":20}]}"#),
            "POST /api/v1/feeds": (200, #"{"id":42}"#),
            "PATCH /api/v1/feeds/42": (204, ""),
            "DELETE /api/v1/feeds/42": (204, ""),
            "POST /api/v1/searches": (200, #"{"search":{"id":3,"name":"duo","query":"duo","unread":2}}"#),
        ]
        FakeServer.seen = []
        let c = Client(server: server, token: "t", session: FakeServer.session())
        let found = try await c.discover("sixcolors.com")
        #expect(found.first?.title == "Six Colors" && found.first?.page == false)
        #expect(try await c.subscribe(found[0], folder: "Apple") == 42)
        let sub = try JSONSerialization.jsonObject(with: FakeServer.seen[1].httpBody ?? Data()) as? [String: Any]
        #expect(sub?["folderName"] as? String == "Apple" && sub?["page"] as? Bool == false)
        try await c.updateFeed(42, .init(title: "6C", paused: true))
        let patch = try JSONSerialization.jsonObject(with: FakeServer.seen[2].httpBody ?? Data()) as? [String: Any]
        #expect(patch?["title"] as? String == "6C" && patch?["paused"] as? Bool == true && patch?["folderName"] == nil)
        try await c.unsubscribe(42)
        #expect(try await c.saveSearch("duo").unread == 2)
    }
}

// The outbox shares the fake server: same serialized suite as the client's.
extension ClientTests {
    @Test func mergesABurstAndKeepsMarkAllInPlace() {
        let steps = Outbox.plan([
            .state(action: "read", ids: ["1", "2"]),
            .state(action: "unread", ids: ["2"]),
            .state(action: "star", ids: ["1"]),
            .markAll(feed: 3, folder: nil, view: nil, search: nil, before: "9"),
            .state(action: "unstar", ids: ["1"]),
        ])
        #expect(steps == [
            .state(action: "read", ids: ["1"]),
            .state(action: "unread", ids: ["2"]),
            .state(action: "star", ids: ["1"]),
            .markAll(feed: 3, folder: nil, view: nil, search: nil, before: "9"),
            .state(action: "unstar", ids: ["1"]),
        ])
    }

    @Test func sendsInOrderKeepsWhenOfflineAndDropsWhatTheServerRefuses() async throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        let box = Outbox(account: "isma@x", directory: dir)
        let c = Client(server: server, token: "t", session: FakeServer.session())
        await box.enqueue(.state(action: "star", ids: ["1"]))
        await box.enqueue(.markAll(feed: nil, folder: nil, view: nil, search: "duo", before: "99"))

        FakeServer.routes = ["POST /api/v1/entries/state": (0, ""), "POST /api/v1/entries/mark-all-read": (0, "")]
        #expect(await box.flush(c).stopped == "offline")
        // Kept, and still there for a new app launch.
        #expect(await Outbox(account: "isma@x", directory: dir).count == 2)

        FakeServer.routes = [
            "POST /api/v1/entries/state": (404, #"{"error":"No such article."}"#),
            "GET /api/v1/tree": (200, #"{"counts":{"newest":"99"}}"#),
            "POST /api/v1/entries/mark-all-read": (200, #"{"ids":["5"]}"#),
        ]
        FakeServer.seen = []
        #expect(await box.flush(c) == FlushResult(sent: 2, left: 0))
        #expect(await box.count == 0)
        let mark = try #require(FakeServer.seen.last)
        let body = try JSONSerialization.jsonObject(with: mark.httpBody ?? Data()) as? [String: String]
        #expect(body?["q"] == "duo" && body?["before"] == "99")

        await box.enqueue(.state(action: "read", ids: ["7"]))
        FakeServer.routes = ["POST /api/v1/entries/state": (401, #"{"error":"Sign in."}"#)]
        #expect(await box.flush(c).stopped == "signedOut")
        #expect(await box.count == 1)
    }
}

// The Sun theme, against the same NOAA reference values as the web's sun.test.ts.
@Suite struct SunTests {
    func date(_ s: String) -> Date { ISO8601DateFormatter().date(from: s)! }
    func minutesApart(_ a: Date?, _ b: String) -> Double { abs((a?.timeIntervalSince(date(b)) ?? .infinity) / 60) }
    let madrid = (lat: 40.4168, lon: -3.7038)

    @Test func matchesNOAASunriseAndSunset() {
        let t = Sun.times(date("2026-06-21T12:00:00Z"), lat: madrid.lat, lon: madrid.lon)
        #expect(minutesApart(t.sunrise, "2026-06-21T04:44:00Z") < 4)
        #expect(minutesApart(t.sunset, "2026-06-21T19:48:00Z") < 4)
        let ny = Sun.times(date("2026-12-21T12:00:00Z"), lat: 40.7128, lon: -74.006)
        #expect(minutesApart(ny.sunrise, "2026-12-21T12:16:00Z") < 4)
        #expect(minutesApart(ny.sunset, "2026-12-21T21:32:00Z") < 4)
        #expect(Sun.times(date("2026-12-21T12:00:00Z"), lat: 69.65, lon: 18.96).polar == .night)
        #expect(Sun.times(date("2026-06-21T12:00:00Z"), lat: 69.65, lon: 18.96).polar == .day)
    }

    @Test func movesThroughDayEveningAndNight() {
        // Madrid, 21 June 2026: sunrise about 04:44 UTC, sunset about 19:48 UTC.
        let noon = Sun.theme(date("2026-06-21T12:00:00Z"), lat: madrid.lat, lon: madrid.lon)
        #expect(noon.phase == .day && minutesApart(noon.next, "2026-06-21T18:48:00Z") < 4)
        let before = Sun.theme(date("2026-06-21T19:00:00Z"), lat: madrid.lat, lon: madrid.lon)
        #expect(before.phase == .evening && minutesApart(before.next, "2026-06-21T20:33:00Z") < 4)
        #expect(Sun.theme(date("2026-06-21T20:20:00Z"), lat: madrid.lat, lon: madrid.lon).phase == .evening)
        let night = Sun.theme(date("2026-06-21T22:00:00Z"), lat: madrid.lat, lon: madrid.lon)
        #expect(night.phase == .night && minutesApart(night.next, "2026-06-22T04:44:00Z") < 5)
        let early = Sun.theme(date("2026-06-22T03:00:00Z"), lat: madrid.lat, lon: madrid.lon)
        #expect(early.phase == .night && minutesApart(early.next, "2026-06-22T04:44:00Z") < 5)
    }

    /// The local day, not the UTC one: San Francisco's sunset falls on the
    /// next UTC day, Tokyo's sunrise on the day before.
    @Test func followsTheLocalDay() {
        let sf = Sun.theme(date("2026-06-22T00:30:00Z"), lat: 37.77, lon: -122.42) // 17:30 local
        #expect(sf.phase == .day && minutesApart(sf.next, "2026-06-22T02:35:00Z") < 5)
        #expect(Sun.theme(date("2026-06-22T02:50:00Z"), lat: 37.77, lon: -122.42).phase == .evening)
        let start = date("2026-06-20T21:00:00Z") // 06:00 in Tokyo
        let tokyo = Sun.theme(start, lat: 35.68, lon: 139.69)
        #expect(tokyo.phase == .day && minutesApart(tokyo.next, "2026-06-21T09:00:00Z") < 5)
        for h in 0..<48 {
            let t = start.addingTimeInterval(Double(h) * 3600)
            for lon in [-122.42, -3.7, 139.69, 179] { #expect(Sun.theme(t, lat: 35, lon: lon).next > t) }
        }
    }

    @Test func handlesPolarSeasonsAndRounding() {
        #expect(Sun.theme(date("2026-06-21T23:00:00Z"), lat: 69.65, lon: 18.96).phase == .day)
        let polar = Sun.theme(date("2026-12-21T12:00:00Z"), lat: 69.65, lon: 18.96)
        #expect(polar.phase == .night && polar.next > date("2026-12-21T12:00:00Z"))
        #expect(Sun.coarse(40.41678) == 40.4 && Sun.coarse(-3.70379) == -3.7)
        #expect(Sun.estimate(zone: TimeZone(secondsFromGMT: 3600)!).lon == 15)
    }
}

// The same cases as the web's partial.test.ts.
@Suite struct PartialTests {
    func words(_ n: Int) -> String { (0..<n).map { "word\($0)" }.joined(separator: " ") }

    @Test func findsSummaries() {
        #expect(Partial.looksPartial(""))
        #expect(Partial.looksPartial("<p>A short teaser.</p>"))
        #expect(Partial.looksPartial("<p>\(words(150))…</p>"))
        #expect(Partial.looksPartial("<p>\(words(150)) [...]</p>"))
        #expect(Partial.looksPartial("<p>\(words(150))</p><p><a href=\"x\">Continue reading →</a></p>"))
        #expect(Partial.looksPartial("<p>\(words(150))</p><p>The post Hello appeared first on Example.</p>"))
        #expect(Partial.looksPartial("<p>\(words(500))</p><p><a href=\"x\">Continue reading →</a></p>"))
    }

    @Test func leavesCompleteArticlesAlone() {
        #expect(!Partial.looksPartial("<p>\(words(60)) you can read more about it here \(words(150)).</p>"))
        #expect(!Partial.looksPartial("<p>\(words(150)).</p>"))
        #expect(!Partial.looksPartial("<p>\(words(600))…</p>"))
    }
}
