#if os(macOS)
import SwiftUI
import AppKit

/// Keeps the main window's size and place across launches and updates.
/// SwiftUI saves them under a name made from the view's type, which every
/// build changes, so each update forgot them; this names the window itself.
/// A second window opens where macOS puts it.
struct RemembersFrame: NSViewRepresentable {
    static let name = "Correspondent"

    func makeNSView(context: Context) -> NSView { Tracker() }
    func updateNSView(_ view: NSView, context: Context) {}

    private final class Tracker: NSView {
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            guard let window else { return }
            // After SwiftUI has set up the window: naming it moves it to the
            // frame saved under the name, and AppKit then saves each move and
            // resize. It fails, harmlessly, when another window has the name.
            DispatchQueue.main.async {
                guard window.frameAutosaveName != RemembersFrame.name else { return }
                if window.setFrameAutosaveName(RemembersFrame.name) {
                    window.setFrameUsingName(RemembersFrame.name)
                }
            }
        }
    }
}
#endif
