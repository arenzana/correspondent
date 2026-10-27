import SwiftUI
import CorrespondentKit

/// Sign in to a Correspondent server, as the web's login page. The password
/// is exchanged for a token for this device and never kept.
struct SignInView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @State private var server = ""
    @State private var username = ""
    @State private var password = ""
    @State private var busy = false
    @State private var message = ""

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                HStack(spacing: 12) {
                    Mark(size: 40)
                    Text("Correspondent").font(Typeface.read.font(30, weight: 560, opsz: 48)).foregroundStyle(theme.ink)
                }
                .padding(.bottom, 8)
                Text("Sign in to your Correspondent server.").font(Typeface.ui.font(16)).foregroundStyle(theme.slate)

                field("Server", text: $server, prompt: "rss.example.org")
                    .textContentType(.URL)
                    #if os(iOS)
                    .keyboardType(.URL).textInputAutocapitalization(.never)
                    #endif
                field("Username", text: $username, prompt: "")
                    .textContentType(.username)
                    #if os(iOS)
                    .textInputAutocapitalization(.never)
                    #endif
                VStack(alignment: .leading, spacing: 6) {
                    Text("Password").font(Typeface.ui.font(13.5, weight: 600)).foregroundStyle(theme.ink)
                    SecureField("", text: $password)
                        .textContentType(.password)
                        .modifier(FieldStyle())
                        .onSubmit(submit)
                }

                Button(action: submit) {
                    Text(busy ? "Signing in…" : "Sign in").font(Typeface.ui.font(16, weight: 600)).foregroundStyle(.white)
                        .frame(maxWidth: .infinity).frame(height: 44)
                        .background(RoundedRectangle(cornerRadius: 6).fill(theme.accent))
                }
                .buttonStyle(.plain)
                .disabled(busy || server.isEmpty || username.isEmpty || password.isEmpty)

                if !message.isEmpty {
                    Text(message).font(Typeface.ui.font(14)).foregroundStyle(theme.danger)
                }
                Text("Your password is used once, to get a token for this device. You can revoke it in Settings → Account on the web.")
                    .font(Typeface.ui.font(13)).foregroundStyle(theme.slate)
            }
            .frame(maxWidth: 420)
            .padding(28)
            .frame(maxWidth: .infinity)
            .padding(.top, 60)
        }
        .background(theme.fog)
    }

    private func field(_ label: String, text: Binding<String>, prompt: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(label).font(Typeface.ui.font(13.5, weight: 600)).foregroundStyle(theme.ink)
            TextField(prompt, text: text).autocorrectionDisabled().modifier(FieldStyle())
        }
    }

    private func submit() {
        guard !busy else { return }
        busy = true
        message = ""
        Task {
            do {
                try await app.signIn(server: server, username: username, password: password)
            } catch {
                message = error.localizedDescription
            }
            busy = false
        }
    }
}

struct FieldStyle: ViewModifier {
    @Environment(\.theme) private var theme
    func body(content: Content) -> some View {
        content
            .font(Typeface.ui.font(16))
            .foregroundStyle(theme.ink)
            .textFieldStyle(.plain)
            .padding(.horizontal, 12).frame(height: 42)
            .background(RoundedRectangle(cornerRadius: 6).fill(theme.sheet))
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.rule))
    }
}
