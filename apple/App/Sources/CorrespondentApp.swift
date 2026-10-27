import SwiftUI

@main
struct CorrespondentApp: App {
    @State private var app = AppModel()
    @State private var player = Player()
    @Environment(\.scenePhase) private var phase

    init() {
        Typeface.register()
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
                .onChange(of: phase) { _, now in
                    if now == .active, app.account != nil { Task { await app.resumed() } }
                }
                #if os(macOS)
                .frame(minWidth: 980, minHeight: 620)
                #endif
        }
        #if os(macOS)
        .defaultSize(width: 1280, height: 820)
        .windowToolbarStyle(.unifiedCompact)
        #endif
    }
}
