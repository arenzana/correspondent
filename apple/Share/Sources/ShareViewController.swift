import SwiftUI
import UIKit
import UniformTypeIdentifiers
import CorrespondentKit

/// "Correspondent" in the share sheet: finds the shared page's feeds and
/// subscribes, without opening the app. It signs in as the app does (the
/// account and token are shared through the App Group and Keychain group).
final class ShareViewController: UIViewController {
    override func viewDidLoad() {
        super.viewDidLoad()
        Typeface.register()
        let done: () -> Void = { [weak self] in self?.extensionContext?.completeRequest(returningItems: nil) }
        let host = UIHostingController(rootView: ShareView(load: { await self.sharedURL() }, done: done))
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
    }

    /// The shared address: a URL, or text that is one.
    private func sharedURL() async -> URL? {
        let providers = (extensionContext?.inputItems as? [NSExtensionItem] ?? []).flatMap { $0.attachments ?? [] }
        for p in providers where p.hasItemConformingToTypeIdentifier(UTType.url.identifier) {
            if let url = try? await p.loadItem(forTypeIdentifier: UTType.url.identifier) as? URL { return url }
        }
        for p in providers where p.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) {
            if let text = try? await p.loadItem(forTypeIdentifier: UTType.plainText.identifier) as? String,
               let url = URL(string: text.trimmingCharacters(in: .whitespacesAndNewlines)), url.scheme?.hasPrefix("http") == true {
                return url
            }
        }
        return nil
    }
}

struct ShareView: View {
    var load: () async -> URL?
    var done: () -> Void
    @Environment(\.colorScheme) private var scheme
    @State private var url: URL?
    @State private var found: [Candidate] = []
    @State private var chosen: Candidate?
    @State private var folders: [Folder] = []
    @State private var folder = ""
    @State private var state: Phase = .finding
    enum Phase: Equatable { case finding, choose, none, signedOut, subscribing, done(String), failed(String) }

    var body: some View {
        let theme = Theme.resolve("auto", scheme: scheme)
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    if let url { Text(url.host() ?? url.absoluteString).font(Typeface.ui.font(14)).foregroundStyle(theme.slate) }
                    switch state {
                    case .finding:
                        ProgressView("Looking for feeds…").frame(maxWidth: .infinity).padding(.top, 30)
                    case .signedOut:
                        note("Sign in to Correspondent in the app first.", theme)
                    case .none:
                        note("No feed found on this page. In the app, Add a feed can have your AI read pages without one.", theme)
                    case .failed(let m):
                        note(m, theme)
                    case .done(let m):
                        note(m, theme)
                    case .choose, .subscribing:
                        ForEach(found) { c in row(c, theme) }
                        if !folders.isEmpty {
                            Text("Folder").font(Typeface.ui.font(13.5, weight: 600)).foregroundStyle(theme.ink).padding(.top, 6)
                            ScrollView(.horizontal, showsIndicators: false) {
                                HStack(spacing: 6) {
                                    chip("None", on: folder.isEmpty, theme) { folder = "" }
                                    ForEach(folders) { f in chip(f.name, on: folder == f.name, theme) { folder = f.name } }
                                }
                            }
                        }
                    }
                }
                .padding(20)
            }
            .background(theme.sheet)
            .navigationTitle("Subscribe")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(state == .done("") ? "Done" : "Cancel", action: done) }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Subscribe") { Task { await subscribe() } }
                        .disabled(chosen == nil || state != .choose)
                }
            }
        }
        .tint(theme.accent)
        .task { await find() }
    }

    private func note(_ text: String, _ theme: Theme) -> some View {
        Text(text).font(Typeface.ui.font(15)).foregroundStyle(theme.ink).padding(.top, 20)
    }

    private func row(_ c: Candidate, _ theme: Theme) -> some View {
        let on = chosen == c
        return Button { chosen = c } label: {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: on ? "largecircle.fill.circle" : "circle").foregroundStyle(on ? theme.accent : theme.ring)
                VStack(alignment: .leading, spacing: 3) {
                    Text(c.title.isEmpty ? c.url : c.title).font(Typeface.read.font(17, weight: 560)).foregroundStyle(theme.ink)
                    Text("\(c.itemCount) articles").font(Typeface.ui.font(13)).foregroundStyle(theme.slate)
                }
                Spacer()
            }
            .padding(12)
            .background(RoundedRectangle(cornerRadius: 10).stroke(on ? theme.accent : theme.rule, lineWidth: on ? 2 : 1))
        }
        .buttonStyle(.plain)
    }

    private func chip(_ label: String, on: Bool, _ theme: Theme, action: @escaping () -> Void) -> some View {
        Button(label, action: action)
            .font(Typeface.ui.font(13.5, weight: on ? 600 : 450)).foregroundStyle(on ? theme.accent : theme.ink)
            .padding(.horizontal, 10).frame(height: 30)
            .background(Capsule().stroke(on ? theme.accent : theme.rule))
            .buttonStyle(.plain)
    }

    private var client: Client? {
        Account.load().map { Client(server: $0.0.server, token: $0.1) }
    }

    private func find() async {
        url = await load()
        guard let client else { state = .signedOut; return }
        guard let url else { state = .failed("Nothing to subscribe to was shared."); return }
        async let tree = try? client.tree()
        do {
            found = try await client.discover(url.absoluteString)
            folders = await tree?.folders ?? []
            chosen = found.first
            state = found.isEmpty ? .none : .choose
        } catch {
            state = .failed(error.localizedDescription)
        }
    }

    private func subscribe() async {
        guard let client, let c = chosen else { return }
        state = .subscribing
        do {
            try await client.subscribe(c, folder: folder.isEmpty ? nil : folder)
            state = .done("Subscribed to \(c.title.isEmpty ? c.url : c.title).")
            try? await Task.sleep(for: .seconds(1.2))
            done()
        } catch {
            state = .failed("Couldn't subscribe. \(error.localizedDescription)")
        }
    }
}
