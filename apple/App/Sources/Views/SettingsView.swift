import SwiftUI
import UniformTypeIdentifiers
import CorrespondentKit

/// Settings, as the web's page (ui/src/components/Settings.svelte) for what
/// an app on this device can change. Preferences are shared with the web
/// app: changing one here changes it there, and the other way round.
struct SettingsView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.openURL) private var openURL
    var onBack: (() -> Void)? = nil
    @State private var saving = false
    @State private var confirmSignOut = false
    @State private var search: String = {
        #if DEBUG
        ProcessInfo.processInfo.environment["CORR_SETTINGS_SEARCH"] ?? ""
        #else
        ""
        #endif
    }()
    @State private var importing = false
    @State private var exportFile: OPMLFile?
    @State private var copied = false

    /// The sections, with the words each answers to (as the web's settings search).
    private static let sections: [(title: String, words: String)] = [
        ("Appearance", "theme sun sunrise sunset day evening night dark light system app icon wire byline morse"),
        ("Reading", "text size interface larger smaller density compact comfortable system font typeface serif sans bionic spotlight focus paragraph line dim line length unread only mark read opened sponsored skip"),
        ("Feeds", "feeds subscriptions manage folders rename move pause delete failing address opml import export full pages"),
        ("AI", "ai model anthropic openai claude key summaries parser read aloud speech voice tts elevenlabs deepgram"),
        ("Apps & sync", "reeder apps freshrss google reader server username password sync api"),
        ("This device", "offline badge count icon dock notifications bell test save background audio listen"),
        ("Account", "server sign out password passkey web token"),
        ("About", "version build source"),
    ]

    /// A section's place to scroll to. Not its bare title: the section
    /// buttons have those ids, and scrollTo found the button instead.
    static func anchor(_ title: String) -> String { "section." + title }

    private var shown: [String] {
        let words = search.lowercased().split(separator: " ").map(String.init)
        return Self.sections.filter { s in
            let hay = (s.title + " " + s.words).lowercased()
            return words.allSatisfy { hay.contains($0) }
        }.map(\.title)
    }

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

            ScrollViewReader { proxy in
                guide(proxy)
                    #if DEBUG
                    .task {
                        guard let to = ProcessInfo.processInfo.environment["CORR_SETTINGS_JUMP"] else { return }
                        try? await Task.sleep(for: .seconds(2))
                        withAnimation { proxy.scrollTo(Self.anchor(to), anchor: .top) }
                    }
                    #endif
                ScrollView {
                    VStack(alignment: .leading, spacing: 34) {
                        if shown.contains("Appearance") { appearance.id(Self.anchor("Appearance")) }
                        if shown.contains("Reading") { reading.id(Self.anchor("Reading")) }
                        if shown.contains("Feeds") { feeds.id(Self.anchor("Feeds")) }
                        if shown.contains("AI"), app.me?.aiAvailable == true { ai.id(Self.anchor("AI")) }
                        if shown.contains("Apps & sync") { apps.id(Self.anchor("Apps & sync")) }
                        if shown.contains("This device") { device.id(Self.anchor("This device")) }
                        if shown.contains("Account") { account.id(Self.anchor("Account")) }
                        if shown.contains("About") { about.id(Self.anchor("About")) }
                        if shown.isEmpty {
                            Text("No settings match “\(search.trimmingCharacters(in: .whitespaces))”.")
                                .font(Typeface.ui.font(14.5)).foregroundStyle(theme.slate)
                        }
                    }
                    .padding(.horizontal, 20).padding(.vertical, 24)
                    .frame(maxWidth: 680, alignment: .leading)
                    .frame(maxWidth: .infinity)
                }
            }
        }
        .background(theme.sheet)
        .confirmationDialog("Sign out of \(app.account?.server.host() ?? "this server")?", isPresented: $confirmSignOut, titleVisibility: .visible) {
            Button("Sign out", role: .destructive) { Task { await app.signOut() } }
        } message: {
            Text("This device's token is revoked and its offline copy deleted.")
        }
    }

    // MARK: Sections

    private var appearance: some View {
        section("Appearance") {
            VStack(alignment: .leading, spacing: 10) {
                Text("Theme").font(Typeface.ui.font(15, weight: 600)).foregroundStyle(theme.ink)
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 104), spacing: 10)], spacing: 10) {
                    themeTile("auto", "Match system", [Theme.day, Theme.night])
                    themeTile("sun", "Sunrise and sunset", [Theme.day, Theme.evening, Theme.night])
                    themeTile("day", "Day", [Theme.day])
                    themeTile("evening", "Evening", [Theme.evening])
                    themeTile("night", "Night desk", [Theme.night])
                }
                if app.prefs.theme == "sun" {
                    Text(sunNote).font(Typeface.ui.font(13)).foregroundStyle(theme.slate)
                }
            }
            VStack(alignment: .leading, spacing: 10) {
                Text("App icon").font(Typeface.ui.font(15, weight: 600)).foregroundStyle(theme.ink)
                HStack(spacing: 14) {
                    ForEach(AppIconChoice.allCases) { icon in
                        let on = app.prefs.appIcon == icon.rawValue
                        Button {
                            app.setPrefs({ $0.appIcon = icon.rawValue }, ["appIcon": icon.rawValue])
                            icon.apply()
                        } label: {
                            VStack(spacing: 6) {
                                icon.preview.resizable().frame(width: 60, height: 60)
                                    .clipShape(RoundedRectangle(cornerRadius: 13.5, style: .continuous))
                                    .overlay(RoundedRectangle(cornerRadius: 13.5, style: .continuous).stroke(on ? theme.accent : .clear, lineWidth: 2.5).padding(-4))
                                Text(icon.label).font(Typeface.ui.font(13, weight: on ? 600 : 450)).foregroundStyle(on ? theme.accent : theme.ink)
                            }
                        }
                        .buttonStyle(.plain)
                        .accessibilityAddTraits(on ? .isSelected : [])
                    }
                }
            }
        }
    }

    private var reading: some View {
        section("Reading") {
            VStack(alignment: .leading, spacing: 18) {
                // A live preview of the article text.
                Text("Most of the craft is restraint: deciding which of a thousand small events deserve a second look.")
                    .font(previewFont)
                    .foregroundStyle(theme.ink)
                    .lineSpacing(app.prefs.readSize * 0.4)
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: 10).fill(theme.fog))

                Text("Sizes are kept on this device, so a phone and a desk can each have their own.")
                    .font(Typeface.ui.font(13)).foregroundStyle(theme.slate)
                VStack(alignment: .leading, spacing: 8) {
                    Text("Interface text").font(Typeface.ui.font(15, weight: 600)).foregroundStyle(theme.ink)
                    Picker("Interface text", selection: Binding(get: { app.prefs.uiScale }, set: { v in
                        app.setPrefs({ $0.uiScale = v }, ["uiScale": v])
                    })) {
                        Text("Small").tag(0.92)
                        Text("Default").tag(1.0)
                        Text("Large").tag(1.1)
                        Text("Larger").tag(1.2)
                    }
                    .pickerStyle(.segmented).labelsHidden().frame(maxWidth: 360)
                }
                choice("Interface typeface", app.prefs.uiFont, [("grotesk", "Grotesk"), ("system", "System")]) { v in
                    app.setPrefs({ $0.uiFont = v }, ["uiFont": v])
                }
                choice("List density", app.prefs.density, [("comfortable", "Comfortable"), ("compact", "Compact")]) { v in
                    app.setPrefs({ $0.density = v }, ["density": v])
                }
                labeled("Article text", "\(Int(app.prefs.readSize)) pt") {
                    Stepper("Article text size", value: Binding(get: { app.prefs.readSize }, set: { v in
                        app.setPrefs({ $0.readSize = v }, ["readSize": v])
                    }), in: 14...30, step: 1).labelsHidden()
                }
                labeled("Typeface", nil) {
                    Picker("", selection: Binding(get: { app.prefs.readFont }, set: { v in
                        app.setPrefs({ $0.readFont = v }, ["readFont": v])
                    })) {
                        Text("Serif").tag("serif")
                        Text("Sans").tag("sans")
                    }
                    .pickerStyle(.segmented).labelsHidden().frame(maxWidth: 200)
                }
                ReadingAids()
                labeled("Line length", "\(Int(app.prefs.measure)) characters") {
                    Slider(value: Binding(get: { app.prefs.measure }, set: { v in
                        let r = v.rounded()
                        if r != app.prefs.measure { app.setPrefs({ $0.measure = r }, ["measure": r]) }
                    }), in: 52...84, step: 2).frame(maxWidth: 220)
                }

                toggle("Show unread only in feeds and folders", "Today, Starred and All articles always show everything.",
                       isOn: app.prefs.unreadOnly) { v in app.setPrefs({ $0.unreadOnly = v }, ["unreadOnly": v]) }
                toggle("Mark as read when opened", "After a moment, so skimming past doesn't read them.",
                       isOn: app.prefs.markOnOpen) { v in app.setPrefs({ $0.markOnOpen = v }, ["markOnOpen": v]) }
                toggle("Skip sponsored posts", "Posts titled “Sponsor:” and the like arrive already read, here and in Reeder.",
                       isOn: app.prefs.skipSponsored) { v in app.setPrefs({ $0.skipSponsored = v }, ["skipSponsored": v]) }
            }
        }
    }

    private var feeds: some View {
        section("Feeds") {
            VStack(alignment: .leading, spacing: 12) {
                let f = app.tree.feeds
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 120), spacing: 10)], spacing: 10) {
                    stat(f.count, "feeds")
                    stat(f.filter(\.fullText).count, "with full articles")
                    stat(f.filter(\.page).count, "read from pages")
                    stat(f.filter { !$0.paused && $0.errorCount > 0 }.count, "failing", warn: true)
                }
                HStack(spacing: 8) {
                    Button("Manage subscriptions") { NotificationCenter.default.post(name: .manage, object: nil) }
                    Button("Import OPML…") { importing = true }
                    Button("Export OPML") { Task { await exportOPML() } }
                }
                .buttonStyle(QuietButton())
            }
        }
        .fileImporter(isPresented: $importing, allowedContentTypes: [UTType(filenameExtension: "opml") ?? .xml, .xml]) { result in
            if case .success(let url) = result { Task { await importOPML(url) } }
        }
        .fileExporter(isPresented: Binding(get: { exportFile != nil }, set: { if !$0 { exportFile = nil } }),
                      document: exportFile, contentType: UTType(filenameExtension: "opml") ?? .xml,
                      defaultFilename: "Correspondent subscriptions.opml") { result in
            if case .success = result { app.show("Exported your subscriptions.") }
        }
    }

    private func stat(_ n: Int, _ label: String, warn: Bool = false) -> some View {
        Button { NotificationCenter.default.post(name: .manage, object: nil) } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text("\(n)").font(Typeface.read.font(24, weight: 560)).foregroundStyle(warn && n > 0 ? theme.danger : theme.ink)
                Text(label).font(Typeface.ui.font(12.5)).foregroundStyle(theme.slate)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(RoundedRectangle(cornerRadius: 8).fill(theme.fog))
        }
        .buttonStyle(.plain)
    }

    private func importOPML(_ url: URL) async {
        guard let client = app.client else { return }
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        do {
            let added = try await client.importOPML(try Data(contentsOf: url))
            await app.refreshTree()
            app.show(added == 0 ? "Nothing new: you follow all of those already." : "Subscribed to \(added) \(added == 1 ? "feed" : "feeds").")
        } catch {
            app.show("Couldn't import it. \(error.localizedDescription)")
        }
    }

    private func exportOPML() async {
        guard let client = app.client else { return }
        do { exportFile = OPMLFile(data: try await client.exportOPML()) } catch { app.show("Couldn't export. \(error.localizedDescription)") }
    }

    /// The reader's own AI and read-aloud setups. Keys are set on the web:
    /// the server lets only a signed-in browser change them, never a token
    /// (which is how this app signs in).
    private var ai: some View {
        section("AI") {
            VStack(alignment: .leading, spacing: 12) {
                setup("AI model", app.me?.ai, uses: "Summaries, and parsers for sites without feeds.")
                setup("Read aloud", app.me?.speech, uses: "Articles in a natural voice, shared with everyone here once made (unless made through an address you entered).")
                webButton("Change on the web")
            }
        }
    }

    private func setup(_ title: String, _ s: Me.Setup?, uses: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title).font(Typeface.ui.font(15, weight: 600)).foregroundStyle(theme.ink)
            Text(uses).font(Typeface.ui.font(13)).foregroundStyle(theme.slate).fixedSize(horizontal: false, vertical: true)
            if let s, let provider = s.provider {
                let name = ["anthropic": "Anthropic", "openai": "OpenAI or compatible", "elevenlabs": "ElevenLabs", "deepgram": "Deepgram"][provider] ?? provider
                let detail = [name, s.model, s.voice.map { "voice \($0)" }, s.keyHint.map { "key ending \($0)" }]
                    .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: ", ")
                Text(detail).font(Typeface.ui.font(13.5, weight: 500)).foregroundStyle(theme.ink)
            } else {
                Text("Not set up.").font(Typeface.ui.font(13.5, weight: 500)).foregroundStyle(theme.ink)
            }
        }
    }

    /// What Reeder and other FreshRSS-compatible apps need.
    private var apps: some View {
        section("Apps & sync") {
            VStack(alignment: .leading, spacing: 12) {
                Text("Add a FreshRSS account in Reeder or another app with these details. Your sign-in password never works for apps.")
                    .font(Typeface.ui.font(14)).foregroundStyle(theme.slate).fixedSize(horizontal: false, vertical: true)
                let server = (app.account?.server.absoluteString ?? "").trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/api/greader.php"
                HStack {
                    row("Server", server)
                    Button(copied ? "Copied" : "Copy") { copy(server); copied = true }
                        .buttonStyle(.plain).font(Typeface.ui.font(13.5, weight: 560)).foregroundStyle(theme.accent)
                }
                row("Username", app.account?.username ?? "")
                row("App password", app.me?.hasApiPassword == true ? "Set" : "Not set yet")
                webButton(app.me?.hasApiPassword == true ? "Change the app password on the web" : "Choose an app password on the web")
            }
        }
    }

    /// Opens Settings on the web, where keys and the app password are changed.
    private func webButton(_ title: String) -> some View {
        Button(title) {
            if let server = app.account?.server, let url = URL(string: server.absoluteString.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + "/#/settings") { openURL(url) }
        }
        .buttonStyle(QuietButton())
    }

    private func copy(_ s: String) {
        #if os(iOS)
        UIPasteboard.general.string = s
        #else
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(s, forType: .string)
        #endif
    }

    private var device: some View {
        section("This device") {
            VStack(alignment: .leading, spacing: 12) {
                Text("Unread and starred articles are kept on this device, with their text, for reading offline, and refreshed in the background.")
                    .font(Typeface.ui.font(14.5)).foregroundStyle(theme.slate)
                toggle(badgeLabel, "Kept up to date in the background.", isOn: app.showBadge) { v in Task { await app.enableBadge(v) } }
                toggle("Keep audio for listening offline", "Read-aloud audio for the newest \(Player.kept) unread or starred articles that have it. Nothing is downloaded in Low Data Mode.",
                       isOn: app.offlineAudio) { v in app.offlineAudio = v }
                if app.notificationsAvailable {
                    toggle("Notifications on this device", "Only feeds you choose notify you: long-press a feed and turn on its bell.",
                           isOn: app.notificationsOn) { v in Task { await app.setNotifications(v) } }
                    if app.notificationsOn {
                        Button("Send a test notification") { Task { await app.testNotification() } }.buttonStyle(QuietButton())
                    }
                }
                Button {
                    saving = true
                    Task {
                        await app.syncOffline()
                        saving = false
                        app.show("Saved for offline reading.")
                    }
                } label: {
                    Text(saving ? "Saving…" : "Save for offline now")
                }
                .buttonStyle(QuietButton()).disabled(saving || !app.online)
            }
        }
    }

    private var account: some View {
        section("Account") {
            VStack(alignment: .leading, spacing: 12) {
                row("Server", app.account?.server.host() ?? "")
                row("Signed in as", app.account?.username ?? "")
                Text("Passkeys, passwords, AI keys, notifications and subscriptions are managed in Correspondent on the web.")
                    .font(Typeface.ui.font(13.5)).foregroundStyle(theme.slate)
                HStack(spacing: 10) {
                    if let server = app.account?.server, let web = URL(string: server.absoluteString + "/#/settings") {
                        Button("Open on the web") { openURL(web) }
                            .buttonStyle(QuietButton())
                    }
                    Button("Sign out") { confirmSignOut = true }.buttonStyle(QuietButton(danger: true))
                }
            }
        }
    }

    private var about: some View {
        let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? ""
        let build = Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? ""
        return section("About") {
            VStack(alignment: .leading, spacing: 6) {
                Text("Correspondent \(version) (\(build))").font(Typeface.ui.font(14.5)).foregroundStyle(theme.ink)
                Button("Source") { openURL(URL(string: "https://github.com/arenzana/correspondent")!) }
                    .font(Typeface.ui.font(14.5)).tint(theme.accent).buttonStyle(.plain).foregroundStyle(theme.accent)
            }
        }
    }

    // MARK: Pieces

    /// Search, and the sections to jump to, as on the web.
    private func guide(_ proxy: ScrollViewProxy) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass").font(.system(size: 13)).foregroundStyle(theme.slate)
                TextField("Search settings", text: $search)
                    .textFieldStyle(.plain).font(Typeface.ui.font(14)).foregroundStyle(theme.ink)
                    .autocorrectionDisabled()
                if !search.isEmpty {
                    Button { search = "" } label: { Image(systemName: "xmark.circle.fill") }
                        .buttonStyle(.plain).foregroundStyle(theme.slate).accessibilityLabel("Clear the search")
                }
            }
            .padding(.horizontal, 12).frame(height: 34)
            .background(RoundedRectangle(cornerRadius: 6).fill(theme.fog))
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    ForEach(shown, id: \.self) { title in
                        Button {
                            withAnimation(.easeOut(duration: 0.25)) { proxy.scrollTo(Self.anchor(title), anchor: .top) }
                        } label: {
                            Text(title).font(Typeface.ui.font(13.5, weight: 500)).foregroundStyle(theme.ink)
                                .padding(.horizontal, 11).frame(height: 28)
                                .background(Capsule().stroke(theme.rule))
                                .contentShape(Capsule())
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
        .padding(.horizontal, 18).padding(.vertical, 10)
        .overlay(alignment: .bottom) { Rectangle().fill(theme.rule).frame(height: 1) }
    }

    private var badgeLabel: String {
        #if os(iOS)
        "Show the unread count on the app icon"
        #else
        "Show the unread count on the Dock icon"
        #endif
    }

    private var previewFont: Font {
        app.prefs.readFont == "sans"
            ? Typeface.ui.font(app.prefs.readSize)
            : Typeface.read.font(app.prefs.readSize, opsz: 16)
    }

    private func section<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(title).font(Typeface.read.font(21, weight: 560, opsz: 36)).foregroundStyle(theme.ink)
            content()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// What the Sun theme does next, as the web's note under the themes.
    private var sunNote: String {
        let s = Theme.sun(place: app.prefs.place)
        let next = ["day": "Evening", "evening": "Night desk", "night": "Day"][s.phase.rawValue] ?? ""
        let time = s.next.formatted(date: .omitted, time: .shortened)
        return "\(next) at \(time)" + (app.prefs.place?.estimated ?? true ? ", estimated from your time zone." : ".")
    }

    private func themeTile(_ id: String, _ label: String, _ swatches: [Theme]) -> some View {
        let on = app.prefs.theme == id
        return Button {
            app.setTheme(id)
        } label: {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 0) {
                    ForEach(Array(swatches.enumerated()), id: \.offset) { _, t in
                        ZStack(alignment: .topLeading) {
                            t.sheet
                            VStack(alignment: .leading, spacing: 4) {
                                Circle().fill(t.accent).frame(width: 7, height: 7)
                                RoundedRectangle(cornerRadius: 1).fill(t.ink).frame(width: 26, height: 3)
                                RoundedRectangle(cornerRadius: 1).fill(t.slate).frame(width: 18, height: 3)
                            }
                            .padding(8)
                        }
                    }
                }
                .frame(height: 52)
                .clipShape(RoundedRectangle(cornerRadius: 6))
                .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.rule))
                Text(label).font(Typeface.ui.font(13, weight: on ? 600 : 450)).foregroundStyle(on ? theme.accent : theme.ink)
                    .lineLimit(1).minimumScaleFactor(0.85)
            }
            .padding(8)
            .background(RoundedRectangle(cornerRadius: 10).stroke(on ? theme.accent : theme.rule, lineWidth: on ? 2 : 1))
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? .isSelected : [])
    }

    /// A label over a few choices, as segments.
    private func choice(_ label: String, _ value: String, _ options: [(String, String)], set: @escaping (String) -> Void) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(label).font(Typeface.ui.font(15, weight: 600)).foregroundStyle(theme.ink)
            Picker(label, selection: Binding(get: { value }, set: set)) {
                ForEach(options, id: \.0) { Text($0.1).tag($0.0) }
            }
            .pickerStyle(.segmented).labelsHidden().frame(maxWidth: 360)
        }
    }

    private func labeled<Control: View>(_ label: String, _ value: String?, @ViewBuilder control: () -> Control) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(label).font(Typeface.ui.font(15, weight: 600)).foregroundStyle(theme.ink)
                if let value { Text(value).font(Typeface.ui.font(13)).foregroundStyle(theme.slate) }
            }
            Spacer()
            control()
        }
    }

    private func toggle(_ title: String, _ note: String, isOn: Bool, set: @escaping (Bool) -> Void) -> some View {
        // The text wraps and the switch sits at the end: a Mac checkbox's
        // label would keep to one line and widen the whole page.
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(Typeface.ui.font(15, weight: 600)).foregroundStyle(theme.ink)
                Text(note).font(Typeface.ui.font(13)).foregroundStyle(theme.slate)
            }
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            Toggle(title, isOn: Binding(get: { isOn }, set: set)).labelsHidden().toggleStyle(.switch).tint(theme.accent)
        }
    }

    private func row(_ label: String, _ value: String) -> some View {
        HStack {
            Text(label).font(Typeface.ui.font(15)).foregroundStyle(theme.slate)
            Spacer()
            Text(value).font(Typeface.ui.font(15, weight: 520)).foregroundStyle(theme.ink)
        }
    }
}

/// The web's .btn: a quiet outlined button.
struct QuietButton: ButtonStyle {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    var danger = false
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(Typeface.ui.font(14.5, weight: 520))
            .foregroundStyle(danger ? theme.danger : theme.ink)
            .padding(.horizontal, 14).frame(height: 36)
            .background(RoundedRectangle(cornerRadius: 6).fill(configuration.isPressed ? theme.hover : theme.sheet))
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(theme.rule))
            .opacity(enabled ? 1 : 0.5)
    }
}

/// An exported OPML file, for the system's save panel.
struct OPMLFile: FileDocument {
    static let readableContentTypes = [UTType(filenameExtension: "opml") ?? .xml]
    var data: Data
    init(data: Data) { self.data = data }
    init(configuration: ReadConfiguration) throws { data = configuration.file.regularFileContents ?? Data() }
    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper { FileWrapper(regularFileWithContents: data) }
}
