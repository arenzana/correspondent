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
    @Environment(\.dynamicTypeSize) private var textSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var isPhone: Bool {
        #if os(iOS)
        size == .compact
        #else
        false
        #endif
    }

    var body: some View {
        let theme = app.theme(scheme)
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
            if !isPhone, player.now != nil {
                PlayerBar().frame(maxWidth: 640).padding(.horizontal, 16).padding(.bottom, 16)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            VStack(spacing: 8) {
                ForEach(app.notices) { n in Toast(notice: n) }
            }
            .padding(.bottom, 24 + (player.now != nil && !isPhone ? 110 : 0))
        }
        // The device's text size (Dynamic Type) is read when fonts are made:
        // a new one rebuilds the views. The interface size and typeface are
        // observed instead (InterfaceType), so changing them keeps your place.
        .id(textSize)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: app.notices)
        .animation(reduceMotion ? nil : .spring(duration: 0.35), value: player.now?.id)
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
    #if os(macOS)
    @State private var keyMonitor: Any?
    #endif
    @State private var settings = Self.debugEnv("CORR_TAB") == "settings"
    @State private var showingKeys = false
    @State private var managing = Self.debugEnv("CORR_MANAGE") != nil
    /// When g was pressed: the next key picks a list (g u, g t, g s, g a).
    @State private var gAt: Date?
    @State private var columns: NavigationSplitViewVisibility = Self.debugEnv("CORR_SIDEBAR") == "hidden" ? .doubleColumn : .all

    /// Screenshots start where they need to (debug builds only).
    private static func debugEnv(_ key: String) -> String? {
        #if DEBUG
        ProcessInfo.processInfo.environment[key]
        #else
        nil
        #endif
    }

    var body: some View {
        NavigationSplitView(columnVisibility: $columns) {
            SidebarView(onSearch: { searching = true }, onSettings: { settings = true }, onCollapse: { toggleSidebar() })
                .toolbar(removing: .sidebarToggle)
                .frame(minWidth: 220)
                .navigationSplitViewColumnWidth(min: 220, ideal: PaneWidths.nav, max: 360)
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { PaneWidths.saveSoon(nav: $0) }
        } content: {
            WireView(leading: collapsed ? AnyView(collapsedLeading) : nil, footer: collapsed ? AnyView(collapsedFooter) : nil)
                .navigationSplitViewColumnWidth(min: 320, ideal: PaneWidths.list, max: 560)
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { PaneWidths.saveSoon(list: $0) }
        } detail: {
            if settings {
                SettingsView(onBack: { settings = false })
            } else {
                ArticleView()
            }
        }
        .navigationSplitViewStyle(.balanced)
        .sheet(isPresented: $searching) { SearchView().environment(\.theme, theme).environment(app).environment(player).frame(minWidth: 520, minHeight: 480) }
        .sheet(isPresented: $showingKeys) { ShortcutsView().environment(\.theme, theme) }
        .sheet(isPresented: $managing) { ManageView().environment(\.theme, theme).environment(app) }
        .onReceive(NotificationCenter.default.publisher(for: .manage)) { _ in managing = true }
        .onKeyPress { press in keys(press) }
        #if os(macOS)
        .onAppear { keyMonitor = watchKeys() }
        .onDisappear { if let keyMonitor { NSEvent.removeMonitor(keyMonitor) } }
        #endif
        .onReceive(NotificationCenter.default.publisher(for: .search)) { _ in searching = true }
        .onReceive(NotificationCenter.default.publisher(for: .focus)) { _ in toggleFocus() }
        .onReceive(NotificationCenter.default.publisher(for: .sidebar)) { _ in toggleSidebar() }
        .onReceive(NotificationCenter.default.publisher(for: .shortcuts)) { _ in showingKeys = true }
        .onReceive(NotificationCenter.default.publisher(for: .openSettings)) { _ in settings = true }
        #if os(macOS)
        .onReceive(NotificationCenter.default.publisher(for: .escape)) { _ in _ = escape() }
        #endif
        .onChange(of: app.selectedID) { _, id in if id != nil { settings = false } }
        // The sidebar as the reader left it (here or on the web).
        .onChange(of: app.prefs.sidebar, initial: true) { _, shown in
            if columns != .detailOnly { columns = shown ? .all : .doubleColumn }
        }
    }

    /// The web's keyboard shortcuts (iPad with a keyboard; the Mac uses the monitor below).
    private func keys(_ press: KeyPress) -> KeyPress.Result {
        if press.key == .escape { return escape() ? .handled : .ignored }
        return shortcut(press.characters) ? .handled : .ignored
    }

    /// One of the web's single-key shortcuts. False when the key isn't one.
    private func shortcut(_ key: String) -> Bool {
        // g, then a letter: a list.
        if let at = gAt, Date().timeIntervalSince(at) < 1.5 {
            gAt = nil
            if key == "m" { managing = true; return true }
            let to: Source? = ["u": .unread, "t": .today, "s": .starred, "a": .all][key]
            if let to { Task { await app.go(to) } }
            return to != nil
        }
        switch key {
        case "g": gAt = Date()
        case "j": Task { await app.move(1) }
        case "k": Task { await app.move(-1) }
        case "n": Task { await app.move(1, unread: true) }
        case "p": Task { await app.move(-1, unread: true) }
        case " ": page(1)
        case "a": NotificationCenter.default.post(name: .addFeed, object: nil)
        case ",": settings = true
        case "[": toggleSidebar()
        case "?": showingKeys = true
        case "s": if let id = app.selectedID { Task { await app.toggleStar(id) } }
        case "m": if let id = app.selectedID { Task { await app.toggleRead(id) } }
        case "S": Task { await app.summarize() }
        case "l": if let e = app.article { if player.now?.id == e.id { player.toggle() } else { Task { await player.play(e) } } }
        case "L": Task { await player.playList(app.entries, from: app.selectedID) }
        case "A": Task { await app.markAllRead() }
        case "r": Task { await app.refreshTree(); await app.load() }
        case "/": searching = true
        case "f": toggleFocus()
        case "v": if let u = app.article.flatMap({ URL(string: $0.url) }) { openOriginal(u) }
        default: return false
        }
        return true
    }

    /// Space: a page down the article, or the next article from its end.
    private func page(_ by: Int) {
        if app.article == nil { Task { await app.move(1) } } else { NotificationCenter.default.post(name: .page, object: by) }
    }

    #if os(macOS)
    /// On the Mac the keys work wherever the focus is (the article's text,
    /// the list, nowhere), as on the web, but never while typing, with
    /// Command, Control or Option held, or in a sheet.
    private func watchKeys() -> Any? {
        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            guard let window = event.window, window.isKeyWindow, window.sheetParent == nil, window.attachedSheet == nil,
                  event.modifierFlags.intersection([.command, .control, .option]).isEmpty else { return event }
            if let text = window.firstResponder as? NSText, text.isEditable { return event }
            if searching { return event }
            // With the spotlight on, ↓ and ↑ move it, as on the web.
            if app.article != nil, app.prefs.spotlight != "off", event.keyCode == 125 || event.keyCode == 126 {
                NotificationCenter.default.post(name: .spotlightStep, object: event.keyCode == 125 ? 1 : -1)
                return nil
            }
            // Shift-Space pages back up.
            if event.keyCode == 49, event.modifierFlags.contains(.shift) { page(-1); return nil }
            return shortcut(event.charactersIgnoringModifiers.map { event.modifierFlags.contains(.shift) ? $0.uppercased() : $0 } ?? "") ? nil : event
        }
    }
    #endif

    /// Escape, as on the web: leaves focused reading, then closes Settings
    /// or the article. False when there was nothing to close.
    private func escape() -> Bool {
        if columns == .detailOnly { toggleFocus() } else if settings { settings = false } else if app.article != nil { app.closeArticle() } else { return false }
        return true
    }

    /// The sidebar is hidden (focused reading hides everything instead).
    private var collapsed: Bool { columns == .doubleColumn }

    /// The sidebar's mark and a way back, at the head of the list.
    private var collapsedLeading: some View {
        HStack(spacing: 6) {
            Button { toggleSidebar() } label: { Image(systemName: "sidebar.left") }.iconButton()
                .accessibilityLabel("Show the sidebar")
            Mark(size: 24)
        }
    }

    /// The sidebar's toolbar, under the list.
    private var collapsedFooter: some View {
        HStack(spacing: 2) {
            Button { NotificationCenter.default.post(name: .addFeed, object: nil) } label: { Image(systemName: "plus") }.iconButton()
                .accessibilityLabel("Add a feed")
            Spacer()
            Button { settings = true } label: { Image(systemName: "slider.horizontal.3") }.iconButton()
                .accessibilityLabel("Settings")
        }
        .padding(.horizontal, 10).padding(.vertical, 8)
        .background(theme.fog)
        .overlay(alignment: .top) { Rectangle().fill(theme.rule.opacity(0.6)).frame(height: 1) }
    }

    /// Shows or hides the sidebar, remembered with the account as on the web.
    private func toggleSidebar() {
        let show = collapsed
        withAnimation(.easeOut(duration: 0.2)) { columns = show ? .all : .doubleColumn }
        app.setPrefs({ $0.sidebar = show }, ["sidebar": show])
    }

    /// Focused reading: the article alone, then back to three columns.
    private func toggleFocus() {
        withAnimation(.easeOut(duration: 0.2)) { columns = columns == .detailOnly ? .all : .detailOnly }
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
    @State private var managing = false
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
        .sheet(isPresented: $managing) { ManageView().environment(\.theme, theme).environment(app) }
        .onReceive(NotificationCenter.default.publisher(for: .manage)) { _ in managing = true }
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

/// The sidebar's and the list's widths as the reader dragged them, kept on
/// this device (a column's width belongs to the screen, as the text sizes
/// do) and used as the columns' starting widths.
@MainActor
enum PaneWidths {
    private static let d = UserDefaults.standard
    static var nav: CGFloat { saved("width.nav", 260, 220...360) }
    static var list: CGFloat { saved("width.list", 400, 320...560) }

    private static func saved(_ key: String, _ fallback: CGFloat, _ range: ClosedRange<CGFloat>) -> CGFloat {
        let w = d.double(forKey: key)
        return w > 0 ? min(max(CGFloat(w), range.lowerBound), range.upperBound) : fallback
    }

    private static var pending: [String: CGFloat] = [:]
    private static var task: Task<Void, Never>?

    /// Saves after the drag settles; a hidden column (narrower than its
    /// minimum) is never saved.
    static func saveSoon(nav: CGFloat? = nil, list: CGFloat? = nil) {
        if let nav, nav >= 220 { pending["width.nav"] = nav }
        if let list, list >= 320 { pending["width.list"] = list }
        task?.cancel()
        task = Task {
            try? await Task.sleep(for: .milliseconds(600))
            guard !Task.isCancelled else { return }
            for (k, w) in pending { d.set(Double(w.rounded()), forKey: k) }
            pending = [:]
        }
    }
}
