import SwiftUI

@main
struct CorrespondentApp: App {
    @State private var app = AppModel()

    init() {
        Typeface.register()
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(app)
                .task { await app.boot() }
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
