import SwiftUI
import WebKit

#if os(iOS)
typealias PlatformImage = UIImage
#else
typealias PlatformImage = NSImage
#endif

/// A picture from the server (thumbnails, feed icons), as AsyncImage but
/// also for SVG, which many sites use for their icons: macOS reads SVG
/// itself; on iOS it is drawn by WebKit at the size shown. Pictures are
/// kept in memory, and their bytes in the URL cache.
struct RemoteImage<Placeholder: View>: View {
    var url: URL?
    /// The size it is shown at, in points: SVGs are drawn at this size.
    var size: CGSize
    @ViewBuilder var placeholder: () -> Placeholder
    @State private var image: PlatformImage?
    @Environment(\.displayScale) private var scale

    var body: some View {
        Group {
            if let image {
                #if os(iOS)
                Image(uiImage: image).resizable().scaledToFill()
                #else
                Image(nsImage: image).resizable().scaledToFill()
                #endif
            } else {
                placeholder()
            }
        }
        .task(id: url) {
            guard let url else { image = nil; return }
            image = await ImageLoader.shared.image(url, size: size, scale: scale)
        }
    }
}

actor ImageLoader {
    static let shared = ImageLoader()
    private let memory = NSCache<NSString, PlatformImage>()
    private var inFlight: [String: Task<PlatformImage?, Never>] = [:]

    func image(_ url: URL, size: CGSize, scale: CGFloat) async -> PlatformImage? {
        let key = "\(url.absoluteString)@\(Int(size.width))x\(Int(size.height))" as NSString
        if let hit = memory.object(forKey: key) { return hit }
        if let running = inFlight[key as String] { return await running.value }
        let task = Task<PlatformImage?, Never> {
            guard let (data, resp) = try? await URLSession.shared.data(from: url),
                  (resp as? HTTPURLResponse).map({ (200..<300).contains($0.statusCode) }) ?? true else { return nil }
            if let img = PlatformImage(data: data) { return img }
            let type = (resp as? HTTPURLResponse)?.value(forHTTPHeaderField: "Content-Type") ?? ""
            guard type.contains("svg") || Self.looksLikeSVG(data) else { return nil }
            #if os(iOS)
            return await SVGRenderer.shared.render(data, size: size, scale: scale)
            #else
            return nil
            #endif
        }
        inFlight[key as String] = task
        let img = await task.value
        inFlight[key as String] = nil
        if let img { memory.setObject(img, forKey: key) }
        return img
    }

    static func looksLikeSVG(_ data: Data) -> Bool {
        guard let head = String(data: data.prefix(512), encoding: .utf8) else { return false }
        return head.contains("<svg")
    }
}

#if os(iOS)
/// Draws SVG pictures with WebKit, one at a time: each in a fresh web view
/// of exactly its size, placed behind the app's content (WebKit doesn't
/// paint views it considers off screen), snapshotted, then removed.
@MainActor
final class SVGRenderer: NSObject, WKNavigationDelegate {
    static let shared = SVGRenderer()
    private var loaded: CheckedContinuation<Void, Never>?
    private var queue: Task<Void, Never>?
    /// Which render is waiting: a late timeout never ends the next one's.
    private var loading = 0

    func render(_ data: Data, size: CGSize, scale: CGFloat) async -> UIImage? {
        let previous = queue
        let mine = Task<UIImage?, Never> {
            await previous?.value
            return await draw(data, size: size)
        }
        queue = Task { _ = await mine.value }
        return await mine.value
    }

    private func draw(_ data: Data, size: CGSize) async -> UIImage? {
        guard size.width > 0, size.height > 0,
              let window = UIApplication.shared.connectedScenes.compactMap({ ($0 as? UIWindowScene)?.keyWindow }).first
        else { return nil }
        let c = WKWebViewConfiguration()
        c.defaultWebpagePreferences.allowsContentJavaScript = false
        let web = WKWebView(frame: CGRect(origin: .zero, size: size), configuration: c)
        web.isOpaque = false
        web.backgroundColor = .clear
        web.scrollView.backgroundColor = .clear
        web.scrollView.contentInsetAdjustmentBehavior = .never
        web.isUserInteractionEnabled = false
        web.navigationDelegate = self
        window.insertSubview(web, at: 0)
        defer { web.removeFromSuperview() }
        let w = Int(size.width), h = Int(size.height)
        let html = """
        <html><head><meta name="viewport" content="width=\(w), initial-scale=1"></head>
        <body style="margin:0;background:transparent;overflow:hidden">
        <img src="data:image/svg+xml;base64,\(data.base64EncodedString())" style="display:block;width:\(w)px;height:\(h)px;object-fit:cover">
        </body></html>
        """
        loading += 1
        let mine = loading
        await withCheckedContinuation { c in
            loaded = c
            web.loadHTMLString(html, baseURL: nil)
            // Renders wait in line: one that never finishes mustn't hold up
            // every other icon.
            Task { @MainActor in
                try? await Task.sleep(for: .seconds(5))
                if self.loading == mine { self.finish() }
            }
        }
        // The load event waits for the picture; give it frames to paint.
        try? await Task.sleep(for: .milliseconds(60))
        let config = WKSnapshotConfiguration()
        config.rect = CGRect(origin: .zero, size: size)
        config.afterScreenUpdates = true
        return try? await web.takeSnapshot(configuration: config)
    }

    private func finish() {
        loaded?.resume()
        loaded = nil
    }

    nonisolated func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        Task { @MainActor in self.finish() }
    }

    nonisolated func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        Task { @MainActor in self.finish() }
    }

    nonisolated func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        Task { @MainActor in self.finish() }
    }

    nonisolated func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        Task { @MainActor in self.finish() }
    }
}
#endif
