import SwiftUI
import WebKit
import CorrespondentKit

/// The open article: a toolbar, then the article as the web shows it
/// (ui/src/components/Article.svelte), rendered with the same styles.
struct ArticleView: View {
    @Environment(AppModel.self) private var app
    @Environment(Player.self) private var player
    @Environment(\.theme) private var theme
    @Environment(\.openURL) private var openURL
    /// On iPhone, the way back to the list.
    var onBack: (() -> Void)? = nil
    /// In a navigation stack (iPhone): the actions go in the navigation bar.
    var native = false
    @State private var textControls = false
    @State private var zoomed: URL?

    var body: some View {
        VStack(spacing: 0) {
            if let e = app.article {
                if !native { toolbar(e) }
                ArticlePage(entry: e, feed: app.feedsByID[e.feedId], summary: app.summaryOpen ? e.summary : nil,
                            theme: theme, prefs: app.prefs, base: app.client?.server,
                            next: neighbour(e, 1), previous: neighbour(e, -1),
                            onNavigate: { step in Task { await app.move(step) } },
                            onImage: { zoomed = $0 })
                    .id(e.id)
            } else if !app.articleError.isEmpty {
                Text("Couldn't load this article. \(app.articleError)").font(Typeface.ui.font(15)).foregroundStyle(theme.slate).padding()
                Spacer()
            } else {
                placeholder
            }
        }
        .background(theme.sheet)
        .sheet(item: $zoomed) { url in ImageViewer(url: url).environment(\.theme, theme) }
        // On iPhone the Text item in the menu opens the controls as a short sheet.
        .sheet(isPresented: Binding(get: { native && textControls }, set: { textControls = $0 })) {
            TextControls().environment(\.theme, theme).padding(24)
                .presentationDetents([.height(260)])
        }
        #if os(iOS)
        .toolbar {
            if native, let e = app.article {
                ToolbarItemGroup(placement: .topBarTrailing) { actions(e) }
            }
        }
        #endif
    }

    /// The next or previous article in the list, for the links at the end.
    private func neighbour(_ e: Entry, _ step: Int) -> Entry? {
        guard let i = app.entries.firstIndex(where: { $0.id == e.id }), app.entries.indices.contains(i + step) else { return nil }
        return app.entries[i + step]
    }

    /// Reads the article aloud (making its audio if needed: the reader asked).
    private func listenButton(_ e: Entry) -> some View {
        Button {
            if player.now?.id == e.id { player.toggle() } else { Task { await player.play(e) } }
        } label: {
            Image(systemName: player.now?.id == e.id && player.playing ? "headphones.circle.fill" : "headphones")
                .symbolEffect(.pulse, isActive: player.preparing == e.id)
        }
        .disabled(player.preparing != nil)
        .accessibilityLabel(player.preparing == e.id ? "Preparing audio" : "Listen")
    }

    /// Star, read, summary, full article, original.
    @ViewBuilder
    private func actions(_ e: Entry) -> some View {
        Button { Task { await app.toggleStar(e.id) } } label: { Image(systemName: e.starred ? "star.fill" : "star") }
            .tint(e.starred ? theme.seal : theme.slate).accessibilityLabel(e.starred ? "Unstar" : "Star")
        Button { Task { await app.toggleRead(e.id) } } label: { Image(systemName: e.read ? "circle" : "largecircle.fill.circle") }
            .tint(theme.slate).accessibilityLabel(e.read ? "Mark unread" : "Mark read")
        if app.speechConfigured || e.audio {
            listenButton(e).tint(theme.slate)
        }
        Menu {
            Button { textControls = true } label: { Label("Text", systemImage: "textformat.size") }
            Button { Task { await app.summarize() } } label: { Label(app.summaryOpen ? "Hide summary" : "Summary", systemImage: "text.alignleft") }
            Button { Task { await app.loadFullArticle() } } label: { Label("Load the full article", systemImage: "doc.text") }
                .disabled(e.fullText)
            if let url = URL(string: e.url), !e.url.isEmpty {
                Button { openURL(url) } label: { Label("Open original", systemImage: "safari") }
                ShareLink(item: url)
            }
        } label: { Image(systemName: "ellipsis.circle") }
            .tint(theme.slate)
    }

    private func toolbar(_ e: Entry) -> some View {
        HStack(spacing: 2) {
            if let onBack {
                Button(action: onBack) { Image(systemName: "chevron.left") }.iconButton().accessibilityLabel("Back to articles")
            }
            if let i = app.entries.firstIndex(where: { $0.id == e.id }) {
                Text("\(i + 1) of \(app.entries.count)").font(Typeface.ui.font(12.5)).monospacedDigit().foregroundStyle(theme.slate)
                    .padding(.leading, 6)
            }
            Spacer()
            Button { Task { await app.toggleStar(e.id) } } label: { Image(systemName: e.starred ? "star.fill" : "star") }
                .iconButton().foregroundStyle(e.starred ? theme.seal : theme.slate)
                .accessibilityLabel(e.starred ? "Unstar" : "Star")
            Button { Task { await app.toggleRead(e.id) } } label: { Image(systemName: e.read ? "circle" : "largecircle.fill.circle") }
                .iconButton().accessibilityLabel(e.read ? "Mark unread" : "Mark read")
            if app.speechConfigured || e.audio {
                listenButton(e).iconButton()
            }
            Button { Task { await app.summarize() } } label: {
                Image(systemName: "text.alignleft").symbolEffect(.pulse, isActive: app.summarizing)
            }
            .iconButton().accessibilityLabel("Summary")
            Button { textControls = true } label: { Image(systemName: "textformat.size") }
                .iconButton().accessibilityLabel("Text size and typeface")
                .popover(isPresented: $textControls) { TextControls().environment(\.theme, theme).padding(20).frame(width: 300) }
            Button { NotificationCenter.default.post(name: .focus, object: nil) } label: { Image(systemName: "arrow.up.left.and.arrow.down.right") }
                .iconButton().accessibilityLabel("Focused reading")
            Button { Task { await app.loadFullArticle() } } label: { Image(systemName: "doc.text") }
                .iconButton().accessibilityLabel("Load the full article").disabled(e.fullText)
            if let url = URL(string: e.url), !e.url.isEmpty {
                Button { openURL(url) } label: { Image(systemName: "arrow.up.forward.square") }
                    .iconButton().accessibilityLabel("Open original")
            }
        }
        .padding(.horizontal, 10).frame(height: 52)
        .background(theme.sheet)
        .overlay(alignment: .bottom) { Rectangle().fill(theme.rule).frame(height: 1) }
    }

    private var placeholder: some View {
        VStack(spacing: 14) {
            Spacer()
            VStack(spacing: 6) {
                Circle().fill(theme.accent).frame(width: 9, height: 9)
                Circle().stroke(theme.ring, lineWidth: 1.5).frame(width: 9, height: 9)
                Circle().stroke(theme.ring, lineWidth: 1.5).frame(width: 9, height: 9)
            }
            Text("Select an article to read it.").font(Typeface.ui.font(15)).foregroundStyle(theme.slate)
            Spacer()
        }
        .frame(maxWidth: .infinity)
    }
}

/// The article page itself, in a web view with the web app's article styles.
struct ArticlePage: View {
    var entry: Entry
    var feed: Feed?
    var summary: String?
    var theme: Theme
    var prefs: Prefs
    var base: URL?
    var next: Entry? = nil
    var previous: Entry? = nil
    var onNavigate: (Int) -> Void = { _ in }
    var onImage: (URL) -> Void = { _ in }

    var body: some View {
        WebPage(html: ArticleHTML.page(entry: entry, feed: feed, summary: summary, theme: theme, prefs: prefs,
                                       next: next?.title, previous: previous?.title),
                base: base, onNavigate: onNavigate, onImage: onImage)
    }
}

enum ArticleHTML {
    static func escape(_ s: String) -> String {
        s.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;").replacingOccurrences(of: "\"", with: "&quot;")
    }

    /// The bundled fonts as data: a page from the server can't read the app's files.
    static let fonts: String = {
        func face(_ family: String, _ file: String, italic: Bool, weights: String) -> String {
            guard let url = Bundle.main.url(forResource: file, withExtension: "ttf"), let data = try? Data(contentsOf: url) else { return "" }
            return "@font-face { font-family: '\(family)'; src: url(data:font/ttf;base64,\(data.base64EncodedString())); font-style: \(italic ? "italic" : "normal"); font-weight: \(weights); }"
        }
        return [face("Newsreader", "Newsreader", italic: false, weights: "200 800"),
                face("Newsreader", "Newsreader-Italic", italic: true, weights: "200 800"),
                face("Schibsted Grotesk", "SchibstedGrotesk", italic: false, weights: "400 900")].joined(separator: "\n")
    }()

    static func page(entry e: Entry, feed: Feed?, summary: String?, theme: Theme, prefs: Prefs = Prefs(),
                     next: String? = nil, previous: String? = nil) -> String {
        let footer = (next == nil && previous == nil) ? "" : """
        <nav class="turn">
        \(previous.map { "<a class=\"prev\" href=\"correspondent-nav:previous\"><small>Previous</small>\(escape($0))</a>" } ?? "<span></span>")
        \(next.map { "<a class=\"next\" href=\"correspondent-nav:next\"><small>Next</small>\(escape($0))</a>" } ?? "")
        </nav>
        """
        // The reader's text size, line length and typeface, as on the web.
        let size = min(max(prefs.readSize, 14), 30)
        let measure = min(max(prefs.measure, 40), 90)
        let family = prefs.readFont == "sans" ? "var(--font-ui)" : "var(--font-read)"
        let vars = theme.css.map { "--\($0.key): \($0.value);" }.joined(separator: " ")
        let date = e.published.formatted(.dateTime.weekday(.wide).month(.wide).day().year())
        let words = (e.content ?? "").split { $0 == " " || $0 == "\n" }.count
        let minutes = max(1, Int((Double(words) / 230).rounded()))
        let byline = [feed?.title, e.author.isEmpty || e.author == feed?.title ? nil : "by " + e.author].compactMap { $0 }
        let title = e.url.isEmpty ? escape(e.title) : "<a href=\"\(escape(e.url))\">\(escape(e.title.isEmpty ? "Untitled" : e.title))</a>"
        let summaryHTML = summary.map { "<aside class=\"summary\"><p>\(escape($0))</p><small>Summary written by AI.</small></aside>" } ?? ""
        return """
        <!doctype html><html><head><meta charset="utf-8">
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <style>
        \(fonts)
        :root { \(vars) --font-read: 'Newsreader', 'Iowan Old Style', Georgia, serif; --font-ui: 'Schibsted Grotesk', -apple-system, sans-serif; color-scheme: \(theme.dark ? "dark" : "light"); }
        html, body { margin: 0; background: var(--sheet); color: var(--ink); -webkit-text-size-adjust: 100%; }
        .column { max-width: \(Int(measure))ch; margin: 0 auto; padding: 36px 24px 96px; }
        .byline { margin: 0 0 10px; font-family: var(--font-ui); font-size: 14px; color: var(--slate); }
        .byline strong { color: var(--ink); font-weight: 600; }
        h1.title { margin: 0 0 12px; font-family: var(--font-read); font-weight: 560; font-size: 34px; line-height: 1.12; letter-spacing: -0.015em; font-variation-settings: 'opsz' 72; text-wrap: balance; }
        h1.title a { color: inherit; text-decoration: none; }
        .dateline { margin: 0 0 22px; padding-bottom: 22px; border-bottom: 1px solid var(--rule); font-family: var(--font-ui); font-size: 13.5px; color: var(--slate); }
        .dateline span + span { margin-left: 14px; }
        .summary { margin: 0 0 28px; padding: 2px 0 2px 16px; border-left: 2px solid var(--accent); font-family: var(--font-ui); font-size: 15.5px; line-height: 1.55; }
        .summary p { margin: 0 0 6px; }
        .summary small { color: var(--slate); font-size: 12px; }
        .prose { font-family: \(family); font-size: \(Int(size))px; line-height: 1.66; font-variation-settings: 'opsz' 16; overflow-wrap: break-word; hanging-punctuation: first; }
        .prose p, .prose ul, .prose ol, .prose pre, .prose table, .prose figure, .prose blockquote { margin: 0 0 1.05em; }
        .prose h1, .prose h2, .prose h3, .prose h4 { font-family: var(--font-read); font-weight: 560; line-height: 1.2; letter-spacing: -0.01em; margin: 1.7em 0 0.55em; text-wrap: balance; }
        .prose h1, .prose h2 { font-size: 1.32em; }
        .prose h3 { font-size: 1.13em; }
        .prose a { color: var(--accent); text-decoration-thickness: 1px; text-underline-offset: 3px; }
        .prose img, .prose video, .prose picture { display: block; max-width: 100%; height: auto; border-radius: 4px; }
        .prose p > img:only-child { margin: 1.4em auto; }
        .prose figure { margin: 1.6em 0; }
        .prose figcaption { margin-top: 8px; font-family: var(--font-ui); font-size: 13px; line-height: 1.45; color: var(--slate); }
        .prose blockquote { margin-left: 0; padding-left: 1em; border-left: 2px solid var(--accent); font-style: italic; opacity: 0.9; }
        .prose code { font-family: ui-monospace, 'SF Mono', Menlo, monospace; font-size: 0.82em; padding: 0.1em 0.3em; border-radius: 4px; background: var(--fog); }
        .prose pre { padding: 14px 16px; overflow-x: auto; border-radius: 6px; background: var(--fog); line-height: 1.5; }
        .prose pre code { padding: 0; background: none; }
        .prose hr { border: 0; height: 1px; background: var(--rule); margin: 2em 0; }
        .prose table { width: 100%; border-collapse: collapse; font-size: 0.85em; display: block; overflow-x: auto; }
        .prose th, .prose td { padding: 6px 10px; border-bottom: 1px solid var(--rule); text-align: left; }
        .prose img { cursor: zoom-in; }
        .turn { display: flex; justify-content: space-between; gap: 16px; margin-top: 48px; padding-top: 20px; border-top: 1px solid var(--rule); font-family: var(--font-ui); }
        .turn a { display: block; max-width: 48%; color: var(--ink); text-decoration: none; font-family: var(--font-read); font-size: 17px; font-weight: 560; line-height: 1.3; }
        .turn a small { display: block; margin-bottom: 4px; font-family: var(--font-ui); font-size: 12.5px; font-weight: 500; color: var(--slate); }
        .turn .next { text-align: right; margin-left: auto; }
        </style></head><body><div class="column">
        <p class="byline"><strong>\(escape(byline.first ?? ""))</strong>\(byline.count > 1 ? " " + escape(byline[1]) : "")</p>
        <h1 class="title">\(title)</h1>
        <p class="dateline"><span>\(escape(date))</span><span>\(minutes) min read</span></p>
        \(summaryHTML)
        <div class="prose">\(e.content ?? "")</div>
        \(footer)
        </div></body></html>
        """
    }
}

/// A web view that shows one HTML page and opens its links in the browser.
#if os(iOS)
struct WebPage: UIViewRepresentable {
    var html: String
    var base: URL?
    var onNavigate: (Int) -> Void = { _ in }
    var onImage: (URL) -> Void = { _ in }
    func makeCoordinator() -> WebDelegate { WebDelegate() }
    func makeUIView(context: Context) -> WKWebView {
        let v = WKWebView(frame: .zero, configuration: WebDelegate.configuration(context.coordinator))
        v.navigationDelegate = context.coordinator
        v.isOpaque = false
        v.backgroundColor = .clear
        v.scrollView.backgroundColor = .clear
        return v
    }
    func updateUIView(_ v: WKWebView, context: Context) {
        context.coordinator.onNavigate = onNavigate
        context.coordinator.onImage = onImage
        if context.coordinator.loaded != html {
            context.coordinator.loaded = html
            v.loadHTMLString(html, baseURL: base)
        }
    }
}
#else
struct WebPage: NSViewRepresentable {
    var html: String
    var base: URL?
    var onNavigate: (Int) -> Void = { _ in }
    var onImage: (URL) -> Void = { _ in }
    func makeCoordinator() -> WebDelegate { WebDelegate() }
    func makeNSView(context: Context) -> WKWebView {
        let v = WKWebView(frame: .zero, configuration: WebDelegate.configuration(context.coordinator))
        v.navigationDelegate = context.coordinator
        v.setValue(false, forKey: "drawsBackground")
        return v
    }
    func updateNSView(_ v: WKWebView, context: Context) {
        context.coordinator.onNavigate = onNavigate
        context.coordinator.onImage = onImage
        if context.coordinator.loaded != html {
            context.coordinator.loaded = html
            v.loadHTMLString(html, baseURL: base)
        }
    }
}
#endif

final class WebDelegate: NSObject, WKNavigationDelegate, WKScriptMessageHandler {
    var loaded = ""
    var onNavigate: (Int) -> Void = { _ in }
    var onImage: (URL) -> Void = { _ in }

    static func configuration(_ delegate: WebDelegate) -> WKWebViewConfiguration {
        let c = WKWebViewConfiguration()
        // Article text is sanitized by the server; its scripts never run.
        c.defaultWebpagePreferences.allowsContentJavaScript = false
        // The app's own script, in a world apart from the page: a tap on a
        // picture asks the app to show it larger.
        let tap = """
        document.addEventListener('click', function (e) {
          var img = e.target.closest && e.target.closest('.prose img');
          if (!img || img.closest('a')) return;
          e.preventDefault();
          window.webkit.messageHandlers.image.postMessage(img.currentSrc || img.src);
        });
        """
        c.userContentController.addUserScript(WKUserScript(source: tap, injectionTime: .atDocumentEnd, forMainFrameOnly: true, in: .defaultClient))
        c.userContentController.add(delegate, contentWorld: .defaultClient, name: "image")
        return c
    }

    func userContentController(_ controller: WKUserContentController, didReceive message: WKScriptMessage) {
        if let s = message.body as? String, let url = URL(string: s) { onImage(url) }
    }

    #if DEBUG
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        if let path = ProcessInfo.processInfo.environment["CORR_DEBUG_WEB"] {
            NSLog("web: finished, frame %@", "\(webView.frame)")
            #if os(macOS)
            Task { @MainActor in
                try? await Task.sleep(for: .seconds(2))
                webView.takeSnapshot(with: nil) { image, error in
                    if let image, let tiff = image.tiffRepresentation { _ = path; try? tiff.write(to: FileManager.default.temporaryDirectory.appendingPathComponent("websnap.tiff")) }
                    NSLog("web: snapshot %@", error.map { "\($0)" } ?? "ok")
                }
            }
            #endif
        }
    }
    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        NSLog("web: failed %@", "\(error)")
    }
    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        NSLog("web: failed provisional %@", "\(error)")
    }
    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) { NSLog("web: content process terminated") }
    #endif

    func webView(_ webView: WKWebView, decidePolicyFor action: WKNavigationAction) async -> WKNavigationActionPolicy {
        if let url = action.request.url, url.scheme == "correspondent-nav" {
            onNavigate(url.absoluteString.hasSuffix("next") ? 1 : -1)
            return .cancel
        }
        if action.navigationType == .linkActivated, let url = action.request.url {
            #if os(iOS)
            await UIApplication.shared.open(url)
            #else
            NSWorkspace.shared.open(url)
            #endif
            return .cancel
        }
        return .allow
    }
}

/// Text size, typeface and line length, from the article (shared with the web).
struct TextControls: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Size").font(Typeface.ui.font(15, weight: 600)).foregroundStyle(theme.ink)
                Spacer()
                Button { set(size: app.prefs.readSize - 1) } label: { Text("A").font(Typeface.read.font(15)) }
                    .buttonStyle(QuietButton()).disabled(app.prefs.readSize <= 14)
                Text("\(Int(app.prefs.readSize))").font(Typeface.ui.font(14)).monospacedDigit().foregroundStyle(theme.slate).frame(width: 28)
                Button { set(size: app.prefs.readSize + 1) } label: { Text("A").font(Typeface.read.font(21)) }
                    .buttonStyle(QuietButton()).disabled(app.prefs.readSize >= 30)
            }
            Picker("Typeface", selection: Binding(get: { app.prefs.readFont }, set: { v in app.setPrefs({ $0.readFont = v }, ["readFont": v]) })) {
                Text("Serif").tag("serif")
                Text("Sans").tag("sans")
            }
            .pickerStyle(.segmented)
            VStack(alignment: .leading, spacing: 6) {
                Text("Line length · \(Int(app.prefs.measure))").font(Typeface.ui.font(13.5)).foregroundStyle(theme.slate)
                Slider(value: Binding(get: { app.prefs.measure }, set: { v in
                    let r = v.rounded()
                    if r != app.prefs.measure { app.setPrefs({ $0.measure = r }, ["measure": r]) }
                }), in: 52...84, step: 2)
            }
        }
        .tint(theme.accent)
        #if os(iOS)
        .presentationCompactAdaptation(.popover)
        #endif
    }

    private func set(size: Double) {
        let v = min(max(size, 14), 30)
        app.setPrefs({ $0.readSize = v }, ["readSize": v])
    }
}

/// A picture from an article, full screen: pinch or double-tap to zoom.
struct ImageViewer: View {
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss
    var url: URL
    @State private var scale: CGFloat = 1
    @State private var settled: CGFloat = 1
    @State private var offset: CGSize = .zero
    @State private var dragged: CGSize = .zero

    var body: some View {
        ZStack(alignment: .topTrailing) {
            Color.black.ignoresSafeArea()
            RemoteImage(url: url, size: CGSize(width: 1200, height: 1200)) { ProgressView().tint(.white) }
                .scaledToFit()
                .scaleEffect(scale)
                .offset(x: offset.width + dragged.width, y: offset.height + dragged.height)
                .gesture(MagnifyGesture()
                    .onChanged { scale = min(max(settled * $0.magnification, 1), 6) }
                    .onEnded { _ in settled = scale; if scale == 1 { offset = .zero } })
                .simultaneousGesture(DragGesture()
                    .onChanged { if scale > 1 { dragged = $0.translation } }
                    .onEnded { _ in offset.width += dragged.width; offset.height += dragged.height; dragged = .zero })
                .onTapGesture(count: 2) {
                    withAnimation(.easeOut(duration: 0.2)) {
                        scale = scale > 1 ? 1 : 2.5
                        settled = scale
                        if scale == 1 { offset = .zero }
                    }
                }
            Button { dismiss() } label: {
                Image(systemName: "xmark").font(.system(size: 16, weight: .semibold)).foregroundStyle(.white)
                    .frame(width: 40, height: 40).background(Circle().fill(.white.opacity(0.15)))
            }
            .buttonStyle(.plain).padding(16).accessibilityLabel("Close")
        }
        #if os(macOS)
        .frame(minWidth: 700, minHeight: 500)
        #endif
    }
}

extension URL: @retroactive Identifiable { public var id: String { absoluteString } }

extension Notification.Name {
    static let focus = Notification.Name("org.arenzana.correspondent.focus")
}
