import SwiftUI

@main
struct CorrespondentApp: App {
    @State private var app = AppModel()
    @Environment(\.scenePhase) private var phase

    init() {
        Typeface.register()
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(app)
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
