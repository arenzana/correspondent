import Foundation

#if os(macOS)
import SwiftUI
import CorrespondentKit

/// The Mac's menus, with shortcuts (Mail's where they exist). The web's
/// single-letter keys (j, k, s, m…) still work in the window.
struct MacCommands: Commands {
    var app: AppModel
    var player: Player
    @Environment(\.openURL) private var openURL

    var body: some Commands {
        // One window: a second would run the same lists and commands twice.
        CommandGroup(replacing: .newItem) {}
        CommandGroup(after: .newItem) {
            Button("Add a Feed…") { NotificationCenter.default.post(name: .addFeed, object: nil) }
                .keyboardShortcut("n", modifiers: [.command, .shift])
            Button("Refresh") { Task { await app.refreshTree(); await app.load() } }
                .keyboardShortcut("r")
            Button("Manage Subscriptions…") { NotificationCenter.default.post(name: .manage, object: nil) }
                .keyboardShortcut("m", modifiers: [.command, .shift])
        }
        CommandGroup(replacing: .sidebar) {
            Button("Show or Hide the Sidebar") { NotificationCenter.default.post(name: .sidebar, object: nil) }
                .keyboardShortcut("s", modifiers: [.control, .command])
        }
        CommandGroup(replacing: .help) {
            Button("Keyboard Shortcuts") { NotificationCenter.default.post(name: .shortcuts, object: nil) }
                .keyboardShortcut("/", modifiers: [.command, .shift])
        }
        CommandMenu("Go") {
            Button("Unread") { Task { await app.go(.unread) } }.keyboardShortcut("1")
            Button("Today") { Task { await app.go(.today) } }.keyboardShortcut("2")
            Button("Starred") { Task { await app.go(.starred) } }.keyboardShortcut("3")
            Button("All Articles") { Task { await app.go(.all) } }.keyboardShortcut("4")
            Divider()
            Button("Search…") { NotificationCenter.default.post(name: .search, object: nil) }.keyboardShortcut("f")
            Button("Search or Jump To…") { NotificationCenter.default.post(name: .search, object: nil) }.keyboardShortcut("k")
        }
        CommandMenu("Article") {
            Button("Next Article") { Task { await app.move(1) } }.keyboardShortcut(.downArrow)
            Button("Previous Article") { Task { await app.move(-1) } }.keyboardShortcut(.upArrow)
            // A menu item, so Escape works with the article's text focused too.
            Button("Close Article") { NotificationCenter.default.post(name: .escape, object: nil) }
                .keyboardShortcut(.escape, modifiers: [])
            Divider()
            Button(app.article?.read == false ? "Mark as Read" : "Mark as Unread") {
                if let id = app.article?.id { Task { await app.toggleRead(id) } }
            }
            .keyboardShortcut("u", modifiers: [.command, .shift]).disabled(app.article == nil)
            Button(app.article?.starred == true ? "Unstar" : "Star") {
                if let id = app.article?.id { Task { await app.toggleStar(id) } }
            }
            .keyboardShortcut("l", modifiers: [.command, .shift]).disabled(app.article == nil)
            Button(app.summaryOpen ? "Hide Summary" : "Summary") { Task { await app.summarize() } }
                .keyboardShortcut("s", modifiers: [.command, .shift]).disabled(app.article == nil)
            Button(player.now?.id == app.article?.id && player.playing ? "Pause Reading Aloud" : "Read Aloud") {
                guard let e = app.article else { return }
                if player.now?.id == e.id { player.toggle() } else { Task { await player.play(e) } }
            }
            .keyboardShortcut("l", modifiers: [.command, .option]).disabled(app.article == nil)
            Button("Load the Full Article") { Task { await app.loadFullArticle() } }
                .disabled(app.article == nil || app.article?.fullText == true)
            Button("Open Original") {
                if let u = app.article.flatMap({ URL(string: $0.url) }) { openURL(u) }
            }
            .keyboardShortcut(.return).disabled(app.article?.url.isEmpty ?? true)
            Divider()
            Button("Mark All as Read") { Task { await app.markAllRead() } }
                .keyboardShortcut("k", modifiers: [.command, .option])
        }
    }
}

/// Settings in its own window (⌘,), themed like the app.
struct SettingsWindow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        let theme = app.theme(scheme)
        Group {
            if app.account == nil {
                Text("Sign in to Correspondent first.").font(Typeface.ui.font(15)).foregroundStyle(theme.slate)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                SettingsView()
            }
        }
        .environment(\.theme, theme)
        .tint(theme.accent)
        .background(theme.sheet)
    }
}
#endif

extension Notification.Name {
    static let addFeed = Notification.Name("org.arenzana.correspondent.addFeed")
    static let search = Notification.Name("org.arenzana.correspondent.search")
    static let escape = Notification.Name("org.arenzana.correspondent.escape")
    static let sidebar = Notification.Name("org.arenzana.correspondent.sidebar")
    /// Pages the article: the object is 1 (down) or -1 (up).
    static let page = Notification.Name("org.arenzana.correspondent.page")
    static let shortcuts = Notification.Name("org.arenzana.correspondent.shortcuts")
    static let openSettings = Notification.Name("org.arenzana.correspondent.openSettings")
    static let manage = Notification.Name("org.arenzana.correspondent.manage")
    /// Moves the article's spotlight: the object is 1 (down) or -1 (up).
    static let spotlightStep = Notification.Name("org.arenzana.correspondent.spotlightStep")
}
