import Foundation

// The API's shapes (internal/api, internal/store), decoded leniently: a
// field the server leaves out takes its zero value, so older and newer
// servers both work.

public typealias EntryID = String

public struct Entry: Codable, Identifiable, Hashable, Sendable {
    public var id: EntryID
    public var feedId: Int64
    public var url: String
    public var title: String
    public var author: String
    /// The article's HTML: only when asked for (one article, or the offline copy).
    public var content: String?
    public var excerpt: String
    public var imageUrl: String
    /// Seconds since 1970.
    public var publishedAt: Int64
    public var read: Bool
    public var starred: Bool
    public var fullText: Bool
    public var audio: Bool
    public var summary: String?
    public var sponsored: Bool
    /// Why fetching the full article failed last time, if it did.
    public var fullError: String?
    /// Audio and video attached to the article (podcasts, clips): only for one article.
    public var enclosures: [Enclosure] = []

    public var published: Date { Date(timeIntervalSince1970: TimeInterval(publishedAt)) }

    public init(id: EntryID, feedId: Int64, url: String = "", title: String, author: String = "", content: String? = nil,
                excerpt: String = "", imageUrl: String = "", publishedAt: Int64, read: Bool = false, starred: Bool = false,
                fullText: Bool = false, audio: Bool = false, summary: String? = nil, sponsored: Bool = false) {
        self.id = id
        self.feedId = feedId
        self.url = url
        self.title = title
        self.author = author
        self.content = content
        self.excerpt = excerpt
        self.imageUrl = imageUrl
        self.publishedAt = publishedAt
        self.read = read
        self.starred = starred
        self.fullText = fullText
        self.audio = audio
        self.summary = summary
        self.sponsored = sponsored
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(EntryID.self, forKey: .id)
        feedId = try c.decodeIfPresent(Int64.self, forKey: .feedId) ?? 0
        url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        author = try c.decodeIfPresent(String.self, forKey: .author) ?? ""
        content = try c.decodeIfPresent(String.self, forKey: .content)
        excerpt = try c.decodeIfPresent(String.self, forKey: .excerpt) ?? ""
        imageUrl = try c.decodeIfPresent(String.self, forKey: .imageUrl) ?? ""
        publishedAt = try c.decodeIfPresent(Int64.self, forKey: .publishedAt) ?? 0
        read = try c.decodeIfPresent(Bool.self, forKey: .read) ?? false
        starred = try c.decodeIfPresent(Bool.self, forKey: .starred) ?? false
        fullText = try c.decodeIfPresent(Bool.self, forKey: .fullText) ?? false
        audio = try c.decodeIfPresent(Bool.self, forKey: .audio) ?? false
        summary = try c.decodeIfPresent(String.self, forKey: .summary)
        sponsored = try c.decodeIfPresent(Bool.self, forKey: .sponsored) ?? false
        fullError = try c.decodeIfPresent(String.self, forKey: .fullError)
        enclosures = (try? c.decodeIfPresent([Enclosure].self, forKey: .enclosures)) ?? []
    }
}

/// A file attached to an article.
public struct Enclosure: Codable, Hashable, Sendable {
    public var url: String
    public var type: String
}

public struct Feed: Codable, Identifiable, Hashable, Sendable {
    public var id: Int64
    public var folderId: Int64?
    public var url: String
    public var siteUrl: String
    public var title: String
    public var iconUrl: String
    public var unread: Int
    public var paused: Bool
    public var errorCount: Int
    /// New articles are fetched in full from their pages.
    public var fullText: Bool
    /// A site with no feed, read from its page.
    public var page: Bool
    /// New articles notify the reader's devices.
    public var notify: Bool
    public var lastError: String
    /// Articles per day over the last fourteen days, oldest first.
    public var cadence: [Int] = []

    public init(id: Int64, folderId: Int64? = nil, url: String = "", siteUrl: String = "", title: String,
                iconUrl: String = "", unread: Int = 0, paused: Bool = false, errorCount: Int = 0,
                fullText: Bool = false, page: Bool = false, lastError: String = "") {
        self.id = id
        self.folderId = folderId
        self.url = url
        self.siteUrl = siteUrl
        self.title = title
        self.iconUrl = iconUrl
        self.unread = unread
        self.paused = paused
        self.errorCount = errorCount
        self.fullText = fullText
        self.page = page
        self.notify = false
        self.lastError = lastError
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(Int64.self, forKey: .id)
        folderId = try c.decodeIfPresent(Int64.self, forKey: .folderId)
        url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
        siteUrl = try c.decodeIfPresent(String.self, forKey: .siteUrl) ?? ""
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        iconUrl = try c.decodeIfPresent(String.self, forKey: .iconUrl) ?? ""
        unread = try c.decodeIfPresent(Int.self, forKey: .unread) ?? 0
        paused = try c.decodeIfPresent(Bool.self, forKey: .paused) ?? false
        errorCount = try c.decodeIfPresent(Int.self, forKey: .errorCount) ?? 0
        fullText = try c.decodeIfPresent(Bool.self, forKey: .fullText) ?? false
        page = try c.decodeIfPresent(Bool.self, forKey: .page) ?? false
        notify = try c.decodeIfPresent(Bool.self, forKey: .notify) ?? false
        lastError = try c.decodeIfPresent(String.self, forKey: .lastError) ?? ""
        cadence = (try? c.decodeIfPresent([Int].self, forKey: .cadence)) ?? []
    }
}

public struct Folder: Codable, Identifiable, Hashable, Sendable {
    public var id: Int64
    public var name: String
    public var unread: Int
    public init(id: Int64, name: String, unread: Int = 0) {
        self.id = id
        self.name = name
        self.unread = unread
    }
}

public struct SavedSearch: Codable, Identifiable, Hashable, Sendable {
    public var id: Int64
    public var name: String
    public var query: String
    public var unread: Int
}

/// A feed (or a page read without one) found at an address.
public struct Candidate: Codable, Hashable, Sendable, Identifiable {
    public var url: String
    public var title: String
    public var description: String
    public var siteUrl: String
    public var iconUrl: String
    public var itemCount: Int
    public var page: Bool
    /// The newest article's titles, for a page read without a feed.
    public var sample: [String]
    public var id: String { url }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        url = try c.decode(String.self, forKey: .url)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        description = try c.decodeIfPresent(String.self, forKey: .description) ?? ""
        siteUrl = try c.decodeIfPresent(String.self, forKey: .siteUrl) ?? ""
        iconUrl = try c.decodeIfPresent(String.self, forKey: .iconUrl) ?? ""
        itemCount = try c.decodeIfPresent(Int.self, forKey: .itemCount) ?? 0
        page = try c.decodeIfPresent(Bool.self, forKey: .page) ?? false
        sample = try c.decodeIfPresent([String].self, forKey: .sample) ?? []
    }
}

public struct Counts: Codable, Hashable, Sendable {
    public var unread: Int
    public var starred: Int
    public var today: Int
    /// The newest article's ID: the boundary for mark all read.
    public var newest: String
    public init(unread: Int = 0, starred: Int = 0, today: Int = 0, newest: String = "0") {
        self.unread = unread
        self.starred = starred
        self.today = today
        self.newest = newest
    }
}

public struct Tree: Codable, Hashable, Sendable {
    public var folders: [Folder]
    public var feeds: [Feed]
    public var counts: Counts
    public var searches: [SavedSearch]

    public init(folders: [Folder] = [], feeds: [Feed] = [], counts: Counts = Counts(), searches: [SavedSearch] = []) {
        self.folders = folders
        self.feeds = feeds
        self.counts = counts
        self.searches = searches
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        folders = try c.decodeIfPresent([Folder].self, forKey: .folders) ?? []
        feeds = try c.decodeIfPresent([Feed].self, forKey: .feeds) ?? []
        counts = try c.decodeIfPresent(Counts.self, forKey: .counts) ?? Counts()
        searches = try c.decodeIfPresent([SavedSearch].self, forKey: .searches) ?? []
    }
}

public struct Page: Codable, Sendable {
    public var entries: [Entry]
    public var cursor: String
}

/// What a list shows, as the web app's sources.
public enum Source: Hashable, Codable, Sendable {
    case unread, today, starred, all
    case feed(Int64)
    case folder(Int64)
    case search(Int64)
}

/// The reader's preferences the apps follow (the web app saves them).
public struct Prefs: Codable, Hashable, Sendable {
    /// auto, sun, day, night or evening.
    public var theme: String
    public var unreadOnly: Bool
    public var markOnOpen: Bool
    /// Paid posts arrive already read (the server reads this too).
    public var skipSponsored: Bool
    /// Article text size in points, as the web's pixels.
    public var readSize: Double
    /// Line length in characters.
    public var measure: Double
    /// Answered the question on a sponsored post with "keep showing them".
    public var sponsoredAsked = false
    /// Interface text size: 0.92, 1, 1.1 or 1.2, as the web's.
    public var uiScale: Double = 1
    /// Bold the start of each word, for the eye to jump from word to word.
    public var bionic = false
    /// off, paragraph or line: one paragraph or line clear, the rest dimmed.
    public var spotlight = "off"
    /// serif (Newsreader) or sans (Schibsted Grotesk).
    public var readFont: String
    /// wire, byline or morse: the app's icon, as the web app's.
    public var appIcon: String
    /// Coarse position (about 11 km) for the Sun theme, saved by the web app.
    public var place: Place?

    public struct Place: Codable, Hashable, Sendable {
        public var lat: Double
        public var lon: Double
        public var estimated: Bool
        public init(lat: Double, lon: Double, estimated: Bool) {
            self.lat = lat
            self.lon = lon
            self.estimated = estimated
        }
    }

    public init(theme: String = "auto", unreadOnly: Bool = true, markOnOpen: Bool = true, skipSponsored: Bool = false,
                readSize: Double = 19, measure: Double = 68, readFont: String = "serif", appIcon: String = "wire") {
        self.theme = theme
        self.unreadOnly = unreadOnly
        self.markOnOpen = markOnOpen
        self.skipSponsored = skipSponsored
        self.readSize = readSize
        self.measure = measure
        self.readFont = readFont
        self.appIcon = appIcon
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        theme = try c.decodeIfPresent(String.self, forKey: .theme) ?? "auto"
        unreadOnly = try c.decodeIfPresent(Bool.self, forKey: .unreadOnly) ?? true
        markOnOpen = try c.decodeIfPresent(Bool.self, forKey: .markOnOpen) ?? true
        skipSponsored = try c.decodeIfPresent(Bool.self, forKey: .skipSponsored) ?? false
        readSize = try c.decodeIfPresent(Double.self, forKey: .readSize) ?? 19
        measure = try c.decodeIfPresent(Double.self, forKey: .measure) ?? 68
        readFont = try c.decodeIfPresent(String.self, forKey: .readFont) ?? "serif"
        appIcon = try c.decodeIfPresent(String.self, forKey: .appIcon) ?? "wire"
        place = try? c.decodeIfPresent(Place.self, forKey: .place)
        uiScale = (try? c.decodeIfPresent(Double.self, forKey: .uiScale)) ?? 1
        bionic = (try? c.decodeIfPresent(Bool.self, forKey: .bionic)) ?? false
        sponsoredAsked = (try? c.decodeIfPresent(Bool.self, forKey: .sponsoredAsked)) ?? false
        spotlight = (try? c.decodeIfPresent(String.self, forKey: .spotlight)) ?? "off"
    }
}

public struct Me: Codable, Sendable {
    public var username: String
    public var prefs: Prefs?
    /// The reader's AI and text-to-speech setups (only whether they exist matters here).
    public var ai: Setup?
    public var speech: Setup?
    public var aiAvailable: Bool?

    public struct Setup: Codable, Sendable {
        public var provider: String?
        public var keyHint: String?
    }
}
