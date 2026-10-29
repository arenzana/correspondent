import SwiftUI

@main
struct CorrespondentApp: App {
    @State private var app = AppModel()
    @State private var player = Player()
    #if os(iOS)
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    #else
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    #endif
    @Environment(\.scenePhase) private var phase

    init() {
        Typeface.register()
    }

    /// Links from widgets and Shortcuts: unread, an article, or listen.
    @MainActor
    private func handle(_ url: URL) async {
        // A tap on a notification can start the app: wait until it's signed in.
        while app.booting { try? await Task.sleep(for: .milliseconds(100)) }
        guard app.account != nil else { return }
        if url.host() == "listen" {
            await app.go(.unread)
            await player.playList(app.entries, from: nil)
        } else {
            await app.open(url)
        }
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(app)
                .environment(player)
                .onAppear {
                    player.app = app
                    Notifications.shared.app = app
                }
                #if DEBUG
                .task {
                    // Screenshots: start reading the open article aloud.
                    guard ProcessInfo.processInfo.environment["CORR_LISTEN"] != nil else { return }
                    try? await Task.sleep(for: .seconds(4))
                    if let e = app.article { await player.play(e) }
                }
                #endif
                .task { await app.boot() }
                // A link that arrived before the window did.
                .task {
                    if let url = IntentRouter.shared.pending {
                        IntentRouter.shared.pending = nil
                        await handle(url)
                    }
                }
                .onOpenURL { url in Task { await handle(url) } }
                .onChange(of: app.account == nil) { _, signedOut in if signedOut { player.stop() } }
                .onChange(of: IntentRouter.shared.pending) { _, url in
                    guard let url else { return }
                    IntentRouter.shared.pending = nil
                    Task { await handle(url) }
                }
                .onChange(of: phase) { _, now in
                    if now == .active, app.account != nil { Task { await app.resumed() } }
                    if now == .background { app.scheduleRefresh(); Task { await app.saveOffline() } }
                }
                #if os(macOS)
                .frame(minWidth: 980, minHeight: 620)
                .background(RemembersFrame())
                #endif
        }
        #if os(iOS)
        .backgroundTask(.appRefresh(AppModel.refreshTask)) { await app.backgroundRefresh() }
        #endif
        #if os(macOS)
        .defaultSize(width: 1280, height: 820)
        .windowToolbarStyle(.unifiedCompact)
        .commands { MacCommands(app: app, player: player) }
        #endif

        #if os(macOS)
        // ⌘, opens Settings in its own window, as Mac apps do.
        Settings {
            SettingsWindow()
                .environment(app)
                .environment(player)
                .frame(width: 640, height: 720)
        }
        #endif
    }
}
