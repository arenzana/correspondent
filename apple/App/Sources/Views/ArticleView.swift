import SwiftUI
import WebKit
import CorrespondentKit

/// The open article: a toolbar, then the article as the web shows it
/// (ui/src/components/Article.svelte), rendered with the same styles.
struct ArticleView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.openURL) private var openURL
    /// On iPhone, the way back to the list.
    var onBack: (() -> Void)? = nil
    /// In a navigation stack (iPhone): the actions go in the navigation bar.
    var native = false

    var body: some View {
        VStack(spacing: 0) {
            if let e = app.article {
                if !native { toolbar(e) }
                ArticlePage(entry: e, feed: app.feedsByID[e.feedId], summary: app.summaryOpen ? e.summary : nil,
                            theme: theme, base: app.client?.server)
                    .id(e.id)
            } else if !app.articleError.isEmpty {
                Text("Couldn't load this article. \(app.articleError)").font(Typeface.ui.font(15)).foregroundStyle(theme.slate).padding()
                Spacer()
            } else {
                placeholder
            }
        }
        .background(theme.sheet)
        #if os(iOS)
        .toolbar {
            if native, let e = app.article {
                ToolbarItemGroup(placement: .topBarTrailing) { actions(e) }
            }
        }
        #endif
    }

    /// Star, read, summary, full article, original.
    @ViewBuilder
    private func actions(_ e: Entry) -> some View {
        Button { Task { await app.toggleStar(e.id) } } label: { Image(systemName: e.starred ? "star.fill" : "star") }
            .tint(e.starred ? theme.seal : theme.slate).accessibilityLabel(e.starred ? "Unstar" : "Star")
        Button { Task { await app.toggleRead(e.id) } } label: { Image(systemName: e.read ? "circle" : "largecircle.fill.circle") }
            .tint(theme.slate).accessibilityLabel(e.read ? "Mark unread" : "Mark read")
        Menu {
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
            Button { Task { await app.summarize() } } label: {
                Image(systemName: "text.alignleft").symbolEffect(.pulse, isActive: app.summarizing)
            }
            .iconButton().accessibilityLabel("Summary")
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
    var base: URL?

    var body: some View {
        WebPage(html: ArticleHTML.page(entry: entry, feed: feed, summary: summary, theme: theme), base: base)
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

    static func page(entry e: Entry, feed: Feed?, summary: String?, theme: Theme) -> String {
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
        .column { max-width: 68ch; margin: 0 auto; padding: 36px 24px 96px; }
        .byline { margin: 0 0 10px; font-family: var(--font-ui); font-size: 14px; color: var(--slate); }
        .byline strong { color: var(--ink); font-weight: 600; }
        h1.title { margin: 0 0 12px; font-family: var(--font-read); font-weight: 560; font-size: 34px; line-height: 1.12; letter-spacing: -0.015em; font-variation-settings: 'opsz' 72; text-wrap: balance; }
        h1.title a { color: inherit; text-decoration: none; }
        .dateline { margin: 0 0 22px; padding-bottom: 22px; border-bottom: 1px solid var(--rule); font-family: var(--font-ui); font-size: 13.5px; color: var(--slate); }
        .dateline span + span { margin-left: 14px; }
        .summary { margin: 0 0 28px; padding: 2px 0 2px 16px; border-left: 2px solid var(--accent); font-family: var(--font-ui); font-size: 15.5px; line-height: 1.55; }
        .summary p { margin: 0 0 6px; }
        .summary small { color: var(--slate); font-size: 12px; }
        .prose { font-family: var(--font-read); font-size: 19px; line-height: 1.66; font-variation-settings: 'opsz' 16; overflow-wrap: break-word; hanging-punctuation: first; }
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
        </style></head><body><div class="column">
        <p class="byline"><strong>\(escape(byline.first ?? ""))</strong>\(byline.count > 1 ? " " + escape(byline[1]) : "")</p>
        <h1 class="title">\(title)</h1>
        <p class="dateline"><span>\(escape(date))</span><span>\(minutes) min read</span></p>
        \(summaryHTML)
        <div class="prose">\(e.content ?? "")</div>
        </div></body></html>
        """
    }
}

/// A web view that shows one HTML page and opens its links in the browser.
#if os(iOS)
struct WebPage: UIViewRepresentable {
    var html: String
    var base: URL?
    func makeCoordinator() -> WebDelegate { WebDelegate() }
    func makeUIView(context: Context) -> WKWebView {
        let v = WKWebView(frame: .zero, configuration: WebDelegate.configuration())
        v.navigationDelegate = context.coordinator
        v.isOpaque = false
        v.backgroundColor = .clear
        v.scrollView.backgroundColor = .clear
        return v
    }
    func updateUIView(_ v: WKWebView, context: Context) {
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
    func makeCoordinator() -> WebDelegate { WebDelegate() }
    func makeNSView(context: Context) -> WKWebView {
        let v = WKWebView(frame: .zero, configuration: WebDelegate.configuration())
        v.navigationDelegate = context.coordinator
        v.setValue(false, forKey: "drawsBackground")
        return v
    }
    func updateNSView(_ v: WKWebView, context: Context) {
        if context.coordinator.loaded != html {
            context.coordinator.loaded = html
            v.loadHTMLString(html, baseURL: base)
        }
    }
}
#endif

final class WebDelegate: NSObject, WKNavigationDelegate {
    var loaded = ""

    static func configuration() -> WKWebViewConfiguration {
        let c = WKWebViewConfiguration()
        // Article text is sanitized by the server; scripts never run in it.
        c.defaultWebpagePreferences.allowsContentJavaScript = false
        return c
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
