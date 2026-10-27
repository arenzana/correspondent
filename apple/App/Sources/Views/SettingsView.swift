import SwiftUI
import CorrespondentKit

/// Settings for now: the account, the theme (set on the web, followed
/// here), the offline copy, and signing out. More follows the web's page.
struct SettingsView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    var onBack: (() -> Void)? = nil

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 8) {
                if let onBack {
                    Button(action: onBack) { Image(systemName: "chevron.left") }.iconButton().accessibilityLabel("Back to articles")
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text("Settings").font(Typeface.read.font(23, weight: 560, opsz: 36)).foregroundStyle(theme.ink)
                    Text("Signed in as \(app.account?.username ?? "")").font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate)
                }
                Spacer()
            }
            .padding(.horizontal, 18).padding(.vertical, 12)
            .overlay(alignment: .bottom) { Rectangle().fill(theme.rule).frame(height: 1) }

            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    card("Account") {
                        row("Server", app.account?.server.host() ?? "")
                        row("Username", app.account?.username ?? "")
                    }
                    card("Appearance") {
                        row("Theme", app.prefs.theme.capitalized)
                        Text("The theme, reading options and AI keys are set in Correspondent on the web, and followed here.")
                            .font(Typeface.ui.font(13)).foregroundStyle(theme.slate)
                    }
                    card("This device") {
                        Text("Unread and starred articles are kept on this device for reading offline.")
                            .font(Typeface.ui.font(14)).foregroundStyle(theme.slate)
                        Button("Save for offline now") { Task { await app.syncOffline(); app.show("Saved for offline reading.") } }
                            .buttonStyle(.bordered).tint(theme.ink)
                    }
                    Button("Sign out") { Task { await app.signOut() } }
                        .buttonStyle(.bordered).tint(theme.danger)
                    Text("Correspondent \(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "")")
                        .font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate)
                }
                .padding(20)
                .frame(maxWidth: 640, alignment: .leading)
                .frame(maxWidth: .infinity)
            }
        }
        .background(theme.sheet)
    }

    private func card<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title).font(Typeface.read.font(19, weight: 560)).foregroundStyle(theme.ink)
            content()
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 10).stroke(theme.rule))
    }

    private func row(_ label: String, _ value: String) -> some View {
        HStack {
            Text(label).font(Typeface.ui.font(15)).foregroundStyle(theme.slate)
            Spacer()
            Text(value).font(Typeface.ui.font(15, weight: 520)).foregroundStyle(theme.ink)
        }
    }
}
