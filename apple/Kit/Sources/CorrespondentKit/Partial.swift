import Foundation

// Whether an article body looks like a summary of a longer article on the
// web, so the reader can offer to fetch the rest. The web's
// ui/src/lib/partial.ts, with the same thresholds.

public enum Partial {
    static let minFull = 600
    // Teasers are rarely longer than this; one that ends by sending you
    // onward ("Continue reading") may run to several paragraphs.
    static let maxTeaser = 2500
    static let maxOnward = 8000
    // How far from the end a "continue reading" link may sit: feeds often
    // add a line or two after it.
    static let tail = 300

    nonisolated(unsafe) static let ellipsisEnd = /(…|\.\.\.|\[…\]|\[\.\.\.\])[\s.»→›)\]]*$/
    nonisolated(unsafe) static let onward = /(?i)\b(read more|continue reading|read the (full|rest)|keep reading|the post .{1,200} appeared first on)\b/

    public static func looksPartial(_ html: String) -> Bool {
        let text = html.replacing(/<[^>]+>/, with: " ")
            .replacing(/&nbsp;|&#160;/, with: " ")
            .replacing(/\s+/, with: " ")
            .trimmingCharacters(in: .whitespaces)
        let n = text.count
        if n < minFull { return true }
        if n < maxOnward, String(text.suffix(tail)).contains(onward) { return true }
        return n < maxTeaser && text.contains(ellipsisEnd)
    }
}
