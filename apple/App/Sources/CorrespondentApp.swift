import SwiftUI

@main
struct CorrespondentApp: App {
    @State private var app = AppModel()
    @State private var player = Player()
    @Environment(\.scenePhase) private var phase

    init() {
        Typeface.register()
    }

    /// Links from widgets and Shortcuts: unread, an article, or listen.
    @MainActor
    private func handle(_ url: URL) async {
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
                .onAppear { player.app = app }
                #if DEBUG
                .task {
                    // Screenshots: start reading the open article aloud.
                    guard ProcessInfo.processInfo.environment["CORR_LISTEN"] != nil else { return }
                    try? await Task.sleep(for: .seconds(4))
                    if let e = app.article { await player.play(e) }
                }
                #endif
                .task { await app.boot() }
                .onOpenURL { url in Task { await handle(url) } }
                .onChange(of: IntentRouter.shared.pending) { _, url in
                    guard let url else { return }
                    IntentRouter.shared.pending = nil
                    Task { await handle(url) }
                }
                .onChange(of: phase) { _, now in
                    if now == .active, app.account != nil { Task { await app.resumed() } }
                    if now == .background { app.scheduleRefresh() }
                }
                #if os(macOS)
                .frame(minWidth: 980, minHeight: 620)
                #endif
        }
        #if os(iOS)
        .backgroundTask(.appRefresh(AppModel.refreshTask)) { await app.backgroundRefresh() }
        #endif
        #if os(macOS)
        .defaultSize(width: 1280, height: 820)
        .windowToolbarStyle(.unifiedCompact)
        #endif
    }
}
