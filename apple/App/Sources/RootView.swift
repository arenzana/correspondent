import SwiftUI
import CorrespondentKit

/// Chooses the layout, as the web does by width: on iPad and Mac three
/// columns (feeds, list, article); on iPhone one pane at a time with the
/// web's tab bar (Feeds, Unread, Starred, Search, Settings).
struct RootView: View {
    @Environment(AppModel.self) private var app
    @Environment(Player.self) private var player
    @Environment(\.colorScheme) private var scheme
    @Environment(\.horizontalSizeClass) private var size

    private var isPhone: Bool {
        #if os(iOS)
        size == .compact
        #else
        false
        #endif
    }

    var body: some View {
        let theme = Theme.resolve(app.prefs.theme, scheme: scheme)
        ZStack(alignment: .bottom) {
            if app.booting {
                theme.fog.ignoresSafeArea()
            } else if app.account == nil {
                SignInView()
            } else if isPhone {
                #if os(iOS)
                PhoneLayout()
                #endif
            } else {
                WideLayout()
            }
            if !isPhone {
                PlayerBar().frame(maxWidth: 640).padding(.horizontal, 16).padding(.bottom, 16)
            }
            if let t = app.toast { Toast(message: t).padding(.bottom, player.now != nil && !isPhone ? 110 : 0) }
        }
        .animation(.easeOut(duration: 0.2), value: app.toast)
        .environment(\.theme, theme)
        .tint(theme.accent)
        .preferredColorScheme(theme.dark ? .dark : .light)
    }
}

/// iPad and Mac.
struct WideLayout: View {
    @Environment(AppModel.self) private var app
    @Environment(Player.self) private var player
    @Environment(\.theme) private var theme
    @State private var searching = false
    @State private var settings = false
    @State private var columns = NavigationSplitViewVisibility.all

    var body: some View {
        NavigationSplitView(columnVisibility: $columns) {
            SidebarView(onSearch: { searching = true }, onSettings: { settings = true })
                .toolbar(removing: .sidebarToggle)
                .frame(minWidth: 220)
                .navigationSplitViewColumnWidth(min: 220, ideal: 260, max: 360)
        } content: {
            WireView()
                .navigationSplitViewColumnWidth(min: 320, ideal: 400, max: 560)
        } detail: {
            if settings {
                SettingsView(onBack: { settings = false })
            } else {
                ArticleView()
            }
        }
        .navigationSplitViewStyle(.balanced)
        .sheet(isPresented: $searching) { SearchView().environment(\.theme, theme).frame(minWidth: 520, minHeight: 480) }
        .onKeyPress { press in keys(press) }
        .onChange(of: app.selectedID) { _, id in if id != nil { settings = false } }
    }

    /// The web's keyboard shortcuts.
    private func keys(_ press: KeyPress) -> KeyPress.Result {
        switch press.characters {
        case "j": Task { await app.move(1) }
        case "k": Task { await app.move(-1) }
        case "s": if let id = app.selectedID { Task { await app.toggleStar(id) } }
        case "m": if let id = app.selectedID { Task { await app.toggleRead(id) } }
        case "S": Task { await app.summarize() }
        case "l": if let e = app.article { if player.now?.id == e.id { player.toggle() } else { Task { await player.play(e) } } }
        case "L": Task { await player.playList(app.entries, from: app.selectedID) }
        case "A": Task { await app.markAllRead() }
        case "r": Task { await app.refreshTree(); await app.load() }
        case "/": searching = true
        case "v": if let u = app.article.flatMap({ URL(string: $0.url) }) { openOriginal(u) }
        default:
            if press.key == .escape, app.article != nil { app.closeArticle(); return .handled }
            return .ignored
        }
        return .handled
    }

    private func openOriginal(_ url: URL) {
        #if os(iOS)
        UIApplication.shared.open(url)
        #else
        NSWorkspace.shared.open(url)
        #endif
    }
}

#if os(iOS)
/// iPhone: iOS's own tab bar (Feeds, Unread, Starred, Settings, Search),
/// each tab a navigation stack, so articles push with the native back
/// button and swipe. Lists keep the web's header and timeline.
struct PhoneLayout: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    enum TabID: Hashable { case feeds, unread, starred, settings, search }
    enum Screen: Hashable { case list, article }
    @State private var tab: TabID = {
        #if DEBUG
        if ProcessInfo.processInfo.environment["CORR_TAB"] == "settings" { return .settings }
        #endif
        return .unread
    }()
    @State private var feedsPath: [Screen] = []
    @State private var unreadPath: [Screen] = {
        #if DEBUG
        if ProcessInfo.processInfo.environment["CORR_OPEN_FIRST"] != nil { return [.article] }
        #endif
        return []
    }()
    @State private var starredPath: [Screen] = []
    @State private var searchPath: [Screen] = []

    var body: some View {
        TabView(selection: $tab) {
            Tab("Feeds", systemImage: "line.3.horizontal", value: TabID.feeds) {
                NavigationStack(path: $feedsPath) {
                    SidebarView(onSearch: { tab = .search }, onSettings: { tab = .settings }, onChoose: { feedsPath = [.list] })
                        .toolbar(.hidden, for: .navigationBar)
                        .navigationDestination(for: Screen.self) { destination($0, path: $feedsPath) }
                }
            }
            Tab("Unread", systemImage: "tray", value: TabID.unread) { listStack($unreadPath) }
                .badge(app.tree.counts.unread)
            Tab("Starred", systemImage: "star", value: TabID.starred) { listStack($starredPath) }
            Tab("Settings", systemImage: "slider.horizontal.3", value: TabID.settings) { SettingsView() }
            Tab(value: TabID.search, role: .search) {
                NavigationStack(path: $searchPath) {
                    SearchView(onOpen: { _ in searchPath = [.article] }, onList: { searchPath = [.list] }, inTab: true)
                        .toolbar(.hidden, for: .navigationBar)
                        .navigationDestination(for: Screen.self) { destination($0, path: $searchPath) }
                }
            }
        }
        .tint(theme.accent)
        .onChange(of: app.openRequest) { _, id in
            // A widget opened an article: show it in Unread.
            guard id != nil else { return }
            tab = .unread
            unreadPath = [.article]
            app.openRequest = nil
        }
        .modifier(MinimizeTabBarOnScroll())
        .modifier(PlayerAccessory())
        .onChange(of: tab) { _, now in
            switch now {
            case .unread: Task { await app.go(.unread) }
            case .starred: Task { await app.go(.starred) }
            default: break
            }
        }
    }

    private func listStack(_ path: Binding<[Screen]>) -> some View {
        NavigationStack(path: path) {
            WireView(onOpen: { _ in path.wrappedValue = [.article] })
                .toolbar(.hidden, for: .navigationBar)
                .navigationDestination(for: Screen.self) { destination($0, path: path) }
        }
        .onChange(of: path.wrappedValue) { _, now in if !now.contains(.article) { app.closeArticle() } }
    }

    @ViewBuilder
    private func destination(_ screen: Screen, path: Binding<[Screen]>) -> some View {
        switch screen {
        case .list:
            WireView(onOpen: { _ in path.wrappedValue.append(.article) }, pushed: true)
                .navigationTitle(app.title)
                .navigationBarTitleDisplayMode(.inline)
        case .article:
            ArticleView(native: true)
                .navigationBarTitleDisplayMode(.inline)
        }
    }
}
/// The player above the tab bar, as Music and Podcasts show theirs (iOS 26);
/// before that, at the bottom of the screen.
struct PlayerAccessory: ViewModifier {
    @Environment(Player.self) private var player
    func body(content: Content) -> some View {
        if #available(iOS 26.1, *) {
            content.tabViewBottomAccessory(isEnabled: player.now != nil) { PlayerBar(compact: true) }
        } else {
            content.safeAreaInset(edge: .bottom) {
                PlayerBar().padding(.horizontal, 12).padding(.bottom, 60)
            }
        }
    }
}

/// Scrolling down a list or an article shrinks the tab bar (iOS 26), as
/// the web app tucks its bars away on phones.
struct MinimizeTabBarOnScroll: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 26, *) {
            content.tabBarMinimizeBehavior(.onScrollDown)
        } else {
            content
        }
    }
}
#endif
