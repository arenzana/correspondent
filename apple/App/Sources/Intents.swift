import AppIntents
import CorrespondentKit

// Shortcuts and Siri: what's new, summaries, reading aloud. They use the
// signed-in account, like the widgets.

private func signedInClient() throws -> Client {
    guard let (account, token) = Account.load() else {
        throw IntentFailure("Sign in to Correspondent first.")
    }
    return Client(server: account.server, token: token)
}

struct IntentFailure: Error, CustomLocalizedStringResourceConvertible {
    var message: String
    init(_ message: String) { self.message = message }
    var localizedStringResource: LocalizedStringResource { LocalizedStringResource(stringLiteral: message) }
}

/// A feed, to narrow an action to one subscription.
struct FeedEntity: AppEntity {
    static let typeDisplayRepresentation: TypeDisplayRepresentation = "Feed"
    static let defaultQuery = FeedQuery()
    var id: Int
    var title: String
    var displayRepresentation: DisplayRepresentation { DisplayRepresentation(title: "\(title)") }
}

struct FeedQuery: EntityQuery, EntityStringQuery {
    func entities(for identifiers: [Int]) async throws -> [FeedEntity] {
        try await all().filter { identifiers.contains($0.id) }
    }
    func suggestedEntities() async throws -> [FeedEntity] { try await all() }
    func entities(matching string: String) async throws -> [FeedEntity] {
        try await all().filter { $0.title.localizedCaseInsensitiveContains(string) }
    }
    private func all() async throws -> [FeedEntity] {
        try await signedInClient().tree().feeds
            .sorted { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending }
            .map { FeedEntity(id: Int($0.id), title: $0.title) }
    }
}

struct WhatsNewIntent: AppIntent {
    static let title: LocalizedStringResource = "What's new"
    static let description = IntentDescription("How many articles are unread, and the newest headlines.")

    func perform() async throws -> some IntentResult & ReturnsValue<[String]> & ProvidesDialog {
        let client = try signedInClient()
        let tree = try await client.tree()
        let titles = Dictionary(uniqueKeysWithValues: tree.feeds.map { ($0.id, $0.title) })
        let latest = try await client.entries(.init(view: "unread", limit: 5)).entries
        let lines = latest.map { "\($0.title) — \(titles[$0.feedId] ?? "")" }
        let n = tree.counts.unread
        let head = n == 0 ? "You're all caught up." : "You have \(n) unread \(n == 1 ? "article" : "articles")."
        let spoken = lines.isEmpty ? head : head + " The newest: " + latest.prefix(3).map(\.title).joined(separator: "; ") + "."
        return .result(value: lines, dialog: IntentDialog(stringLiteral: spoken))
    }
}

struct SummarizeUnreadIntent: AppIntent {
    static let title: LocalizedStringResource = "Summarize unread"
    static let description = IntentDescription("Short summaries of the newest unread articles. Summaries already made are used first; the rest are written by your AI.")

    @Parameter(title: "Feed") var feed: FeedEntity?
    @Parameter(title: "How many", default: 3, inclusiveRange: (1, 10)) var count: Int

    static var parameterSummary: some ParameterSummary {
        Summary("Summarize \(\.$count) unread from \(\.$feed)")
    }

    func perform() async throws -> some IntentResult & ReturnsValue<[String]> & ProvidesDialog {
        let client = try signedInClient()
        let entries = try await client.entries(.init(view: "unread", feed: feed.map { Int64($0.id) }, limit: count)).entries
        guard !entries.isEmpty else { return .result(value: [], dialog: "Nothing unread there.") }
        var out: [String] = []
        for e in entries {
            let s = (try? await client.summarize(e.id)) ?? "(couldn't summarize)"
            out.append("\(e.title): \(s)")
        }
        return .result(value: out, dialog: IntentDialog(stringLiteral: out.joined(separator: "\n\n")))
    }
}

struct ListenIntent: AppIntent {
    static let title: LocalizedStringResource = "Read unread aloud"
    static let description = IntentDescription("Plays your unread articles that already have audio, one after another.")
    static let openAppWhenRun = true

    @MainActor
    func perform() async throws -> some IntentResult {
        IntentRouter.shared.pending = Links.listen
        return .result()
    }
}

struct OpenUnreadIntent: AppIntent {
    static let title: LocalizedStringResource = "Open unread"
    static let openAppWhenRun = true

    @MainActor
    func perform() async throws -> some IntentResult {
        IntentRouter.shared.pending = Links.unread
        return .result()
    }
}

/// Links the app opens (from intents, and widgets' URLs).
enum Links {
    static let unread = URL(string: "correspondent://unread")!
    static let listen = URL(string: "correspondent://listen")!
}

/// Hands an intent's request to the running app.
@MainActor @Observable
final class IntentRouter {
    static let shared = IntentRouter()
    var pending: URL?
}

struct Shortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(intent: WhatsNewIntent(), phrases: ["What's new in \(.applicationName)", "What's unread in \(.applicationName)"],
                    shortTitle: "What's new", systemImageName: "tray")
        AppShortcut(intent: SummarizeUnreadIntent(), phrases: ["Summarize my unread in \(.applicationName)", "Summarize \(.applicationName)"],
                    shortTitle: "Summarize unread", systemImageName: "text.alignleft")
        AppShortcut(intent: ListenIntent(), phrases: ["Read \(.applicationName) aloud", "Listen to \(.applicationName)"],
                    shortTitle: "Read aloud", systemImageName: "headphones")
        AppShortcut(intent: OpenUnreadIntent(), phrases: ["Open \(.applicationName) unread"],
                    shortTitle: "Unread", systemImageName: "tray.full")
    }
}
